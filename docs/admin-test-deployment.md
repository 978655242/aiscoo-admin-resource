# 中后台测试环境自动发布

合并到 `release` 即自动发布到单台测试服务器。部署全程在服务器的 `magpie` 普通账号下完成，CI 不使用 sudo，不新建账号。**本文描述的是发布草案（已按服务器只读核对结果和 `AGENTS.md` 的 Production Deployment 约定修订）；服务器尚未迁移，工作流尚未启用（需要先配置 Secrets 并完成第 3 节）。**

## 0. 固定端口（以 `AGENTS.md` 为准，不得对调）

| 服务 | 地址 | 说明 |
| --- | --- | --- |
| 官网 | `http://112.124.202.60/`（80）、`https://112.124.202.60/`（443） | 另一仓库，Nginx `default_server` |
| 后台 HTTPS | `https://112.124.202.60:8443/` | **本服务，固定 8443，绝不使用 443**。公网验收主入口 |
| 后台 HTTP 测试入口 | `http://112.124.202.60:8080/` | 仅测试数据；验收的补充入口 |
| 应用 | `127.0.0.1:3000` | 不对外；主机 nft 表 `inet aiscoo_isolation` 丢弃外部到 3000/5432/8005 |
| PostgreSQL | `127.0.0.1:5432` | 库 `aiscoo_admin` |

- 应用来源变量必须带端口：`XCODE_PUBLIC_ORIGIN` 与 `SESSION_COOKIE_TRUSTED_URL` 都是 `https://112.124.202.60:8443`，`SESSION_COOKIE_SECURE=true` 保持。`make-app-env.sh` 写入这个值，`app.env` 格式检查（`receive.sh` 上传前、`prestart.sh` 每次启动前、`setup-magpie.sh check-env`）**拒绝**不带 `:8443` 的值。用旧的无端口来源登录过的客户端需要重新登录。
- 验收：`https://112.124.202.60:8443/api/status` 返回后台；`https://112.124.202.60/api/status`（443）**不得**返回 JSON（应是官网的 404 页面）；`http://112.124.202.60:8080/api/status` 作为补充。
- 两个 HTTPS 监听共用一张 Let's Encrypt IP 证书（有效期 6 天，`aiscoo-certbot-renew.timer` 经 80 端口 HTTP-01 续期）。**本方案不修改 Nginx、证书或续期**；80 端口与 `/.well-known/acme-challenge/` 必须保持可达；改 Nginx 前先 `nginx -t`。
- 8443/8080 对外的 `X-Magpie-*` 响应头隐藏、`/api/setup` 与 `/api/user/register` 的拦截都在 Nginx，不在本方案范围内；探测只读取 `register_enabled` 状态标志。
- 不要把真实用户数据或真实供应商密钥发往明文的 `:8080`。

## 1. 架构

```
push → release
  └─ build job   : 测试 → 构建 linux/amd64 二进制（CGO 关闭，嵌入真实前端，版本串 release-<提交前12位>）→ 上传 artifact
  └─ deploy job  : 校验 artifact 摘要 → 检查 release HEAD == 待发布 SHA（上传前一次）
                   → SSH(restricted 专用 key) 推送「头部(含入口脚本 SHA-256)+二进制」
                   → 服务器 receive.sh：入口哈希核对/加锁/拒绝过期/校验/原子切换/重启/本机健康检查
                   → 公网探测 probe.py（8443 主验收 + 443 不得是后台 + 3000/5432/8005 不可达）
                   → 8080 补充探测 → 再次检查 release HEAD（探测后一次）
  └─ notify job  : 只在 build 与 deploy 全部成功且探测给出验收级别时发“成功”，其余发“失败”
```

服务器（`magpie` 家目录）：

```
~/aiscoo-admin/
  bin/        common.sh receive.sh rollback.sh prestart.sh entry-hash.sh probe.py   (0500，magpie 所有)
  etc/        app.env (0600，含 SQL_DSN、SESSION_SECRET 等 10 个键)  deploy.conf (可选)  backup.enabled (可选)
  releases/   <40位sha>-<run>-<attempt>   (0555 二进制，保留最近 5 个 + current + previous)
  current -> releases/…   previous -> releases/…
  state/      latest-run  accepted  blocked  deploy.lock  deploy.log  evidence/
              data/（工作目录）  logs/（--log-dir）  tmp/（TMPDIR）；应用的 HOME 也是 state/
  staging/    上传临时文件（成功或失败后均为空）
~/.config/systemd/user/aiscoo-admin.service
```

监听与端口不变：应用监听 `*:3000`，由主机 nft 表 `inet aiscoo_isolation` 丢弃外部到 3000/5432/8005 的流量，Nginx（8443/8080）反代到 `127.0.0.1:3000`。unit 里 `UnsetEnvironment=ENABLE_PPROF CRYPTO_SECRET NODE_NAME`：pprof 不会被误开，`CRYPTO_SECRET` 保持未设置（应用回落到 `SESSION_SECRET`，两者必然一致），`NODE_NAME` 与旧环境对齐。

### 版本串与构建（探针、服务器、构建三处必须一致）

