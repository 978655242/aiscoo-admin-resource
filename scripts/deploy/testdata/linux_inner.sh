#!/usr/bin/env bash
# Runs inside the disposable container as root; every deploy action runs as the unprivileged user "magpie".
set -uo pipefail
pass=0 fail=0
ok()  { pass=$((pass + 1)); printf 'PASS %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf 'FAIL %s\n' "$1"; [[ -z ${2:-} ]] || printf '     %s\n' "$2"; }
check() { # name, then a command that must succeed
  local name=$1; shift
  if "$@"; then ok "$name"; else bad "$name"; fi
}

SHA1=1111111111111111111111111111111111111111 SHA2=2222222222222222222222222222222222222222
SHA3=3333333333333333333333333333333333333333 SHA4=4444444444444444444444444444444444444444
SHA5=5555555555555555555555555555555555555555 SHA6=6666666666666666666666666666666666666666
SHA7=7777777777777777777777777777777777777777
H=/home/magpie B=/home/magpie/aiscoo-admin
ver() { printf 'release-%s' "${1:0:12}"; }   # what the build embeds: release- + the first 12 hex digits
ADMIN_ORIGIN=https://112.124.202.60:8443

useradd -m -s /bin/bash magpie
usermod -p '*' magpie
mkdir -p /run/user/"$(id -u magpie)" /run/sshd
chown magpie: /run/user/"$(id -u magpie)"; chmod 700 /run/user/"$(id -u magpie)"

# ---- stub user systemd: the real systemd 255 user manager cannot run in this container ----
cat > /usr/local/bin/systemctl <<'STUB'
#!/bin/bash
[[ ${1:-} == --user ]] && shift
home=$(getent passwd "$(id -u)" | cut -d: -f6); pidf=$home/.stub.pid
alive() { [[ -s $pidf ]] && kill -0 "$(cat "$pidf")" 2>/dev/null; }
halt() { alive && { kill "$(cat "$pidf")"; for _ in 1 2 3 4 5 6 7 8 9 10; do alive || break; sleep 0.2; done; }; rm -f "$pidf"; }
case ${1:-} in
  daemon-reload) ;;
  is-active) if alive; then echo active; else echo failed; exit 3; fi ;;
  stop) halt ;;
  start|restart) halt
    cd "$home/aiscoo-admin/state/data" || exit 1
    # ExecStartPre: a failure means the unit fails to start and nothing runs (Restart=no).
    env -i PATH=/usr/local/bin:/usr/bin:/bin HOME="$home/aiscoo-admin/state" "$home/aiscoo-admin/bin/prestart.sh" >> "$home/svc.log" 2>&1 || exit 1
    setsid env -i PATH=/usr/local/bin:/usr/bin:/bin HOME="$home/aiscoo-admin/state" TMPDIR="$home/aiscoo-admin/state/tmp" "$home/aiscoo-admin/current" --port 3000 --log-dir "$home/aiscoo-admin/state/logs" >> "$home/svc.log" 2>&1 < /dev/null &
    echo $! > "$pidf" ;;
  status) echo "stub unit status: $(alive && echo active || echo failed)" ;;
  *) echo "stub: unsupported $*" >&2; exit 1 ;;
esac
STUB
printf '#!/bin/sh\ntail -n 50 "$HOME/svc.log" 2>/dev/null\n' > /usr/local/bin/journalctl
chmod +x /usr/local/bin/systemctl /usr/local/bin/journalctl

as_magpie() { runuser -u magpie -- env -i HOME=$H PATH=/usr/bin:/bin "$@"; }
# CI hashes its checkout (/src); the server hashes its installed copy. They must be equal right after an install.
ci_hash() { bash /src/entry-hash.sh /src /src/aiscoo-admin.service; }
send() { # sha run attempt file [digest] [size] [entry-hash]  -> speaks the real protocol into receive.sh
  local sha=$1 run=$2 att=$3 file=$4 digest size eh
  digest=${5:-$(sha256sum "$file" | cut -d ' ' -f1)}; size=${6:-$(stat -c %s "$file")}; eh=${7:-$(ci_hash)}
  { printf 'deploy %s %s %s %s %s %s\n' "$sha" "$run" "$att" "$digest" "$size" "$eh"; cat "$file"; } | as_magpie "$B/bin/receive.sh"
}
cur() { readlink "$B/current" 2>/dev/null | xargs -r basename; }
latest() { cat "$B/state/latest-run" 2>/dev/null; }
releases() { ls -1 "$B/releases" | sort | tr '\n' ' '; }
served() { as_magpie python3 -I -c 'import json,urllib.request as u;print(json.loads(u.urlopen("http://127.0.0.1:3000/api/status",timeout=3).read())["data"]["version"])' 2>/dev/null; }
staging_clean() { [[ -z $(ls -A "$B/staging") ]]; }
# Run a deploy that must be rejected: no state change at all, nothing left in staging.
reject() { # name expected-text sha run attempt file [digest] [size]
  local name=$1 text=$2; shift 2
  local before_cur before_latest before_rel out rc
  before_cur=$(cur); before_latest=$(latest); before_rel=$(releases)
  out=$(send "$@" 2>&1); rc=$?
  if (( rc != 0 )) && [[ $out == *"$text"* ]] && [[ $(cur) == "$before_cur" && $(latest) == "$before_latest" && $(releases) == "$before_rel" ]] && staging_clean; then
    ok "$name"
  else
    bad "$name" "rc=$rc out=$out cur=$(cur) latest=$(latest)"
  fi
}

