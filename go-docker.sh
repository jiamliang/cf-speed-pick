#!/bin/bash
# Wrapper for running `go` inside the golang:1.21-alpine container.
# Usage:
#   ./go-docker.sh mod init cf-speed-pick
#   ./go-docker.sh build .
#   ./go-docker.sh test ./...
set -e

IMAGE="golang:1.21-alpine"
PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
WORKDIR="/work"
HTTPS_PROXY_HOST="172.17.0.1"

docker run --rm \
    -v "$PROJECT_DIR:$WORKDIR" \
    -w "$WORKDIR" \
    -e GOPATH=/tmp/go \
    -e GOCACHE=/tmp/go-cache \
    -e CGO_ENABLED=0 \
    golang:1.21-alpine \
    go "$@"