线上现在的 `release-d5a492de8aa2` 是此前手工构建时注入的：`release-` + 提交前 12 位，通过 `-X github.com/QuantumNous/new-api/common.Version=…` 写入 `common.Version`，`/api/status` 的 `version` 字段和 `aiscoo-admin --version` 都输出它（`common/init.go` 先读环境变量 `VERSION` 覆盖，所以服务器上的版本检查用 `env -i` 清空环境）。仓库自带的 Dockerfile 用 `./VERSION` 文件内容（现为 `v0.x`），**与线上做法不同**，工作流因此不复用那一行，而是按 Dockerfile 的其余参数构建：

| 项 | Dockerfile | 本工作流 |
| --- | --- | --- |
| Bun | `oven/bun:1.4.4` | `oven-sh/setup-bun` `1.4.4`（与 `web/package.json` 的 `packageManager` 一致） |
| Go | `golang:1.26.10` | `setup-go` + `go-version-file: go.mod`（`go 1.26.10`，同一个版本源） |
| 前端 | `bun install --frozen-lockfile` + `bun run build` 后嵌入 `web/dist` | 同样，并检查 `dist/index.html` 存在且引用脚本；`VITE_REACT_APP_VERSION` = 版本串 |
| 链接 | `CGO_ENABLED=0`、`GOEXPERIMENT=greenteagc`、`-pgo=…/moejs/default.pgo`、`-ldflags "-s -w -X …Version=…"` | 同样，另加 `-trimpath`；构建后检查为静态、x86-64，且 `--version` 恰好等于版本串 |
| 版本值 | `$(cat VERSION)` | `release-${GITHUB_SHA:0:12}` |

探针要求 `/api/status` 的 `version` **等于** `release-<待发布 SHA 前 12 位>`（40 位原始 SHA、11 位前缀、别的提交都会失败，有测试覆盖）；`receive.sh` 要求上传的二进制 `--version` 输出同一个值，并用它做本机健康检查。只比较前 12 位，两个前 12 位相同的提交被视为同一版本（概率可忽略，不另行处理）。

### 与旧系统服务的对应关系（只读核对所得）

旧服务经 `/opt/aiscoo-admin/launch.py` 以**清空后的环境**启动应用。新 user unit 复刻其环境：

| 项 | 旧 | 新 |
| --- | --- | --- |
| 用户 / 工作目录 | magpie / `/opt/aiscoo-admin/work` | magpie / `~/aiscoo-admin/state/data` |
| `HOME` / `TMPDIR` | `/var/lib/aiscoo-admin` / `…/tmp` | `Environment=` 设为 `~/aiscoo-admin/state` / `…/state/tmp` |
| `PATH` | `/usr/local/bin:/usr/bin:/bin` | 同 |
| 日志目录 | `--log-dir /var/lib/aiscoo-admin/logs` | `--log-dir ~/aiscoo-admin/state/logs` |
| `UMask` / `NoNewPrivileges` | 0077 / true | 同 |
| `TimeoutStopSec` / `Restart` | 130 / no | 同 |
| 代理 | `127.0.0.1:17894`，由 magpie 的 user 服务 `aiscoo-xray.service` 提供 | 写入 `app.env`；unit 里 `After=`/`Wants=aiscoo-xray.service` |
| 启动前安全检查 | `launch.py` | `prestart.sh`（见下）|
| `PrivateTmp` / `ProtectSystem=strict` / `ReadWritePaths` | 有 | **没有**（见第 9 节）|

**环境没有被清空**：user manager 的继承环境里可能还有别的变量。unit 只显式 `UnsetEnvironment` 最小集合（`ENABLE_PPROF CRYPTO_SECRET NODE_NAME`；旧环境里 `TZ`、`ERROR_LOG_ENABLED`、`BATCH_UPDATE_ENABLED`、`LOG_DIR` 同样未设置，但本方案没有逐个清除，迁移后须核对（第 3 节第 12 步））。

### app.env 的变量与启动前检查

`app.env` 必须恰好包含这 10 个键（`KEY=VALUE` 一行一个，值不含引号/空格/`$`/`#`/反斜杠）：

| 键 | 值 | 来源 |
| --- | --- | --- |
| `SQL_DSN` | `postgresql://…@127.0.0.1:5432/aiscoo_admin[?sslmode=…]` | 复制自 `/etc/aiscoo-admin/database.env` 的同名行 |
| `SESSION_SECRET` | 64 位十六进制 | `/etc/aiscoo-admin/session.secret`（文件内容是裸值，转成 `KEY=VALUE`）|
| `PORT` | `3000` | 固定 |
| `XCODE_INTEGRATION_ENABLED` | `true` | 固定 |
| `XCODE_PUBLIC_ORIGIN` / `SESSION_COOKIE_TRUSTED_URL` | `https://112.124.202.60:8443` | 固定（必须带端口，格式检查强制） |
| `SESSION_COOKIE_SECURE` | `true` | 固定（与旧服务一致，见第 9 节）|
| `HTTP_PROXY` / `HTTPS_PROXY` | `http://127.0.0.1:17894` | 固定 |
| `NO_PROXY` | `127.0.0.1,localhost,::1` | 固定 |

