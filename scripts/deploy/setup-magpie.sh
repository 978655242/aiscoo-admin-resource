#!/usr/bin/env bash
# One-time (idempotent) provisioning, run as the service account (not root), from a checkout of this directory:
#   bash setup-magpie.sh install                       create dirs, install scripts and the user unit
#   bash setup-magpie.sh authorize <public-key-file>   add the restricted CI key to authorized_keys
#   bash setup-magpie.sh check-env                     read-only format check of etc/app.env (never prints values)
# It never starts, stops or enables the service and never prints the contents of app.env.
#
# etc/app.env is created ONCE by root with make-app-env.sh (same directory; run as root, from a trusted copy):
#   SQL_DSN       copied from /etc/aiscoo-admin/database.env (one KEY=VALUE line)
#   SESSION_SECRET  from /etc/aiscoo-admin/session.secret (bare 64 hex digits, converted to KEY=VALUE)
#   PORT=3000 XCODE_INTEGRATION_ENABLED=true XCODE_PUBLIC_ORIGIN=https://112.124.202.60:8443 SESSION_COOKIE_SECURE=true
#   SESSION_COOKIE_TRUSTED_URL=https://112.124.202.60:8443 HTTP_PROXY=HTTPS_PROXY=http://127.0.0.1:17894
#   NO_PROXY=127.0.0.1,localhost,::1
# File: mode 0600, owned by this account. CRYPTO_SECRET must stay unset (the application falls back to SESSION_SECRET).
set -Eeuo pipefail
umask 077
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
die() { printf 'setup: %s\n' "$*" >&2; exit 1; }
[[ $(id -u) != 0 ]] || die 'run as the service account, not root'
# shellcheck source=common.sh
source "$here/common.sh"   # check_app_env; also defines home and base

case ${1:-} in
  install)
    install -d -m 700 "$base" "$base/releases" "$base/state" "$base/state/data" "$base/state/logs" "$base/state/tmp" \
      "$base/staging" "$base/etc" "$base/bin"
    for f in common.sh receive.sh rollback.sh prestart.sh entry-hash.sh; do install -m 500 "$here/$f" "$base/bin/$f"; done
    install -m 500 "$here/probe.py" "$base/bin/probe.py"   # not part of the entry hash; CI runs the probe from its own checkout
    install -d -m 700 "$home/.config/systemd/user"
    install -m 600 "$here/aiscoo-admin.service" "$home/.config/systemd/user/aiscoo-admin.service"
    systemctl --user daemon-reload
    status=0
    if [[ -e $base/etc/app.env || -L $base/etc/app.env ]]; then
      # Format only (keys, mode, owner, shapes); values are never printed. A problem is reported after the install.
      if (check_app_env); then printf 'setup: etc/app.env format check passed (values not shown)\n'; else status=1; fi
    else
      printf 'setup: etc/app.env is still missing; root must create it with make-app-env.sh before the service can start\n'
    fi
    printf 'setup: installed under %s\n' "$base"
    exit "$status"
    ;;
  check-env)
    check_app_env
    printf 'setup: etc/app.env format check passed (values not shown)\n'
    ;;
  authorize)
    pub=${2:-}
    [[ -f $pub ]] || die 'usage: setup-magpie.sh authorize <public-key-file>'
    [[ $(wc -l < "$pub") -le 1 ]] || die 'public key file must contain exactly one key'
    key=$(cut -d ' ' -f1,2 "$pub")
    [[ $key =~ ^ssh-ed25519\ AAAA[A-Za-z0-9+/]+=*$ ]] || die 'only a single ssh-ed25519 public key is accepted'
    install -d -m 700 "$home/.ssh"
    line="restrict,command=\"$base/bin/receive.sh\" $key aiscoo-admin-ci-deploy"
    touch "$home/.ssh/authorized_keys"
    chmod 600 "$home/.ssh/authorized_keys"
    if grep -qF -- "$key" "$home/.ssh/authorized_keys"; then
      die 'this key is already present in authorized_keys; remove the old line by hand first'
    fi
    printf '%s\n' "$line" >> "$home/.ssh/authorized_keys"
    printf 'setup: restricted deploy key added\n'
    ;;
  *) die 'usage: setup-magpie.sh install | authorize <public-key-file> | check-env' ;;
esac
