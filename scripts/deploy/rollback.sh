#!/usr/bin/env bash
# Manual, operator-run (as the service account). Switches the binary only. It never touches the database.
#   rollback.sh list
#   rollback.sh to <release-id|previous> I_HAVE_VERIFIED_SCHEMA_COMPATIBILITY
#   rollback.sh clear-blocked        (after you resolved a failed deployment yourself)
# A newer version may already have migrated the schema; starting an older binary on it can corrupt data.
set -Eeuo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=common.sh
source "$here/common.sh"
preflight
load_conf

usage() { die 'usage: rollback.sh list | to <release-id|previous> I_HAVE_VERIFIED_SCHEMA_COMPATIBILITY | clear-blocked'; }
(( $# >= 1 )) || usage
exec 9>"$base/state/deploy.lock"
flock -x -w "$LOCK_TIMEOUT" 9 || die 'deployment lock timed out'

case $1 in
  list)
    printf 'current:  %s\nprevious: %s\nblocked:  %s\n' \
      "$(readlink "$base/current" 2>/dev/null | xargs -r basename || true)" \
      "$(readlink "$base/previous" 2>/dev/null | xargs -r basename || true)" \
      "$(cat "$base/state/blocked" 2>/dev/null || echo none)"
    ls -1t "$base/releases"
    ;;
  clear-blocked)
    (( $# == 1 )) || usage
    rm -f "$base/state/blocked"
    log 'blocked marker cleared by operator'
    ;;
  to)
    (( $# == 3 )) && [[ $3 == I_HAVE_VERIFIED_SCHEMA_COMPATIBILITY ]] || usage
    name=$2
    if [[ $name == previous ]]; then
      [[ -L $base/previous ]] || die 'no previous release recorded'
      name=$(basename "$(readlink "$base/previous")")
    fi
    [[ $name =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] || die 'invalid release id'
    target="$base/releases/$name"
    [[ -f $target && ! -L $target ]] || die 'release not found'
    expected=$(binary_version "$target")
    [[ -n $expected ]] || die 'release binary does not run on this host'
    # Mark blocked first so a failed rollback cannot be followed by an automatic deploy.
    printf 'rollback-to-%s\n' "$name" > "$base/state/blocked"
    switch_current "$target"
    log "rollback: current -> $name (operator asserted schema compatibility)"
    ctl restart || { save_evidence "rollback-$name"; die 'systemctl restart failed; blocked marker kept'; }
    if ! wait_healthy "$expected"; then
      save_evidence "rollback-$name"
      die 'rolled-back service is not healthy; blocked marker kept'
    fi
    rm -f "$base/state/blocked"
    printf '%s\n' "$name" > "$base/state/accepted"
    printf 'rolled back locally to %s. Run probe.py against the public origin before trusting it.\n' "$name"
    ;;
  *) usage ;;
esac