`CRYPTO_SECRET` **不得**出现（检查会拒绝）。由 root 用 `make-app-env.sh` 一次性生成（第 3 节第 7 步）：它只用 `grep`/`tr`/`printf` 重定向到文件，不打印任何值，拒绝覆盖已有文件，结果为 0600、magpie 所有。

`prestart.sh`（unit 的 `ExecStartPre`）与 `receive.sh`（上传前）以及 `setup-magpie.sh check-env` 共用同一个**只读格式检查**，作为旧 `launch.py` 安全检查的等价替代：文件是 magpie 所有的普通文件（非符号链接）、权限为 0600、每行形如 `KEY=VALUE`、键名在白名单内且不重复、10 个键齐全、`SQL_DSN` 形如 `postgres(ql)://…@127.0.0.1:5432/aiscoo_admin`（可带查询参数）、两个来源变量形如 `https://主机:8443`、`SESSION_SECRET` 为 64 位十六进制、`PORT=3000`、代理指向 `127.0.0.1`。**错误信息只写键名和行号，不回显任何值**（测试用夹具“秘密”断言输出里不出现）。检查不验证值本身是否有效（例如密码是否正确）。

### 启动顺序

user unit 无法 `After=postgresql.service`。`prestart.sh` 在检查通过后循环探测 `127.0.0.1:5432`，最多 `PG_WAIT_TIMEOUT` 秒（默认 60，`deploy.conf` 可调，范围 1–80；unit 的 `TimeoutStartSec=90`），仍不可连接则以非零退出，unit 进入 `failed`（`Restart=no`，不会循环重启）。`receive.sh` 的 `wait_healthy` 默认 180 秒，足够覆盖这段等待。

**服务器重启后不会自动启动该服务**：本方案默认不 `enable`，由你决定是否执行 `systemctl --user enable aiscoo-admin.service`（见第 3 节第 13 步；开机自启时 `aiscoo-xray` 与数据库的就绪顺序由上面的等待和 `Wants=` 保证到“尽力而为”的程度，未验证）。

### 选型：EnvironmentFile，不用 LoadCredential

user 服务读不到 root-only 文件，`LoadCredential=` 的来源文件只能是 magpie 可读的文件，凭据还是同一个 magpie 可读的文件，没有额外隔离，只多一层复杂度。所以采用 `~/aiscoo-admin/etc/app.env`（0600，magpie 所有）。DSN 不进仓库也不进 unit 文件。

### 迁移失败不循环重启

unit 为 `Restart=no`。`receive.sh` 切换后若重启失败或超时未返回预期版本，写 `state/blocked`、保存证据（状态+最近 200 行日志）、**不自动降级、不动数据库**，之后所有部署被拒绝，直到人工处理。

## 2. 入口脚本协议（receive.sh）与入口脚本权威性

SSH 授权行：`restrict,command="/home/magpie/aiscoo-admin/bin/receive.sh" ssh-ed25519 … aiscoo-admin-ci-deploy`。`restrict` 同时禁止 pty、端口转发、agent/X11 转发和 `~/.ssh/rc`。入口脚本在 `SSH_ORIGINAL_COMMAND` 非空时直接拒绝。

stdin：一行头部 + 恰好 `size` 字节：

```
deploy <sha1-40hex> <run-number> <run-attempt> <二进制sha256-64hex> <size> <入口哈希-64hex>
```

客户端不能传入路径或命令。**入口脚本以服务器上的副本为准**：`<入口哈希>` 是 CI 对其检出内容算出的 `entry-hash.sh` 值（覆盖 `entry-hash.sh common.sh receive.sh rollback.sh prestart.sh` 与 `aiscoo-admin.service` 的各自 SHA-256，`probe.py` 不在其中，因为服务器不运行它）。`receive.sh` 用同一个脚本对**已安装副本**（`~/aiscoo-admin/bin` 与 `~/.config/systemd/user/aiscoo-admin.service`）重新计算，不相等就在任何落盘之前拒绝，提示按审查过的仓库版本重新安装（`setup-magpie.sh install`）。CI 的 key 只能运行服务器上的 `receive.sh`，不能替换它；所以**修改 `scripts/deploy/` 或 unit 的 PR，必须先在服务器上安装新版本再合并**，否则合并后的发布会被拒绝（这是预期的保护）。线上入口本身保持不变，不会被 CI 更新。

处理顺序：头部限时 10 秒/512 字节 → 目录属主与权限检查 → **入口哈希核对** → 拒绝过期 run → 上传（限时，默认 900 秒，≤1 GiB）→ 长度/多余数据检查 → SHA-256 → ELF64 小端 x86-64 → 在清空环境下运行 `--version`，必须等于 `release-<头部 SHA 前 12 位>` → **取全局 flock（默认 30 秒）** → 持锁复查 `blocked` 与 run 序号 → 登记 `latest-run` → 原子切换 `current` → `systemctl --user restart` → 轮询 `127.0.0.1:3000/api/status`（默认 180 秒）版本必须等于 `release-<前 12 位>`。

过期规则：按 `(run number, attempt)` 严格递增。更小的 run、同一 run 的相同或更小 attempt 被拒；同一 run 的更高 attempt（GitHub “Re-run”）允许。拒绝发生在任何落盘之前，失败不留半成品（测试已覆盖）。