# A stand-in for PostgreSQL: anything listening on 127.0.0.1:5432. Started and stopped by the tests.
pg_up()   { pg_down; setsid python3 -c 'import socket,time
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("127.0.0.1",5432)); s.listen(16)
while True:
    c,_=s.accept(); c.close()' >/dev/null 2>&1 < /dev/null & echo $! > /tmp/pg.pid; disown; sleep 0.5; }
pg_down() { [[ -s /tmp/pg.pid ]] && kill "$(cat /tmp/pg.pid)" 2>/dev/null; rm -f /tmp/pg.pid; sleep 0.3; return 0; }
FIX_PW=FIXTUREPWd0ntl3ak FIX_SECRET=$(printf 'ab12cd34%.0s' 1 2 3 4 5 6 7 8)   # fixture "secrets", never real
setup_out() { runuser -u magpie -- env HOME=$H bash /src/setup-magpie.sh "$@" 2>&1; }
no_leak() { [[ $1 != *"$FIX_PW"* && $1 != *"$FIX_SECRET"* && $1 != *ab12cd34* ]]; }

echo "== provisioning (setup-magpie.sh) =="
out=$(setup_out install); rc=$?
check 'setup install succeeds without app.env' test $rc -eq 0
check 'setup says app.env is still missing' bash -c "[[ '$out' == *'etc/app.env is still missing'* ]]"
check 'dirs are 0700 and owned by magpie' bash -c "[[ \$(stat -c '%a %U' $B $B/releases $B/state $B/state/tmp $B/state/logs $B/state/data $B/staging $B/etc $B/bin | sort -u) == '700 magpie' ]]"
check 'scripts installed read-only (0500)' bash -c "[[ \$(stat -c %a $B/bin/receive.sh $B/bin/common.sh $B/bin/rollback.sh $B/bin/prestart.sh) == \$'500\n500\n500\n500' ]]"
check 'user unit installed' test -f $H/.config/systemd/user/aiscoo-admin.service

echo "== app.env generation (make-app-env.sh, root, one-time) =="
mkdir /tmp/oldetc
printf 'SQL_DSN=postgresql://aiscoo:%s@127.0.0.1:5432/aiscoo_admin?sslmode=disable\n' "$FIX_PW" > /tmp/oldetc/database.env
printf '%s' "$FIX_SECRET" > /tmp/oldetc/session.secret
out=$(bash /src/make-app-env.sh /tmp/oldetc $B/etc/app.env magpie 2>&1); rc=$?
check 'make-app-env writes app.env' test $rc -eq 0
check 'make-app-env prints no secret value' no_leak "$out"
check 'app.env is 0600 owned by magpie with 10 keys' bash -c "[[ \$(stat -c '%a %U' $B/etc/app.env) == '600 magpie' && \$(grep -c . $B/etc/app.env) == 10 ]]"
check 'app.env has every required key, no CRYPTO_SECRET' bash -c "for k in SQL_DSN SESSION_SECRET PORT XCODE_INTEGRATION_ENABLED XCODE_PUBLIC_ORIGIN SESSION_COOKIE_SECURE SESSION_COOKIE_TRUSTED_URL HTTP_PROXY HTTPS_PROXY NO_PROXY; do grep -q \"^\$k=\" $B/etc/app.env || exit 1; done; ! grep -q CRYPTO_SECRET $B/etc/app.env"
check 'app.env fixed values match the previous service, origins carry :8443' bash -c "grep -qx 'XCODE_PUBLIC_ORIGIN=https://112.124.202.60:8443' $B/etc/app.env && grep -qx 'SESSION_COOKIE_TRUSTED_URL=https://112.124.202.60:8443' $B/etc/app.env && grep -qx 'HTTPS_PROXY=http://127.0.0.1:17894' $B/etc/app.env && grep -qx 'NO_PROXY=127.0.0.1,localhost,::1' $B/etc/app.env && grep -qx 'SESSION_COOKIE_SECURE=true' $B/etc/app.env && grep -qx 'PORT=3000' $B/etc/app.env"
check 'app.env SESSION_SECRET is KEY=VALUE of the bare secret' bash -c "grep -qx 'SESSION_SECRET=$FIX_SECRET' $B/etc/app.env"
out=$(bash /src/make-app-env.sh /tmp/oldetc $B/etc/app.env magpie 2>&1); check 'make-app-env refuses to overwrite' bash -c "[[ $? -ne 0 || '$out' == *'refusing'* ]]"
cp $B/etc/app.env /tmp/app.env.good
printf 'abc\n' > /tmp/oldetc/session.secret
out=$(bash /src/make-app-env.sh /tmp/oldetc /tmp/oldetc/x.env magpie 2>&1); rc=$?
check 'make-app-env rejects a malformed session.secret without echoing it' bash -c "[[ $rc -ne 0 && ! -e /tmp/oldetc/x.env && '$out' == *'64 hexadecimal'* ]]"
printf '%s' "$FIX_SECRET" > /tmp/oldetc/session.secret
printf 'SQL_DSN=postgresql://u:%s@10.0.0.9:5432/aiscoo_admin\n' "$FIX_PW" > /tmp/oldetc/database.env
out=$(bash /src/make-app-env.sh /tmp/oldetc /tmp/oldetc/x.env magpie 2>&1); rc=$?
check 'make-app-env rejects a DSN pointing elsewhere without echoing it' bash -c "[[ $rc -ne 0 && ! -e /tmp/oldetc/x.env ]]" && check 'make-app-env error shows no DSN' no_leak "$out"
rm -rf /tmp/oldetc
out=$(runuser -u magpie -- env HOME=$H bash /src/make-app-env.sh 2>&1); check 'make-app-env refuses to run as non-root' bash -c "[[ '$out' == *'run as root'* ]]"

