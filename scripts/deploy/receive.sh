#!/usr/bin/env bash
# Forced command for the CI deploy key (authorized_keys: restrict,command="<this file>").
# Protocol on stdin: one header line, then exactly <size> bytes of the binary, then EOF.
#   deploy <sha1-40hex> <run-number> <run-attempt> <sha256-64hex> <size-bytes> <entry-hash-64hex>
# <entry-hash> is entry-hash.sh over the scripts and unit CI has in its checkout. The scripts installed on the server
# are the authority: when their own hash differs, the deployment is refused before anything is written, so a stale
# or hand-edited copy can never publish silently. The new binary must report release-<first 12 hex of the sha>.
# The client never supplies a path or a command. No sudo; everything runs as the service account.
set -Eeuo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=common.sh
source "$here/common.sh"

[[ -z ${SSH_ORIGINAL_COMMAND:-} ]] || die 'remote commands are not accepted'
preflight
load_conf
# The service refuses to start with a malformed app.env; refuse before the upload and before the run number is consumed.
check_app_env

# Bounded header read, before any lock, so an idle client cannot hold anything.
header=
IFS= read -r -t 10 -n 512 header || die 'header missing, incomplete or timed out'
(( ${#header} < 512 )) || die 'header too long'
operation='' sha='' run='' attempt='' digest='' size='' entry_hash='' extra=''
read -r operation sha run attempt digest size entry_hash extra <<< "$header"
[[ $operation == deploy && -z $extra && $sha =~ ^[0-9a-f]{40}$ && $run =~ ^[0-9]{1,18}$ &&
   $attempt =~ ^[0-9]{1,9}$ && $digest =~ ^[0-9a-f]{64}$ && $size =~ ^[1-9][0-9]{0,9}$ &&
   $entry_hash =~ ^[0-9a-f]{64}$ ]] || die 'invalid header'
self_hash=$("$here/entry-hash.sh" "$here" "$home/.config/systemd/user/$unit") || die 'cannot compute the installed entry hash'
[[ $entry_hash == "$self_hash" ]] || die 'server deploy entry is out of date; reinstall it from the reviewed repository version (setup-magpie.sh install)'
expected_version=$(release_version "$sha")
(( size <= 1073741824 )) || die 'artifact larger than 1 GiB'
id="$sha-$run-$attempt"

# Admission order is (run number, attempt). Anything not strictly newer than the last admitted pair is refused,
# which rejects stale runs (older run number) and duplicates (same pair). A re-run of the newest run (same run,
# higher attempt) is allowed. Called before the upload and again, authoritatively, under the lock.
check_run() {
  local old_run old_attempt
  [[ -e $base/state/latest-run ]] || return 0
  read -r old_run old_attempt < "$base/state/latest-run" || die 'unreadable latest-run state'
  [[ $old_run =~ ^[0-9]{1,18}$ && $old_attempt =~ ^[0-9]{1,9}$ ]] || die 'corrupt latest-run state'
  if (( 10#$run < 10#$old_run )) || { (( 10#$run == 10#$old_run )) && (( 10#$attempt <= 10#$old_attempt )); }; then
    die "stale or duplicate run rejected (run $run attempt $attempt; last admitted $old_run attempt $old_attempt)"
  fi
}
[[ ! -e $base/state/blocked ]] || die 'a previous deployment failed and is unresolved (state/blocked); see rollback.sh'
# Migration guard: while this unit is not running, something else answering on 3000 (typically the old system service
# aiscoo-admin.service) would make the new binary fail to bind and leave the deployment blocked. Refuse first; nothing
# is written and the run number is not consumed, so the same run can simply be re-run after the old service is stopped.
if [[ $(ctl is-active 2>/dev/null || true) != active ]] && timeout 2 bash -c 'exec 3<>/dev/tcp/127.0.0.1/3000' 2>/dev/null; then
  die 'port 3000 is held by another process while this unit is not running (is the old system service aiscoo-admin.service still active? stop and disable it first)'
fi
check_run
# Configuration errors are refused before upload and before the run number is consumed.
if [[ -e $base/etc/backup.enabled ]]; then
  [[ -x $base/bin/pre-migration-backup && -O $base/bin/pre-migration-backup ]] || die 'backup is enabled but bin/pre-migration-backup is missing'
fi

available_kb=$(df -Pk "$base/staging" | awk 'NR==2 {print $4}')
(( available_kb * 1024 > size + 268435456 )) || die 'not enough free disk space'

tmp=$(mktemp "$base/staging/upload.XXXXXXXX")
dest="$base/releases/$id"
created=0 switched=0
cleanup() {
  rm -f "$tmp"
  # A release file that this run created but never made "current" is a half-finished product; remove it.
  # Never remove a release this run did not create.
  if (( created == 1 && switched == 0 )); then rm -f "$dest"; fi
}
trap cleanup EXIT

timeout "$UPLOAD_TIMEOUT" head -c "$size" > "$tmp" || die 'upload timed out'
[[ $(stat -c %s "$tmp") == "$size" ]] || die 'upload shorter than announced'
[[ $(timeout 5 head -c 1 | wc -c) -eq 0 ]] || die 'data after the announced artifact size'

[[ $(sha256sum "$tmp" | cut -d ' ' -f1) == "$digest" ]] || die 'checksum mismatch'
# ELF64, little endian, ET_EXEC or ET_DYN, x86-64.
elf=$(od -An -v -tx1 -N20 "$tmp" | tr -d ' \n')
[[ $elf =~ ^7f454c46020101[0-9a-f]{18}0[23]003e00$ ]] || die 'artifact is not a Linux amd64 ELF executable'
chmod 0500 "$tmp"
[[ $(binary_version "$tmp") == "$expected_version" ]] || die 'artifact does not report the announced version, or does not run on this host'

exec 9>"$base/state/deploy.lock"
flock -x -w "$LOCK_TIMEOUT" 9 || die 'deployment lock timed out'
[[ ! -e $base/state/blocked ]] || die 'a previous deployment failed and is unresolved (state/blocked)'
check_run
[[ ! -e $dest ]] || die 'release already exists'
printf '%s %s\n' "$run" "$attempt" > "$base/state/latest-run.tmp"
mv -f "$base/state/latest-run.tmp" "$base/state/latest-run"
log "admitted $id"
mv -T "$tmp" "$dest"
created=1
chmod 0555 "$dest"

# Optional pre-migration backup. Off unless etc/backup.enabled exists, and then it needs an operator-supplied
# executable bin/pre-migration-backup (this repository ships none). It runs with the service stopped. If it
# fails, the new version has not started yet, so the old version is started again and nothing is blocked.
if [[ -e $base/etc/backup.enabled ]]; then
  log 'pre-migration backup enabled; stopping service'
  ctl stop || true
  if ! "$base/bin/pre-migration-backup"; then
    created=0
    rm -f "$dest"
    ctl start || true
    die 'pre-migration backup failed; new version discarded, old version started again'
  fi
else
  log 'pre-migration backup is NOT enabled; a failed migration cannot be restored automatically'
fi

switch_current "$dest"
switched=1
log "current -> $id"

# From here on the new binary may already have migrated the database. Never roll back automatically.
fail_after_switch() {
  log "FAILED after switch: $1"
  save_evidence "$id"
  printf '%s\n' "$id" > "$base/state/blocked"
  printf 'deploy: service left as is; evidence in state/evidence/%s.log; database untouched by this script\n' "$id" >&2
  exit 1
}
ctl restart || fail_after_switch 'systemctl restart failed'
wait_healthy "$expected_version" || fail_after_switch 'service did not become healthy'

printf '%s\n' "$id" > "$base/state/accepted.tmp"
mv -f "$base/state/accepted.tmp" "$base/state/accepted"
log "local check passed for $id"

# Prune old releases, keeping the newest KEEP_RELEASES plus current and previous.
keep_cur=$(basename "$(readlink "$base/current")")
keep_prev=
[[ -L $base/previous ]] && keep_prev=$(basename "$(readlink "$base/previous")")
count=0
while IFS= read -r name; do
  count=$((count + 1))
  (( count > KEEP_RELEASES )) || continue
  [[ $name == "$keep_cur" || $name == "$keep_prev" ]] && continue
  rm -f "$base/releases/$name"
done < <(ls -1t "$base/releases")
printf 'deployment accepted locally: %s\n' "$id"