## 3. 一次性迁移步骤（阶段二；每条在执行前由你确认）

云助手命令里不得出现凭据，也不得 `cat` 秘密文件。以下 `<…>` 为占位。**身份标注：【root】= 云助手以 root 执行；【magpie】= 以 magpie 身份（SSH 或 `runuser`）执行；【本机】= 你的电脑。** 步骤按顺序做，每个阶段的验证通过再进入下一阶段。

**A. 只读核对（【root】）**

1. `loginctl show-user magpie -p Linger -p State` 应为 `Linger=yes`；`test -S /run/user/1001/bus && echo bus-ok`。
2. 确认 3000 端口只被旧系统服务占用：`ss -ltnp | grep ':3000'`。
3. 确认 `aiscoo-xray` 在运行：`runuser -u magpie -- env XDG_RUNTIME_DIR=/run/user/1001 systemctl --user is-active aiscoo-xray.service`。
4. 确认 22 端口对 GitHub 托管 runner 可达（云防火墙/UFW 是否限制来源）。
5. 记录现状作为回退依据：`systemctl is-enabled aiscoo-admin.service`（应为 `enabled`）、`curl -s http://127.0.0.1:3000/api/status`（版本应为 `release-d5a492de8aa2`）、`nginx -t`。

**B. 安装脚本与 unit（不影响现有服务）**

6. 【本机】把 `scripts/deploy/{common.sh,receive.sh,rollback.sh,prestart.sh,entry-hash.sh,probe.py,setup-magpie.sh,aiscoo-admin.service}` 用 scp 以 magpie 身份传到 `~/deploy-src/`；`make-app-env.sh` 另传到 root 自己的目录（如 `/root/deploy-src/`，root 所有）。**传的必须是即将合并到 `release` 的那一版**，否则首次发布会因入口哈希不一致被拒绝。
7. 【magpie】`bash ~/deploy-src/setup-magpie.sh install`：创建 `~/aiscoo-admin/{releases,state/{data,logs,tmp},staging,etc,bin}`，安装脚本和 user unit，`daemon-reload`；**不启动、不 enable**。此时 `app.env` 还不存在，脚本会提示。

**C. 生成 app.env（【root】，一次，不打印秘密）**

8. 【root】`bash /root/deploy-src/make-app-env.sh`（默认读 `/etc/aiscoo-admin/{database.env,session.secret}`，写 `/home/magpie/aiscoo-admin/etc/app.env`，0600，magpie 所有）。脚本只输出键数和 PASS/FAIL；写入的 `XCODE_PUBLIC_ORIGIN` 与 `SESSION_COOKIE_TRUSTED_URL` 都是 `https://112.124.202.60:8443`。等价的手工做法（同样不回显）：

   ```
   umask 077; f=/home/magpie/aiscoo-admin/etc/app.env
   { grep '^SQL_DSN=' /etc/aiscoo-admin/database.env
     printf 'SESSION_SECRET=%s\n' "$(tr -d '\n' < /etc/aiscoo-admin/session.secret)"
     printf '%s\n' PORT=3000 XCODE_INTEGRATION_ENABLED=true XCODE_PUBLIC_ORIGIN=https://112.124.202.60:8443 \
       SESSION_COOKIE_SECURE=true SESSION_COOKIE_TRUSTED_URL=https://112.124.202.60:8443 \
       HTTP_PROXY=http://127.0.0.1:17894 HTTPS_PROXY=http://127.0.0.1:17894 NO_PROXY=127.0.0.1,localhost,::1
   } > "$f" && chown magpie: "$f" && chmod 600 "$f"
   ```

   不要用 `set -x`、`cat`、`echo $SESSION_SECRET` 之类。
9. 【magpie】`bash ~/deploy-src/setup-magpie.sh check-env`：只读格式检查，成功输出 `format check passed (values not shown)`。失败只会列出键名（例如来源变量缺 `:8443` 时报 `XCODE_PUBLIC_ORIGIN`）。

**D. 后台专用 CI 密钥与主机指纹（与官网那把分开）**

10. 【本机】生成后台专用 ed25519 密钥（私钥只存在本机临时目录，放入 GitHub Secret 后删除；**不要复用官网的 `OFFICIALSITE_DEPLOY_KEY`**）：
    `d=$(mktemp -d) && ssh-keygen -t ed25519 -N '' -C aiscoo-admin-ci-deploy -f "$d/admin_ci_key" && echo "$d"`
    把 `$d/admin_ci_key.pub` 传到服务器。
11. 【magpie】`bash ~/deploy-src/setup-magpie.sh authorize <公钥文件>`：在 `~/.ssh/authorized_keys` 追加一行
    `restrict,command="/home/magpie/aiscoo-admin/bin/receive.sh" ssh-ed25519 AAAA… aiscoo-admin-ci-deploy`
    （官网那把 key 的行不受影响；该 key 只能运行 `receive.sh`，不能运行官网的入口）。已存在的同一公钥会被拒绝。
