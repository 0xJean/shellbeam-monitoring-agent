#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export AGENT_VERSION="${AGENT_VERSION:-1.5.12}"
export VERSION_HASH="${VERSION_HASH:-$(git rev-parse HEAD)}"
export DIST="${DIST:-build}"
for target in linux/amd64 linux/arm64 linux/386 linux/arm linux/loong64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/arm64; do
    export GOOS="${target%/*}" GOARCH="${target#*/}"
    echo "Building ShellBeam Monitoring Agent ${AGENT_VERSION} for ${target}"
    make artifacts DIST="${DIST}/${GOOS}-${GOARCH}"
done
