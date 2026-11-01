#!/usr/bin/env bash
# Prints one SHA-256 over the deployment entry files, so CI and the server can prove they hold the same scripts.
#   entry-hash.sh <dir holding the entry files> <path of aiscoo-admin.service>
# CI runs it on its checkout (scripts/deploy + the unit in that directory); receive.sh runs the installed copy
# (~/aiscoo-admin/bin + ~/.config/systemd/user/aiscoo-admin.service). CI puts the result in the upload header and
# receive.sh refuses to continue when it differs from its own: the repository's reviewed version must have been
# reinstalled on the server first. probe.py is not covered (CI runs it from the checkout, the server never does).
# This list is part of the hash on purpose: a stale copy of this file computes another value and is refused.
set -Eeuo pipefail
export LC_ALL=C PATH=/usr/local/bin:/usr/bin:/bin
(( $# == 2 )) || { echo 'usage: entry-hash.sh <dir> <unit-file>' >&2; exit 2; }
dir=$1 unit_file=$2
files=(entry-hash.sh common.sh receive.sh rollback.sh prestart.sh)
{
  for f in "${files[@]}"; do
    [[ -f $dir/$f && ! -L $dir/$f ]] || { echo "entry-hash: missing entry file $f" >&2; exit 1; }
    printf '%s  %s\n' "$(sha256sum < "$dir/$f" | cut -d ' ' -f1)" "$f"
  done
  [[ -f $unit_file && ! -L $unit_file ]] || { echo 'entry-hash: unit file missing' >&2; exit 1; }
  printf '%s  aiscoo-admin.service\n' "$(sha256sum < "$unit_file" | cut -d ' ' -f1)"
} | sha256sum | cut -d ' ' -f1