12. 通过独立可信通道取主机指纹：【root】`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`，与【本机】`ssh-keyscan -t ed25519 112.124.202.60` 的指纹核对一致后，把 `112.124.202.60 ssh-ed25519 AAAA…` 一行放入 `ADMIN_DEPLOY_KNOWN_HOSTS`（官网那边已有同样内容，可复用其值，但仍是各自独立的 Secret）。
13. 【本机】在 GitHub 仓库配置第 7 节的 Secrets（`ADMIN_FEISHU_WEBHOOK` 已存在）；把 `$d/admin_ci_key` 的完整内容写入 `ADMIN_DEPLOY_SSH_KEY` 后，`rm -rf "$d"`。

**E. 切换服务（有短暂停机；新旧都占 3000，必须先停旧的）**

14. 【root】只停止并禁用旧系统服务，**不删除**旧 unit、`/opt/aiscoo-admin`（含已改成 `:8443` 的 `launch.py`）、`/etc/aiscoo-admin`：
    `systemctl stop aiscoo-admin.service && systemctl disable aiscoo-admin.service && ! ss -ltn | grep -q ':3000 '`
    （停止最长约 130 秒，旧 unit 的 `TimeoutStopSec`。）此后 8443/8080 暂时 502，直到下一步完成。
    保险：旧服务仍占 3000 时，`receive.sh` 会在上传后、写任何状态之前拒绝（消息 `port 3000 is held by another process`），不消耗 run 编号，停掉旧服务后直接 Re-run 即可。
15. 首次发布由工作流完成：把包含这些文件的提交合并到 `release`，或在 Actions 页对 `release` 分支手动运行 `Deploy admin test instance`（`workflow_dispatch`）。第一次 `systemctl --user restart` 会启动新的 user 服务；数据库迁移由应用启动时自动完成。
16. 验证（缺一不可）：
    - Actions 的 `Public acceptance probe` 与 `Supplementary probe` 通过；飞书收到“发布成功”，并注意其中是否有“TLS 链未校验”。
    - 【本机】`curl -sS https://112.124.202.60:8443/api/status`（不带 `-k`）返回的 `version` 是 `release-<新提交前12位>`；`curl -sS -o /dev/null -w '%{http_code}\n' https://112.124.202.60/api/status` 为官网的 404（不是 JSON）；官网首页 `https://112.124.202.60/` 与 `http://112.124.202.60/` 照常。
    - 【root】`ss -ltnp | grep ':3000'` 显示进程属 magpie；`ss -ltn | grep ':8005'` 无输出；`nginx -t`。
    - 【magpie】核对进程环境**只看名称**：`tr '\0' '\n' < /proc/$(systemctl --user show -p MainPID --value aiscoo-admin.service)/environ | cut -d= -f1 | sort`，确认没有 `CRYPTO_SECRET`、`ENABLE_PPROF`、`NODE_NAME`、`TZ`、`LOG_DIR`、`ERROR_LOG_ENABLED`、`BATCH_UPDATE_ENABLED`，以及没有其他预期之外的变量。
    - 【magpie】`systemctl --user show aiscoo-admin.service -p ActiveState -p Result -p UMask`；`journalctl --user -u aiscoo-admin.service -n 50 --no-pager`（日志里不应含秘密，如有请告知）。
    - 浏览器重新登录一次（来源从无端口变为 `:8443`，旧会话需重新登录）；按 `AGENTS.md`，升级后复查各模型的计费表达式与价格配置。
17. （可选，**由你决定，本方案不替你执行**）开机自启：【magpie】`systemctl --user enable aiscoo-admin.service`。不执行则服务器重启后服务不会自动运行，需手动 `systemctl --user start aiscoo-admin.service` 或再次部署。

### 迁移失败回退（旧系统服务文件保留未删）

先停工作流（最简单：在 Actions 页禁用 `Deploy admin test instance`，或暂不向 `release` 合并），否则下一次发布会再启动新的 user 服务并与旧服务争用 3000。然后：

**情形一：首次发布失败（`receive.sh` 已把 `state/blocked` 写下，新服务没起来或不健康）。**
1. 【magpie】保存证据：`ls ~/aiscoo-admin/state/evidence/`，`tail -n 50 ~/aiscoo-admin/state/deploy.log`（不贴出含秘密的内容）。
2. 【magpie】停新服务：`systemctl --user stop aiscoo-admin.service`（未 enable 过则无需 disable；若 enable 过：`systemctl --user disable aiscoo-admin.service`）。
3. 【root】确认 3000 已空闲：`! ss -ltn | grep -q ':3000 '`。
4. 【root】恢复旧系统服务：`systemctl enable --now aiscoo-admin.service && systemctl is-active aiscoo-admin.service`。旧 `launch.py` 已是 `:8443`，登录来源不变。
5. 验证：`curl -sS https://112.124.202.60:8443/api/status` 返回旧版本串 `release-d5a492de8aa2`；`https://112.124.202.60/api/status` 仍不是 JSON。
6. `state/blocked` 留在原处不用管（旧服务不看它）；排查并修复后，再按“重新迁移”重走第 14 步起，届时先 `~/aiscoo-admin/bin/rollback.sh clear-blocked`。

