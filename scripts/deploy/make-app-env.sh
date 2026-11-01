#!/usr/bin/env bash
# ONE-TIME, run by root (cloud assistant) from a root-owned copy: builds ~magpie/aiscoo-admin/etc/app.env
# from the files the previous system service used. Prints no secret value, only key names and PASS/FAIL.
#   make-app-env.sh [src-dir=/etc/aiscoo-admin] [dest=/home/magpie/aiscoo-admin/etc/app.env] [owner=magpie]
#   SQL_DSN         <- the SQL_DSN= line of <src-dir>/database.env (copied byte for byte)
#   SESSION_SECRET  <- <src-dir>/session.secret (bare 64 hex digits, rewritten as KEY=VALUE)
#   the rest        <- fixed values below (the previous service's environment; the two origin values carry
#                      the fixed admin HTTPS port :8443, because 443 belongs to the official website)
# CRYPTO_SECRET is deliberately not written: the application falls back to SESSION_SECRET, so they cannot diverge.
# Refuses to overwrite an existing file. The result is 0600 and owned by <owner>; nothing is read by or sent to
# any other program. Verify afterwards as magpie with: bash setup-magpie.sh check-env
set -Eeuo pipefail
umask 077
export LC_ALL=C PATH=/usr/local/bin:/usr/bin:/bin
die() { printf 'make-app-env: %s\n' "$*" >&2; exit 1; }
[[ $(id -u) == 0 ]] || die 'run as root'
src=${1:-/etc/aiscoo-admin} dest=${2:-/home/magpie/aiscoo-admin/etc/app.env} owner=${3:-magpie}
[[ $dest == /* && $src == /* ]] || die 'paths must be absolute'
[[ ! -e $dest && ! -L $dest ]] || die 'destination already exists; refusing to overwrite'
destdir=$(dirname -- "$dest")
[[ -d $destdir && ! -L $destdir ]] || die 'destination directory missing (run setup-magpie.sh install first)'
[[ -f $src/database.env && ! -L $src/database.env && -f $src/session.secret && ! -L $src/session.secret ]] || die 'source files missing'

[[ $(grep -c '^SQL_DSN=' "$src/database.env") == 1 ]] || die 'database.env must contain exactly one SQL_DSN= line'
dsn_line=$(grep '^SQL_DSN=' "$src/database.env")
dsn_shape='^SQL_DSN=postgres(ql)?://[^[:space:]"'\''\\$#@]+@127\.0\.0\.1:5432/aiscoo_admin(\?[A-Za-z0-9_.=&%-]+)?$'
[[ $dsn_line =~ $dsn_shape ]] || die 'SQL_DSN in database.env does not have the expected shape (value not shown)'
secret=$(tr -d '\n' < "$src/session.secret")
[[ $secret =~ ^[0-9a-fA-F]{64}$ ]] || die 'session.secret is not 64 hexadecimal digits (value not shown)'

tmp=$(mktemp "$destdir/.app.env.XXXXXXXX")
trap 'rm -f "$tmp"' EXIT
{
  printf '%s\n' "$dsn_line"
  printf 'SESSION_SECRET=%s\n' "$secret"
  printf '%s\n' \
    'PORT=3000' \
    'XCODE_INTEGRATION_ENABLED=true' \
    'XCODE_PUBLIC_ORIGIN=https://112.124.202.60:8443' \
    'SESSION_COOKIE_SECURE=true' \
    'SESSION_COOKIE_TRUSTED_URL=https://112.124.202.60:8443' \
    'HTTP_PROXY=http://127.0.0.1:17894' \
    'HTTPS_PROXY=http://127.0.0.1:17894' \
    'NO_PROXY=127.0.0.1,localhost,::1'
} > "$tmp"
chown "$owner": "$tmp"
chmod 600 "$tmp"
ln -- "$tmp" "$dest"   # fails if dest appeared meanwhile; never overwrites
printf 'make-app-env: wrote %s (mode 0600, owner %s, 10 keys; values not shown)\n' "$dest" "$owner"