echo "== app.env format check (read-only, never echoes values) =="
reset_env() { install -o magpie -g magpie -m 600 /tmp/app.env.good $B/etc/app.env; }
expect_bad() { # name text-that-must-appear ; app.env already mutated
  local out rc
  out=$(setup_out check-env); rc=$?
  if (( rc != 0 )) && [[ $out == *"$2"* ]] && no_leak "$out"; then ok "$1"; else bad "$1" "rc=$rc out=$out"; fi
  reset_env
}
out=$(setup_out check-env); rc=$?
check 'check-env passes on the generated file' bash -c "[[ $rc -eq 0 && '$out' == *'values not shown'* ]]"
check 'check-env output has no secret' no_leak "$out"
out=$(setup_out install); rc=$?
check 'setup install with valid app.env reports the check and no value' bash -c "[[ $rc -eq 0 && '$out' == *'format check passed'* ]]" ; check 'install output has no secret' no_leak "$out"
grep -v '^NO_PROXY=' /tmp/app.env.good > $B/etc/app.env; expect_bad 'missing key reported by name' 'missing:NO_PROXY'
grep -v '^SESSION_SECRET=' /tmp/app.env.good > $B/etc/app.env; expect_bad 'missing SESSION_SECRET reported, value of DSN not shown' 'missing:SESSION_SECRET'
chmod 644 $B/etc/app.env; expect_bad 'mode 0644 rejected' 'mode 0600'
chmod 660 $B/etc/app.env; expect_bad 'mode 0660 rejected' 'mode 0600'
chown root: $B/etc/app.env; expect_bad 'foreign owner rejected' 'owned by this account'
mv $B/etc/app.env $B/etc/real.env; ln -s real.env $B/etc/app.env; expect_bad 'symlinked app.env rejected' 'owned by this account'
rm -f $B/etc/real.env
rm $B/etc/app.env; expect_bad 'missing app.env rejected' 'app.env is missing'
sed 's#@127.0.0.1:5432#@10.1.2.3:5432#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'DSN with another host rejected, value not shown' 'SQL_DSN'
sed 's#/aiscoo_admin#/otherdb#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'DSN with another database rejected' 'SQL_DSN'
sed 's#^SQL_DSN=postgresql://#SQL_DSN=mysql://#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'DSN with another scheme rejected' 'SQL_DSN'
sed 's#?sslmode=disable##' /tmp/app.env.good > $B/etc/app.env; setup_out check-env >/dev/null; check 'DSN without sslmode accepted' test $? -eq 0; reset_env
sed "s#^SESSION_SECRET=.*#SESSION_SECRET=${FIX_SECRET:0:60}#" /tmp/app.env.good > $B/etc/app.env; expect_bad 'short SESSION_SECRET rejected, value not shown' 'SESSION_SECRET'
sed "s#^SESSION_SECRET=.*#SESSION_SECRET=zz${FIX_SECRET:2}#" /tmp/app.env.good > $B/etc/app.env; expect_bad 'non-hex SESSION_SECRET rejected' 'SESSION_SECRET'
sed "s#^SESSION_SECRET=\(.*\)#SESSION_SECRET=\"\1\"#" /tmp/app.env.good > $B/etc/app.env; expect_bad 'quoted value rejected, value not shown' 'line 2'
sed "s#^PORT=3000#PORT=3000 \# note#" /tmp/app.env.good > $B/etc/app.env; expect_bad 'trailing comment/space rejected' 'line 3'
{ cat /tmp/app.env.good; printf 'CRYPTO_SECRET=%s\n' "$FIX_SECRET"; } > $B/etc/app.env; expect_bad 'CRYPTO_SECRET in app.env rejected, value not shown' 'not one of the expected'
{ cat /tmp/app.env.good; printf 'PORT=3000\n'; } > $B/etc/app.env; expect_bad 'duplicate key rejected' 'duplicate key PORT'
{ cat /tmp/app.env.good; printf 'FOO=$(id)\n'; } > $B/etc/app.env; expect_bad 'shell-looking value rejected' 'line 11'
sed 's#^PORT=3000#PORT=3001#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'wrong PORT rejected' 'PORT'
sed 's#^XCODE_PUBLIC_ORIGIN=.*#XCODE_PUBLIC_ORIGIN=https://112.124.202.60#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'XCODE_PUBLIC_ORIGIN without :8443 rejected' 'XCODE_PUBLIC_ORIGIN'
sed 's#^SESSION_COOKIE_TRUSTED_URL=.*#SESSION_COOKIE_TRUSTED_URL=https://112.124.202.60:443#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'SESSION_COOKIE_TRUSTED_URL on :443 rejected' 'SESSION_COOKIE_TRUSTED_URL'
sed 's#^XCODE_PUBLIC_ORIGIN=.*#XCODE_PUBLIC_ORIGIN=http://112.124.202.60:8443#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'plain-http origin rejected' 'XCODE_PUBLIC_ORIGIN'
sed 's#^HTTP_PROXY=.*#HTTP_PROXY=http://proxy.example:80#' /tmp/app.env.good > $B/etc/app.env; expect_bad 'non-local proxy rejected' 'HTTP_PROXY'
out=$(setup_out check-env); check 'check-env passes again after the failure cases' test $? -eq 0
# receive.sh refuses to take an upload when app.env is bad
chmod 644 $B/etc/app.env
out=$(send $SHA1 10 1 /fixtures/ok1 2>&1); check 'receive.sh refuses a bad app.env before upload, no state change' bash -c "[[ '$out' == *'mode 0600'* && ! -e $B/state/latest-run && -z \$(ls -A $B/releases) ]]"
chmod 600 $B/etc/app.env
echo 'PORT=3000' > $B/etc/app.env; out=$(setup_out install); rc=$?
check 'setup install exits non-zero on an invalid app.env but installed the files' bash -c "[[ $rc -ne 0 && '$out' == *'installed under'* && '$out' == *invalid* ]]"
reset_env