**情形二：首次发布已成功，之后需要回退到旧系统服务。**
1. 【magpie】`systemctl --user stop aiscoo-admin.service`，如已 enable 则 `disable`。
2. 【root】`! ss -ltn | grep -q ':3000 '` 后 `systemctl enable --now aiscoo-admin.service`，`systemctl is-active aiscoo-admin.service`。
3. 数据库不受这些命令影响；但新版本可能已迁移 schema，旧版能否运行须自行确认（第 8 节：没有备份）。

**回退后的前提**：旧单元文件、`/opt/aiscoo-admin`、`/etc/aiscoo-admin` 仍在，所以才能这样回退；做了“迁移完成后的清理”之后这条路径不再存在。

### 迁移完成并稳定后（可选清理，不属于迁移步骤）

在新服务稳定运行一段时间、且你确认不再需要回退后，才可以由【root】自行清理旧残留：`/opt/aiscoo-admin`（`launch.py`、旧二进制、`work/`）、`/etc/aiscoo-admin`（**含秘密，删除前确认 `app.env` 已有同样内容，并自行决定是否保留备份**）、旧 `aiscoo-admin.service` 单元文件，以及 `/var/lib/aiscoo-admin`（仅 356 KB 日志）。清理后回退路径不再存在。本仓库不提供清理脚本。

## 4. 回滚（只切换二进制，不碰数据库）

在服务器以 magpie 运行（也可由有 shell 权限的操作员运行；CI key 不能运行）：

```
~/aiscoo-admin/bin/rollback.sh list
~/aiscoo-admin/bin/rollback.sh to previous I_HAVE_VERIFIED_SCHEMA_COMPATIBILITY
~/aiscoo-admin/bin/rollback.sh clear-blocked      # 自行处理完失败的部署后解除阻塞
```

新版本可能已执行不可逆迁移，旧版直接启动可能破坏数据，所以必须带确认词，并先自行确认 schema 向后兼容。回滚前先写 `blocked`，成功且本机版本匹配后才清除；失败则保留。回滚后需运行 `probe.py` 做公网验收（版本参数是被回滚到的那个提交的完整 40 位 SHA，即 release 目录名的前 40 位）：`python3 -I scripts/deploy/probe.py https://112.124.202.60:8443 <40位SHA>`。**没有数据库备份时，迁移失败只能人工处理。**

## 5. 公网验收与通知

`probe.py <origin> <sha>`（工作流里 `<origin>` 固定为 `https://112.124.202.60:8443`）按序检查：

1. TLS：先做正常的证书链校验（见下“TLS 降级”）。
2. `/api/status` 的 `version` == `release-<待发布 SHA 前 12 位>`；`setup == true`；`register_enabled == false`（读取状态标志，**不发送注册请求**）。
3. 首页及其引用的全部同源 JS 返回 200 且为 JavaScript。
4. 外部到 3000/5432/8005 的 TCP 不可达。
5. **443 不得是后台**：`AISCOO_PROBE_NOT_ADMIN_ORIGINS=https://112.124.202.60`，对其 `/api/status` 发 GET，返回体能解析为 JSON 即失败（官网返回 404 HTML 即通过）；该来源必须与验收来源同主机、不同端口，否则参数错误。
6. 结束前再查一次版本。

默认重试 6 次、间隔 5 秒（等待服务就绪）。任何一项失败，整个工作流失败。随后 `Supplementary probe` 对 `http://112.124.202.60:8080` 做同样的无凭据检查；它是补充，设置了 `continue-on-error`，失败时不使发布失败，但**不会被静默放过**：飞书通知里会写“8080 补充检查未通过”。

**TLS 降级路径**（因为 GitHub runner 的 CA 库是否信任 Let's Encrypt 的根未知）：`AISCOO_PROBE_TLS=strict|fallback`，工作流默认 `fallback`（仓库变量 `ADMIN_PROBE_TLS=strict` 可改为严格）。`fallback` 下先正常校验；**仅当**错误是“签发者/根不被信任”（OpenSSL 验证码 2、18、19、20、21）时才继续不校验，并且：日志打印 `WARNING: … could NOT be verified`、输出 `TLS: UNVERIFIED`、工作流 `::warning::`、飞书成功通知追加“TLS 链未校验”；此时**绝不发送令牌**（登录态检查自动降为 `basic`）。证书过期、主机名不符、吊销、连接被拒等其他 TLS/网络错误**不降级**，直接失败。未校验的验收只能证明版本和路由，不能证明对端身份，所以首次发布后请在【本机】用不带 `-k` 的 `curl` 再核对一次（第 3 节第 16 步）。

验收级别：有探测账号时为 `full`（额外 `GET /api/user/self` 校验账号 id），否则为 `basic`，并在日志和通知里如实标注“未做登录态业务检查”。令牌只会经**已校验的 HTTPS** 发送：HTTP 来源需要 `AISCOO_PROBE_TOKEN_OVER_HTTP=1`（工作流不设置它，8080 补充探测也不带令牌），TLS 未校验时一律跳过。

