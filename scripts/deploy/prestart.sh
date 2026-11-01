#!/usr/bin/env bash
# ExecStartPre of the user unit. Read-only: validates etc/app.env (format only, values are never printed) and
# waits for PostgreSQL on 127.0.0.1:5432, because a user unit cannot be ordered after postgresql.service.
# Exits non-zero on any problem; the unit has Restart=no, so a failure ends in "failed", not in a restart loop.
set -Eeuo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=common.sh
source "$here/common.sh"
load_conf
check_app_env
wait_postgres || die "PostgreSQL did not accept connections on 127.0.0.1:5432 within ${PG_WAIT_TIMEOUT}s"