ssh-keygen -q -t ed25519 -N '' -f /tmp/ci_key
ssh-keygen -q -t ed25519 -N '' -f /tmp/other_key
ssh-keygen -q -t rsa -b 2048 -N '' -f /tmp/rsa_key
cp /tmp/ci_key.pub /tmp/ci_key.pub.copy; chmod 644 /tmp/*.pub
out=$(runuser -u magpie -- env HOME=$H bash /src/setup-magpie.sh authorize /tmp/rsa_key.pub 2>&1); check 'authorize rejects non-ed25519 key' test $? -ne 0
out=$(runuser -u magpie -- env HOME=$H bash /src/setup-magpie.sh authorize /tmp/ci_key.pub 2>&1); check 'authorize adds ed25519 key' test $? -eq 0
out=$(runuser -u magpie -- env HOME=$H bash /src/setup-magpie.sh authorize /tmp/ci_key.pub 2>&1); check 'authorize refuses a duplicate key' test $? -ne 0
check 'authorized_keys has restrict + fixed command and mode 0600' bash -c "
  [[ \$(stat -c %a $H/.ssh/authorized_keys) == 600 && \$(wc -l < $H/.ssh/authorized_keys) == 1 ]] &&
  grep -q '^restrict,command=\"$B/bin/receive.sh\" ssh-ed25519 ' $H/.ssh/authorized_keys"

pg_up
echo "== entry point basics =="
out=$($B/bin/receive.sh </dev/null 2>&1); check 'refuses to run as root' bash -c "[[ '$out' == *'must not run as root'* ]]"
out=$(as_magpie env SSH_ORIGINAL_COMMAND='id' "$B/bin/receive.sh" </dev/null 2>&1); check 'refuses client-supplied command' bash -c "[[ '$out' == *'remote commands are not accepted'* ]]"
chmod 777 $B/staging
out=$(send $SHA1 10 1 /fixtures/ok1 2>&1); check 'refuses when a deploy dir is group/world writable' bash -c "[[ '$out' == *'writable by others'* ]] && [[ -z \$(ls $B/releases) ]]"
chmod 700 $B/staging
mv $B/releases $B/releases.real; ln -s releases.real $B/releases
out=$(send $SHA1 10 1 /fixtures/ok1 2>&1); check 'refuses a symlinked deploy dir' bash -c "[[ '$out' == *'symlinked directory'* ]]"
rm $B/releases; mv $B/releases.real $B/releases

echo "== protocol rejections (no state change, nothing left behind) =="
out=$(as_magpie "$B/bin/receive.sh" </dev/null 2>&1); check 'empty stdin rejected' bash -c "[[ '$out' == *'header missing'* ]]"
out=$(printf 'garbage\n' | as_magpie "$B/bin/receive.sh" 2>&1); check 'garbage header rejected' bash -c "[[ '$out' == *'invalid header'* ]]"
out=$(printf 'deploy %s 10 1 %064d 5 %064d extra\nabcde' $SHA1 0 0 | as_magpie "$B/bin/receive.sh" 2>&1); check 'header with extra field rejected' bash -c "[[ '$out' == *'invalid header'* ]]"
out=$(printf 'deploy %s 10 1 %064d 5 ../../etc/passwd\nabcde' $SHA1 0 | as_magpie "$B/bin/receive.sh" 2>&1); check 'client-supplied path rejected' bash -c "[[ '$out' == *'invalid header'* ]]"
out=$(printf 'deploy %s 10 1 %064d 5\n' ZZZ 0 | as_magpie "$B/bin/receive.sh" 2>&1); check 'bad sha rejected' bash -c "[[ '$out' == *'invalid header'* ]]"
out=$(printf 'deploy %s 10 1 %064d 0 %064d\n' $SHA1 0 0 | as_magpie "$B/bin/receive.sh" 2>&1); check 'zero size rejected' bash -c "[[ '$out' == *'invalid header'* ]]"
out=$(printf 'rollback %s 10 1 %064d 5 %064d\n' $SHA1 0 0 | as_magpie "$B/bin/receive.sh" 2>&1); check 'unknown operation rejected' bash -c "[[ '$out' == *'invalid header'* ]]"
out=$(python3 -c 'import sys;sys.stdout.write("deploy "+"a"*600+"\n")' | as_magpie "$B/bin/receive.sh" 2>&1); check 'oversized header rejected' bash -c "[[ '$out' == *'header too long'* || '$out' == *'invalid header'* ]]"
reject 'checksum mismatch rejected' 'checksum mismatch' $SHA1 10 1 /fixtures/ok1 "$(printf '%064d' 0)"
reject 'non-ELF file (correct checksum) rejected' 'not a Linux amd64 ELF' $SHA1 10 1 /fixtures/script
reject 'arm64 ELF (correct checksum) rejected' 'not a Linux amd64 ELF' $SHA6 10 1 /fixtures/arm64
reject 'upload shorter than announced rejected' 'upload shorter' $SHA1 10 1 /fixtures/ok1 '' "$(( $(stat -c %s /fixtures/ok1) + 100 ))"
reject 'data after announced size rejected' 'data after the announced' $SHA1 10 1 /fixtures/ok1 '' "$(( $(stat -c %s /fixtures/ok1) - 100 ))"
reject 'binary reporting another version rejected' 'does not report the announced version' $SHA5 10 1 /fixtures/mismatch
check 'nothing was admitted by any rejection above' bash -c "[[ -z '$(latest)' && -z '$(releases)' ]]"

echo "== happy path =="
out=$(send $SHA1 10 1 /fixtures/ok1 2>&1); rc=$?
check 'first deploy accepted' test $rc -eq 0
check 'current points at the new release' test "$(cur)" = "$SHA1-10-1"
check 'service serves the deployed version' test "$(served)" = "$(ver $SHA1)"
check 'latest-run and accepted recorded' bash -c "[[ '$(latest)' == '10 1' && \$(cat $B/state/accepted) == $SHA1-10-1 ]]"
check 'release file is read-only 0555' test "$(stat -c %a "$B/releases/$SHA1-10-1")" = 555
check 'staging is empty after success' staging_clean
check 'log has no secret material' bash -c "! grep -qi 'fixture\|SESSION_SECRET' $B/state/deploy.log"

echo "== entry script authority (header carries the entry hash; the server copy decides) =="
check 'CI entry hash equals the hash of the installed copy' test "$(ci_hash)" = "$(as_magpie "$B/bin/entry-hash.sh" "$B/bin" "$H/.config/systemd/user/aiscoo-admin.service")"
reject 'a wrong entry hash is refused, no state change' 'out of date' $SHA2 9000 1 /fixtures/ok2 '' '' "$(printf '%064d' 7)"
chmod u+w $B/bin/prestart.sh; printf '# tampered\n' >> $B/bin/prestart.sh; chmod 500 $B/bin/prestart.sh
reject 'an edited installed script no longer matches the CI hash' 'out of date' $SHA2 9000 1 /fixtures/ok2
install -m 500 -o magpie -g magpie /src/prestart.sh $B/bin/prestart.sh
cp $H/.config/systemd/user/aiscoo-admin.service /tmp/unit.good; printf '# edited\n' >> $H/.config/systemd/user/aiscoo-admin.service
reject 'an edited user unit no longer matches the CI hash' 'out of date' $SHA2 9000 1 /fixtures/ok2
install -m 600 -o magpie -g magpie /tmp/unit.good $H/.config/systemd/user/aiscoo-admin.service
check 'restored entry files match again' test "$(ci_hash)" = "$(as_magpie "$B/bin/entry-hash.sh" "$B/bin" "$H/.config/systemd/user/aiscoo-admin.service")"
check 'entry-hash.sh is installed read-only by setup' test "$(stat -c %a $B/bin/entry-hash.sh)" = 500
out=$(printf 'deploy %s 9000 1 %064d 5\nabcde' $SHA2 0 | as_magpie "$B/bin/receive.sh" 2>&1); check 'a header without the entry hash is invalid' bash -c "[[ '$out' == *'invalid header'* ]]"
reject 'a binary reporting the bare 40-hex sha (not release-<12>) is refused' 'does not report the announced version' $SHA7 9001 1 /fixtures/barever

echo "== migration guard: a foreign process on 3000 while the unit is stopped =="
as_magpie /usr/local/bin/systemctl --user stop
setsid python3 -c 'import socket,time
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("127.0.0.1",3000)); s.listen(16)
while True:
    c,_=s.accept(); c.close()' >/dev/null 2>&1 < /dev/null & echo $! > /tmp/old3000.pid; disown; sleep 0.5
reject 'port 3000 held by a foreign process (old service) refuses the deploy, nothing consumed' 'port 3000 is held by another process' $SHA2 9100 1 /fixtures/ok2
kill "$(cat /tmp/old3000.pid)"; rm -f /tmp/old3000.pid; sleep 0.5
as_magpie /usr/local/bin/systemctl --user start
check 'after the foreign process is gone the unit starts again and the same run is admitted later' bash -c "sleep 1; [[ '$(served)' == $(ver $SHA1) && ! -e $B/state/blocked ]]"

echo "== service environment and start-up order (stubbed systemd; the real unit is NOT verified) =="
envdump=$(as_magpie bash -c "tr '\\0' '\\n' < /proc/\$(cat $H/.stub.pid)/environ" 2>/dev/null)
check 'service process gets the unit HOME/TMPDIR/PATH' bash -c "[[ '$envdump' == *HOME=$B/state* && '$envdump' == *TMPDIR=$B/state/tmp* && '$envdump' == *PATH=/usr/local/bin:/usr/bin:/bin* ]]"
check 'state/tmp exists for TMPDIR' test -d $B/state/tmp
pg_down
printf 'PG_WAIT_TIMEOUT=3\n' > $B/etc/deploy.conf; chown magpie: $B/etc/deploy.conf
t0=$SECONDS; out=$(as_magpie "$B/bin/prestart.sh" 2>&1); rc=$?; el=$((SECONDS - t0))
check 'prestart fails non-zero after the wait when PostgreSQL is down' bash -c "[[ $rc -ne 0 && $el -ge 2 && $el -le 8 && '$out' == *'did not accept connections'* ]]"
check 'prestart failure output shows no secret' no_leak "$out"
before=$(cat $H/.stub.pid)
as_magpie /usr/local/bin/systemctl --user stop; as_magpie /usr/local/bin/systemctl --user start; rc=$?
check 'unit start fails (ExecStartPre) and no service process runs when PostgreSQL is down' bash -c "[[ $rc -ne 0 && ! -e $H/.stub.pid ]] && ! pgrep -u magpie -f aiscoo-admin/current >/dev/null"
( sleep 1; pg_up ) &
t0=$SECONDS; out=$(as_magpie "$B/bin/prestart.sh" 2>&1); rc=$?; el=$((SECONDS - t0)); wait
check 'prestart waits and then succeeds once PostgreSQL appears' bash -c "[[ $rc -eq 0 && $el -ge 1 && $el -le 6 ]]"
t0=$SECONDS; out=$(as_magpie "$B/bin/prestart.sh" 2>&1); rc=$?
check 'prestart returns immediately when PostgreSQL is up' bash -c "[[ $rc -eq 0 && $((SECONDS - t0)) -le 2 ]]"
chmod 644 $B/etc/app.env; t0=$SECONDS; out=$(as_magpie "$B/bin/prestart.sh" 2>&1); rc=$?
check 'prestart fails fast on a bad app.env (no waiting)' bash -c "[[ $rc -ne 0 && '$out' == *'mode 0600'* && $((SECONDS - t0)) -le 2 ]]"
chmod 600 $B/etc/app.env
for v in 0 81 abc; do printf 'PG_WAIT_TIMEOUT=%s\n' $v > $B/etc/deploy.conf; out=$(as_magpie "$B/bin/prestart.sh" 2>&1); check "PG_WAIT_TIMEOUT=$v rejected" test $? -ne 0; done
rm -f $B/etc/deploy.conf
as_magpie /usr/local/bin/systemctl --user start
check 'service runs again after PostgreSQL is back' bash -c "sleep 1; [[ '$(served)' == $(ver $SHA1) ]]"

echo "== stale / duplicate runs =="
reject 'older run number rejected' 'stale or duplicate' $SHA2 9 1 /fixtures/ok2
reject 'same run and attempt rejected' 'stale or duplicate' $SHA2 10 1 /fixtures/ok2
out=$(send $SHA2 10 2 /fixtures/ok2 2>&1); rc=$?
check 're-run of newest run (higher attempt) accepted' bash -c "[[ $rc -eq 0 && '$(cur)' == $SHA2-10-2 && '$(served)' == $(ver $SHA2) ]]"
check 'previous link recorded' test "$(readlink $B/previous | xargs basename)" = "$SHA1-10-1"
reject 'stale attempt of the same run rejected' 'stale or duplicate' $SHA1 10 1 /fixtures/ok1

echo "== lock =="
printf 'LOCK_TIMEOUT=2\n' > $B/etc/deploy.conf; chown magpie: $B/etc/deploy.conf
( as_magpie bash -c "exec 9>$B/state/deploy.lock; flock -x 9; sleep 7" ) &
holder=$!; sleep 1
reject 'deploy fails while another holds the lock' 'lock timed out' $SHA1 11 1 /fixtures/ok1
wait $holder
rm -f $B/etc/deploy.conf
out=$(send $SHA1 11 1 /fixtures/ok1 2>&1); check 'deploy after lock release accepted' bash -c "[[ '$(cur)' == $SHA1-11-1 ]]"
# Two simultaneous deploys: they serialise; the end state is consistent.
( send $SHA2 20 1 /fixtures/ok2 > /tmp/par1.out 2>&1; echo $? > /tmp/par1.rc ) &
( send $SHA1 21 1 /fixtures/ok1 > /tmp/par2.out 2>&1; echo $? > /tmp/par2.rc ) &
wait
check 'parallel deploys: at least one wins, end state consistent' bash -c "
  [[ \$(cat /tmp/par1.rc) == 0 || \$(cat /tmp/par2.rc) == 0 ]] &&
  [[ \$(cat $B/state/accepted) == \$(readlink $B/current | xargs basename) ]] &&
  [[ \$(cat $B/state/accepted | cut -d- -f2) == \$(cut -d' ' -f1 $B/state/latest-run) ]] &&
  [[ -z \$(ls -A $B/staging) && ! -e $B/state/blocked ]]"
run=$(cut -d ' ' -f1 "$B/state/latest-run")

echo "== failed start after the switch (migration failure) =="
prev_good=$(cur); prev_sha=$(ver "${prev_good%%-*}")
out=$(send $SHA3 30 1 /fixtures/crash 2>&1); rc=$?
check 'crashing release makes the deploy fail' test $rc -ne 0
check 'service left as is: no automatic rollback' test "$(cur)" = "$SHA3-30-1"
check 'blocked marker written' test "$(cat $B/state/blocked 2>/dev/null)" = "$SHA3-30-1"
check 'evidence file saved' test -s "$B/state/evidence/$SHA3-30-1.log"
check 'failed release not recorded as accepted' test "$(cat $B/state/accepted)" = "$prev_good"
reject 'further deploys refused while blocked' 'unresolved' $SHA2 31 1 /fixtures/ok2
out=$(as_magpie "$B/bin/rollback.sh" to previous 2>&1); check 'rollback without the confirmation word refused' bash -c "[[ '$out' == *usage* ]]"
out=$(as_magpie "$B/bin/rollback.sh" to ../../etc/passwd I_HAVE_VERIFIED_SCHEMA_COMPATIBILITY 2>&1); check 'rollback with a path-like id refused' bash -c "[[ '$out' == *'invalid release id'* ]]"
out=$(as_magpie "$B/bin/rollback.sh" list 2>&1); check 'rollback list works' bash -c "[[ '$out' == *'blocked:  $SHA3-30-1'* ]]"
out=$(as_magpie "$B/bin/rollback.sh" to previous I_HAVE_VERIFIED_SCHEMA_COMPATIBILITY 2>&1); rc=$?
check 'manual rollback to previous binary succeeds' test $rc -eq 0
check 'rollback serves the old version and clears blocked' bash -c "[[ '$(served)' == $prev_sha && ! -e $B/state/blocked && '$(cur)' == $prev_good ]]"
out=$(send $SHA2 31 1 /fixtures/ok2 2>&1); check 'deploy works after rollback' bash -c "[[ '$(cur)' == $SHA2-31-1 && '$(served)' == $(ver $SHA2) ]]"

echo "== service up but reports another version =="
printf 'HEALTH_TIMEOUT=6\n' > $B/etc/deploy.conf; chown magpie: $B/etc/deploy.conf
out=$(send $SHA4 40 1 /fixtures/wrongstatus 2>&1); rc=$?
check 'health timeout fails the deploy and blocks' bash -c "[[ $rc -ne 0 && -e $B/state/blocked && '$out' == *'FAILED after switch'* ]]"
as_magpie "$B/bin/rollback.sh" to previous I_HAVE_VERIFIED_SCHEMA_COMPATIBILITY >/dev/null 2>&1
check 'rollback recovers' bash -c "[[ ! -e $B/state/blocked && '$(served)' == $(ver $SHA2) ]]"
rm -f $B/etc/deploy.conf
out=$(as_magpie "$B/bin/rollback.sh" clear-blocked 2>&1); check 'clear-blocked works' test $? -eq 0

echo "== optional pre-migration backup hook (default off) =="
check 'backup is off by default and the log says so' bash -c "grep -q 'pre-migration backup is NOT enabled' $B/state/deploy.log"
before=$(cur)
as_magpie touch $B/etc/backup.enabled
reject 'backup enabled without hook is rejected, no half release' 'bin/pre-migration-backup is missing' $SHA1 50 1 /fixtures/ok1
printf '#!/bin/sh\nexit 1\n' > $B/bin/pre-migration-backup; chown magpie: $B/bin/pre-migration-backup; chmod 700 $B/bin/pre-migration-backup
out=$(send $SHA1 51 1 /fixtures/ok1 2>&1); rc=$?
check 'failing backup: deploy refused, old version running again, not blocked' bash -c "[[ $rc -ne 0 && '$(cur)' == $before && '$(served)' == $(ver $SHA2) && ! -e $B/state/blocked && ! -e $B/releases/$SHA1-51-1 ]]"
printf '#!/bin/sh\ntouch "$HOME/backup-ran"\nexit 0\n' > $B/bin/pre-migration-backup
out=$(send $SHA1 52 1 /fixtures/ok1 2>&1); rc=$?
check 'succeeding backup hook runs before the switch' bash -c "[[ $rc -eq 0 && -e $H/backup-ran && '$(cur)' == $SHA1-52-1 ]]"
rm -f $B/etc/backup.enabled $B/bin/pre-migration-backup

echo "== retention =="
for i in 1 2 3 4 5 6 7; do
  f=/fixtures/ok1; s=$SHA1; (( i % 2 )) && { f=/fixtures/ok2; s=$SHA2; }
  send $s $((60 + i)) 1 $f >/dev/null 2>&1
done
check 'old releases pruned, current and previous kept' bash -c "[[ \$(ls $B/releases | wc -l) -le 7 && -e $B/releases/\$(readlink $B/current | xargs basename) && -e $B/releases/\$(readlink $B/previous | xargs basename) ]]"

echo "== probe.py against the running fake app =="
pg_down   # the probe requires 5432 to be closed on this address
export AISCOO_PROBE_ALLOW_HTTP=1 AISCOO_PROBE_ATTEMPTS=1 AISCOO_PROBE_DELAY=0
cur_sha=$SHA2; [[ $(served) == "$(ver $SHA1)" ]] && cur_sha=$SHA1
out=$(python3 -I /src/probe.py http://127.0.0.1:3000 "$cur_sha" 2>&1); check 'probe passes against current version (basic mode)' bash -c "[[ '$out' == *'ACCEPTANCE: basic'* ]]"; printf '%s\n' "$out" | tail -3
out=$(python3 -I /src/probe.py http://127.0.0.1:3000 $SHA5 2>&1); check 'probe fails on a version mismatch' bash -c "[[ \$? -ne 0 || '$out' == *failed* ]] && [[ '$out' != *ACCEPTANCE* ]]"

echo "== real sshd with the restricted key =="
pg_up
ssh-keygen -A >/dev/null 2>&1
/usr/sbin/sshd -p 2222 -o ListenAddress=127.0.0.1 -o UsePAM=no -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -o AllowUsers=magpie -o PidFile=/run/sshd2222.pid
sleep 1
{ printf '[127.0.0.1]:2222 '; cut -d ' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub; } > /tmp/known_hosts
{ printf '[127.0.0.1]:2222 '; cut -d ' ' -f1,2 /tmp/other_key.pub; } > /tmp/bad_known_hosts
S="ssh -p 2222 -T -i /tmp/ci_key -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/tmp/known_hosts -o ConnectTimeout=5"
out=$($S magpie@127.0.0.1 id </dev/null 2>&1); check 'arbitrary remote command is not executed' bash -c "[[ '$out' == *'remote commands are not accepted'* && '$out' != *uid=* ]]"
out=$($S magpie@127.0.0.1 'bash -c "id; ls /"' </dev/null 2>&1); check 'shell command via ssh is not executed' bash -c "[[ '$out' != *uid=* && '$out' != *bin* ]]"
out=$(ssh -p 2222 -tt -i /tmp/ci_key -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/tmp/known_hosts magpie@127.0.0.1 </dev/null 2>&1); check 'pty request refused' bash -c "[[ '$out' == *'PTY allocation request failed'* ]]"
out=$(ssh -p 2222 -T -i /tmp/other_key -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/tmp/known_hosts -o ConnectTimeout=5 magpie@127.0.0.1 true </dev/null 2>&1); check 'unauthorised key denied' bash -c "[[ '$out' == *'Permission denied'* ]]"
out=$(ssh -p 2222 -T -i /tmp/ci_key -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/tmp/bad_known_hosts -o ConnectTimeout=5 magpie@127.0.0.1 true </dev/null 2>&1); check 'wrong pinned host key aborts the connection' bash -c "[[ '$out' == *'Host key verification failed'* || '$out' == *'REMOTE HOST IDENTIFICATION HAS CHANGED'* ]]"
# Port forwarding must be refused by "restrict": keep a session open and try to use the forward.
( sleep 6 | $S -L 19000:127.0.0.1:3000 magpie@127.0.0.1 >/tmp/fwd.out 2>&1 ) &
sleep 2
resp=$(python3 -c '
import socket
s=socket.create_connection(("127.0.0.1",19000),timeout=3); s.sendall(b"GET /api/status HTTP/1.0\r\n\r\n")
try: print(s.recv(4096).decode(errors="replace"))
except Exception as e: print("closed")' 2>&1)
wait
check 'local port forwarding refused' bash -c "[[ '$resp' != *version* ]]"
before_rel=$(releases)
out=$({ printf 'deploy %s 80 1 %s %s %s\n' $SHA1 "$(sha256sum /fixtures/ok1 | cut -d' ' -f1)" "$(stat -c %s /fixtures/ok1)" "$(ci_hash)"; cat /fixtures/ok1; } | $S magpie@127.0.0.1 2>&1); rc=$?
check 'real deploy over restricted ssh succeeds' bash -c "[[ $rc -eq 0 && '$(cur)' == $SHA1-80-1 && '$(served)' == $(ver $SHA1) ]]"
out=$({ printf 'deploy %s 70 1 %s %s %s\n' $SHA1 "$(sha256sum /fixtures/ok1 | cut -d' ' -f1)" "$(stat -c %s /fixtures/ok1)" "$(ci_hash)"; cat /fixtures/ok1; } | $S magpie@127.0.0.1 2>&1); rc=$?
check 'stale run over ssh fails with non-zero exit status' bash -c "[[ $rc -ne 0 && '$out' == *'stale or duplicate'* ]]"
kill "$(cat /run/sshd2222.pid)" 2>/dev/null
pg_down
pkill -u magpie -f 'aiscoo-admin/current' 2>/dev/null; true

printf '\n%d passed, %d failed\n' $pass $fail
(( fail == 0 ))