通知（`notify` job，`needs: [build, deploy]`，`if: always() && !cancelled()`）：仅当 build 与 deploy 都为 `success` 且探测给出验收级别时发“发布成功”，并附验收入口、443 结果、8080 结果和 TLS 状态；否则发“发布失败”，并写出失败阶段（构建/测试、核对或上传、公网验收、验收之后的 release HEAD 复核）。被取消的运行（例如被更新的 push 顶替）不发。webhook 只从 Secret 读取，经 curl 的 stdin 配置传入，不进参数和日志；发送失败、webhook 缺失或被拒只产生警告，不改变工作流结果，也不会把失败说成成功。

## 6. 并发与过期保护

- 工作流 `concurrency: aiscoo-admin-deploy`、`cancel-in-progress: false`：不取消运行中的部署；等待中的运行会被更新的 push 顶替（GitHub 同组仅保留一个等待运行），最新提交最终会被发布。
- 服务器端 flock 串行，并按 run 序号拒绝过期/重复。
- 部署前（上传前）与探测后各检查一次 `release` HEAD == 待发布 SHA。部署前不一致则拒绝；**探测后不一致只让本次运行失败并说明被顶替，不会自动回滚**（新二进制可能已迁移数据库）。
- 剩余竞态：检查与上传之间 release 仍可能前进。后果有限：服务器按 run 序号保证不会用更旧的 run 覆盖更新的 run，更新的 run 随后部署。

## 7. Secrets 与变量清单

用户自行配置，Agent 不创建、不读取。

| 名称 | 类型 | 值 | 必须 |
| --- | --- | --- | --- |
| `ADMIN_DEPLOY_TARGET` | Secret | `magpie@112.124.202.60` | 是 |
| `ADMIN_DEPLOY_SSH_KEY` | Secret | 后台专用 ed25519 私钥（完整 PEM），**不得与官网那把相同** | 是 |
| `ADMIN_DEPLOY_KNOWN_HOSTS` | Secret | 一行 `112.124.202.60 ssh-ed25519 AAAA…`（经独立通道核对） | 是 |
| `ADMIN_FEISHU_WEBHOOK` | Secret | 飞书群自定义机器人 webhook（**已存在**） | 否（缺失时只告警，不通知） |
| `ADMIN_PROBE_TOKEN` + `ADMIN_PROBE_USER_ID` | Secret | 只读探测账号的访问令牌与其用户 id，**必须成对** | 否（缺失则探针降级为 `basic`，通知里标注“未做登录态业务检查”） |
| `ADMIN_PROBE_TLS` | Variable | `strict` 禁止 TLS 降级；缺省为 `fallback` | 否 |

验收地址已固定在工作流里（`https://112.124.202.60:8443`、`https://112.124.202.60`、`http://112.124.202.60:8080`），**不再提供 `ADMIN_PUBLIC_ORIGIN` 变量**，避免有人把验收指到 443 或别处。不要使用已有的个人访问令牌。工作流未使用 GitHub Environment（私有仓库当前套餐可能不支持保护规则），Secrets 为仓库级。

## 8. 可选：迁移前备份（默认关闭）

**当前未启用备份，迁移失败无法自动恢复。** 服务器目前没有 age 和备份目录，本仓库不提供备份实现。接口：创建 `~/aiscoo-admin/etc/backup.enabled`，并提供可执行的 `~/aiscoo-admin/bin/pre-migration-backup`（magpie 所有）。启用后 `receive.sh` 会在切换前停服务并运行它；失败则丢弃新版本、重新启动旧版本、不写 `blocked`；启用但缺少该程序则在上传前直接拒绝。备份方案（如 `pg_dump | age`，公钥放服务器、私钥不在服务器、需要独立恢复演练）由你另行决定。

## 9. 已知限制

