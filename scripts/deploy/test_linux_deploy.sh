#!/usr/bin/env bash
# Local-only isolated test. Builds fake binaries, then runs the real deploy scripts in a disposable
# linux/amd64 container as an unprivileged user (stub systemctl, real sshd). It never touches a remote host.
# Requires: docker, go. The image must exist or be pullable: ubuntu:24.04.
set -Eeuo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/fixtures"

build() { # label version mode goarch
  (cd "$here/testdata/fakeapp" && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="$4" \
    go build -trimpath -ldflags "-X main.version=release-${2:0:12} -X main.mode=$3" -o "$work/fixtures/$1" main.go)
}
build ok1 1111111111111111111111111111111111111111 ok amd64
build ok2 2222222222222222222222222222222222222222 ok amd64
build crash 3333333333333333333333333333333333333333 crash amd64
build wrongstatus 4444444444444444444444444444444444444444 wrongstatus amd64
build mismatch 1111111111111111111111111111111111111111 ok amd64
# reports the bare 40-hex sha instead of release-<12 hex>: the old, wrong convention must be refused
(cd "$here/testdata/fakeapp" && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-X main.version=7777777777777777777777777777777777777777 -X main.mode=ok" -o "$work/fixtures/barever" main.go)
build arm64 6666666666666666666666666666666666666666 ok arm64
printf '#!/bin/sh\necho 7777777777777777777777777777777777777777\n' > "$work/fixtures/script"
chmod +x "$work/fixtures/script"

image=${AISCOO_DEPLOY_TEST_IMAGE:-aiscoo-deploy-test:local}
if ! docker image inspect "$image" >/dev/null 2>&1; then
  printf 'Test image %s is missing. Build it with:\n' "$image" >&2
  printf '  printf "FROM ubuntu:24.04\\nRUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends python3 openssh-server openssh-client util-linux coreutils procps\\n" | docker build --platform linux/amd64 -t %s -\n' "$image" >&2
  exit 2
fi
docker run --rm --platform linux/amd64 --network none --pull=never \
  --mount "type=bind,src=$here,dst=/src,readonly" \
  --mount "type=bind,src=$work/fixtures,dst=/fixtures,readonly" \
  "$image" bash /src/testdata/linux_inner.sh
