# shellcheck shell=bash
# Sourced by receive.sh and rollback.sh. Defines helpers only; runs nothing.
# Everything runs as the unprivileged service account. There is no sudo anywhere.

umask 077
export LC_ALL=C
export PATH=/usr/local/bin:/usr/bin:/bin

die() { printf 'deploy: %s\n' "$*" >&2; exit 1; }

[[ $(id -u) != 0 ]] || die 'must not run as root'
home=$(getent passwd "$(id -u)" | cut -d: -f6)
[[ $home == /* ]] || die 'cannot determine home directory'
base="$home/aiscoo-admin"
unit=aiscoo-admin.service

# systemctl --user needs the user manager (loginctl enable-linger) and its bus.
XDG_RUNTIME_DIR="/run/user/$(id -u)"
export XDG_RUNTIME_DIR
[[ ! -S $XDG_RUNTIME_DIR/bus ]] || export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"
[[ -d $XDG_RUNTIME_DIR ]] || die "user manager runtime dir missing: $XDG_RUNTIME_DIR (is linger enabled?)"

# Never hand the deployment lock (fd 9) to the service or to anything systemctl starts.
ctl() { systemctl --user "$@" "$unit" 9>&-; }

log() {
  local line
  line=$(printf '%s %s' "$(date -u +%FT%TZ)" "$*")
  printf 'deploy: %s\n' "$*" >&2
  printf '%s\n' "$line" >> "$base/state/deploy.log"
}

# The version string the build embeds (-X ...common.Version=): "release-" + the first 12 hex digits of the commit.
# This is what /api/status and `--version` report, so every comparison goes through here.
release_version() { printf 'release-%s' "${1:0:12}"; }

# Directories must be owned by this account and not writable by anyone else.
preflight() {
  local d perm
  for d in "$base" "$base/releases" "$base/state" "$base/staging" "$base/etc" "$base/bin"; do
    [[ -d $d && ! -L $d && -O $d ]] || die "missing, foreign or symlinked directory: $d"
    perm=$(stat -c %a "$d")
    (( (8#$perm & 022) == 0 )) || die "directory writable by others: $d"
  done
}

# Tunables come only from etc/deploy.conf (whitelisted keys, numeric values), never from the environment.
# shellcheck disable=SC2034  # read by the scripts that source this file
HEALTH_TIMEOUT=180 KEEP_RELEASES=5 UPLOAD_TIMEOUT=900 LOCK_TIMEOUT=30 PG_WAIT_TIMEOUT=60
load_conf() {
  local file="$base/etc/deploy.conf" key value
  [[ -e $file ]] || return 0
  [[ -f $file && ! -L $file && -O $file ]] || die 'deploy.conf must be a regular file owned by this account'
  while IFS='=' read -r key value; do
    [[ -z $key || $key == '#'* ]] && continue
    [[ $key =~ ^(HEALTH_TIMEOUT|KEEP_RELEASES|UPLOAD_TIMEOUT|LOCK_TIMEOUT|PG_WAIT_TIMEOUT)$ && $value =~ ^[0-9]{1,6}$ ]] || die 'invalid deploy.conf entry'
    printf -v "$key" '%s' "$((10#$value))"
  done < "$file"
  # The unit allows 90 s for ExecStartPre (TimeoutStartSec); a longer database wait would be killed by systemd.
  (( PG_WAIT_TIMEOUT >= 1 && PG_WAIT_TIMEOUT <= 80 )) || die 'PG_WAIT_TIMEOUT must be between 1 and 80'
}

# Read-only format check of etc/app.env: the replacement for the old launcher's safety checks.
# Reports key names and line numbers only. A value is never printed, and no value is ever passed to a command.
APP_ENV_KEYS=(SQL_DSN SESSION_SECRET PORT XCODE_INTEGRATION_ENABLED XCODE_PUBLIC_ORIGIN SESSION_COOKIE_SECURE
  SESSION_COOKIE_TRUSTED_URL HTTP_PROXY HTTPS_PROXY NO_PROXY)
check_app_env() {
  local file="$base/etc/app.env" line key value n=0 k bad=
  local -A seen=() allowed=() pattern=()
  pattern[SQL_DSN]='^postgres(ql)?://[^@]+@127\.0\.0\.1:5432/aiscoo_admin(\?[A-Za-z0-9_.=&%-]+)?$'
  pattern[SESSION_SECRET]='^[0-9a-fA-F]{64}$'
  pattern[PORT]='^3000$'
  pattern[XCODE_INTEGRATION_ENABLED]='^(true|false)$'
  # The admin service is always on 8443 (443 belongs to the website); an origin without the port would break cookies.
  pattern[XCODE_PUBLIC_ORIGIN]='^https://[A-Za-z0-9.-]+:8443$'
  pattern[SESSION_COOKIE_SECURE]='^(true|false)$'
  pattern[SESSION_COOKIE_TRUSTED_URL]='^https://[A-Za-z0-9.-]+:8443$'
  pattern[HTTP_PROXY]='^http://127\.0\.0\.1:[0-9]{1,5}$'
  pattern[HTTPS_PROXY]='^http://127\.0\.0\.1:[0-9]{1,5}$'
  pattern[NO_PROXY]='^.+$'
  local shape='^([A-Z_][A-Z0-9_]*)=([^[:space:]"'\''\\$#]+)$'
  for k in "${APP_ENV_KEYS[@]}"; do allowed[$k]=1; done
  [[ -e $file || -L $file ]] || die 'etc/app.env is missing'
  [[ -f $file && ! -L $file && -O $file ]] || die 'etc/app.env must be a regular file owned by this account'
  [[ $(stat -c %a "$file") == 600 ]] || die 'etc/app.env must have mode 0600'
  while IFS= read -r line || [[ -n $line ]]; do
    n=$((n + 1))
    [[ -z $line || $line == '#'* ]] && continue
    [[ $line =~ $shape ]] || die "etc/app.env line $n is not KEY=VALUE (no quotes, spaces, \$, # or backslash allowed)"
    key=${BASH_REMATCH[1]} value=${BASH_REMATCH[2]}
    [[ -n ${allowed[$key]:-} ]] || die "etc/app.env line $n: key is not one of the expected variables"
    [[ -z ${seen[$key]:-} ]] || die "etc/app.env line $n: duplicate key $key"
    seen[$key]=1
    [[ $value =~ ${pattern[$key]} ]] || bad+=" $key"
  done < "$file"
  for k in "${APP_ENV_KEYS[@]}"; do [[ -n ${seen[$k]:-} ]] || bad+=" missing:$k"; done
  [[ -z $bad ]] || die "etc/app.env invalid, check these keys (values not shown):$bad"
}

# Waits until PostgreSQL accepts TCP connections on 127.0.0.1:5432. Returns 1 after PG_WAIT_TIMEOUT seconds.
wait_postgres() {
  local deadline=$((SECONDS + PG_WAIT_TIMEOUT))
  while ! timeout 2 bash -c 'exec 3<>/dev/tcp/127.0.0.1/5432' 2>/dev/null; do
    (( SECONDS < deadline )) || return 1
    sleep 1
  done
}

# Prints the version reported by the running service, or nothing.
local_version() {
  python3 -I -c '
import json, urllib.request
try:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    data = json.loads(opener.open("http://127.0.0.1:3000/api/status", timeout=3).read(1 << 20).decode())
    print(data["data"]["version"] if data.get("success") is True else "")
except Exception:
    print("")
' 2>/dev/null || true
}

# Version string a binary reports for itself; the environment is cleared so VERSION cannot override it.
binary_version() {
  local binary=$1 dir out
  dir=$(mktemp -d "$base/staging/vcheck.XXXXXXXX")
  out=$(cd "$dir" && timeout 20 env -i PATH=/usr/bin:/bin HOME="$dir" "$binary" --version 2>/dev/null) || out=
  rm -rf "$dir"
  printf '%s' "$out"
}

# Waits until the service answers with the expected version. Returns 1 on failure; never stops or kills anything.
wait_healthy() {
  local expected=$1 deadline state seen
  deadline=$((SECONDS + HEALTH_TIMEOUT))
  while (( SECONDS < deadline )); do
    state=$(ctl is-active 2>/dev/null || true)
    case $state in
      failed|inactive) log "service is $state during startup"; return 1 ;;
    esac
    seen=$(local_version)
    [[ $seen == "$expected" ]] && return 0
    sleep 2
  done
  log 'service did not report the expected version before the timeout'
  return 1
}

# Keep the evidence for a failed switch. Does not print environment or credentials.
save_evidence() {
  local id=$1
  install -d -m 700 "$base/state/evidence"
  {
    printf 'release: %s\ntime: %s\n\n' "$id" "$(date -u +%FT%TZ)"
    ctl status --no-pager 2>&1 | head -40 || true
    printf '\n--- journal ---\n'
    journalctl --user -u "$unit" -n 200 --no-pager 2>&1 || true
  } > "$base/state/evidence/$id.log" 2>&1 || true
}

# Atomically repoint "current". Records the old target as "previous" when it differs.
switch_current() {
  local target=$1 old=
  [[ -L $base/current ]] && old=$(readlink "$base/current")
  ln -sfn "$target" "$base/current.next"
  mv -T -f "$base/current.next" "$base/current"
  if [[ -n $old && $old != "$target" ]]; then
    ln -sfn "$old" "$base/previous.next"
    mv -T -f "$base/previous.next" "$base/previous"
  fi
}