- **8080 是 HTTP 明文**：只适合测试数据，探测令牌绝不会发往它。主验收走 8443 HTTPS；但如果 runner 不信任该证书链，验收会按降级路径在**未校验**的 TLS 上完成（日志和通知里有标注），此时它只能证明版本与路由，不能证明对端身份。
- **无备份**：见第 8 节。
- **无分支保护**：私有仓库当前套餐无法强制分支保护，任何有写权限的人可直接 push 到 `release` 触发发布；Secrets 也对所有能改工作流的人可用。需要靠团队约定。
- **同账号可读凭据**：user 服务与 `magpie` 的其他进程（含已有的 `aiscoo-magpie-web-*` user 服务）同一账号，可读 `app.env` 中的数据库凭据。没有进程隔离。
- **与旧单元的隔离差异**：旧单元有 `PrivateTmp=true`、`ProtectSystem=strict`、`ReadWritePaths=/var/lib/aiscoo-admin`。新 user unit **故意不加** `PrivateTmp`/`ProtectSystem`/`ProtectHome`：它们在 user manager 里依赖非特权用户命名空间，Ubuntu 24.04（内核 6.8）可能经 AppArmor 限制，未验证；若加入而不可用，服务会以 `226/NAMESPACE` 启动失败。保留的隔离只有 `NoNewPrivileges`、`UMask=0077`、非 root 账号；服务对 `magpie` 家目录和 `/tmp` 都有读写能力。如需恢复这些指令，应在服务器上先试验再加。
- **环境未清空**：旧服务的启动器清空环境；新 unit 只设置 `HOME`/`TMPDIR`/`PATH` 并 `UnsetEnvironment` 三个变量，user manager 的其他继承变量仍可能传入（迁移后按第 3 节第 16 步核对）。
- **启动顺序**：user unit 不能排在系统服务 `postgresql.service` 之后，只靠 `prestart.sh` 最多等 60 秒；开机后数据库更晚就绪则服务启动失败（不重启循环）。默认不 enable。
- **`SESSION_COOKIE_SECURE=true`**：保持不变，与 HTTPS 的 8443 一致；经明文 `:8080` 登录大概率拿不到会话 cookie，这是预期（8080 仅作测试入口，**未验证**）。`probe.py` 不发送 cookie。来源变量从无端口改成 `:8443` 后，旧会话需重新登录。
- **Nginx、证书与续期不在本方案内**：8443/8080 的 server 块、IP 证书（6 天有效期）续期、`X-Magpie-*` 头隐藏和注册路径拦截由服务器上已有配置负责。如果证书续期失败，8443 验收会因证书过期失败（不会被降级放过）。
- **入口脚本更新要先装服务器**：见第 2 节，合并改动 `scripts/deploy/` 或 unit 的 PR 之前，要先在服务器上 `setup-magpie.sh install` 新版本，否则下一次发布会被拒绝。
- **版本串只比较前 12 位**：前 12 位相同的两个提交无法区分（实际概率可忽略）。
- **CI key 的权限**：该 key 只能运行 `receive.sh`，但等价于“能向服务器提交并运行任意二进制”，必须保护好私钥和仓库写权限。
- **探测来自 GitHub 托管 runner**：不能证明内网/其他网络位置的可达性；端口不可达检查依赖 runner 的出口策略。
- **`register_enabled` 为状态标志**：不是实际注册请求的结果。
- 部署期间服务短暂中断（重启）；无蓝绿。
- 旧数据目录 `/var/lib/aiscoo-admin` 只有约 356 KB 日志，**不迁移**；新日志从空目录开始。

## 10. 测试覆盖

| 内容 | 命令 | 状态 |
| --- | --- | --- |
| probe 与文件静态契约（38 项，含：`release-<12>` 版本比对、443 不得返回 JSON、HTTPS 用一次性 CA 的严格校验 / 仅信任问题时的显式降级 / 降级时不发令牌 / 主机名不符与过期不降级、所有后台地址带 `:8443`、工作流固定 `ubuntu-24.04` 与 SHA、入口哈希覆盖范围） | `python3 -I scripts/deploy/test_deploy.py` | 已测，工作流内每次运行 |
| Linux amd64 容器（136 项）：协议解析、入口哈希（正确 / 错误 / 已装脚本被改 / unit 被改 / 头缺哈希）、`release-<12>` 版本核对（40 位裸 SHA 被拒）、锁与并发、过期 run、校验和、非 ELF/arm64、原子切换、失败不留半成品、迁移失败阻塞、人工回滚、可选备份钩子、保留策略、`make-app-env.sh`（值为 `:8443`）与 `app.env` 格式检查（含无端口 / `:443` / http 来源被拒）、旧服务占用 3000 时拒绝且不消耗 run、`prestart.sh` 等待 PostgreSQL、桩 `systemctl` 的 ExecStartPre 语义、真实 sshd + `restrict` key | `AISCOO_DEPLOY_TEST_IMAGE=<镜像> bash scripts/deploy/test_linux_deploy.sh`（需要 docker、go 和本地 `aiscoo-deploy-test:local` 镜像） | 已测，手动运行，不在 CI 中 |
| 用工作流同样的参数在本机交叉编译 linux/amd64 二进制（`CGO_ENABLED=0`、`GOEXPERIMENT=greenteagc`、PGO、`-X …Version=release-<12>`，嵌入本地已有的 `web/dist`），在 amd64 容器里 `--version` | 手动 | 已测一次：输出 `release-660ae9cb99bf`，ELF64 x86-64。没有在本机重建前端，也没有 `readelf` 做静态链接检查（CI 里有） |
| 真实 systemd 255 user manager 下 unit 的行为：`EnvironmentFile`、`Environment=`、`UnsetEnvironment`、`UMask`、`ExecStartPre` 失败语义、`TimeoutStartSec`、`Restart=no`、`After=/Wants=aiscoo-xray.service`、linger | — | **未验证**：容器测试使用 `systemctl` 桩 |
| AppArmor 对非特权用户命名空间的限制、`PrivateTmp`/`ProtectSystem` 在 user unit 里是否可用 | — | **未验证**（因此未加入 unit）|
| 真实 PostgreSQL 就绪等待（用桩 TCP 监听代替）、`app.env` 的值是否真实有效（检查只验证格式）、真实服务器重启后的行为、真实数据库迁移 | — | **未验证** |
| GitHub 托管 runner 对 Let's Encrypt IP 证书链的信任（决定走严格还是降级）、真实 Actions 运行、飞书发送、真实 Nginx/nft/云防火墙 | — | **未验证** |
| `shellcheck` | — | **未运行**（本机未安装；仅 `bash -n`） |
