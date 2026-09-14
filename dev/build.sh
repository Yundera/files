#!/usr/bin/env bash
# Build and test without a local go or node toolchain.
#
# Neither is installed in the Yundera dev container, so both stacks run in Docker
# against the host engine. The bind mount must use the HOST path, which is what
# $ROOT resolves to — a container-only path would mount empty.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_IMAGE=golang:1.26
NODE_IMAGE=node:22

go_run() { docker run --rm -v "$ROOT:/src" -w /src -e GOFLAGS=-mod=mod "$GO_IMAGE" "$@"; }

# Mounts the WHOLE repo, not just web/, and only then sets the workdir. vite's
# outDir is ../internal/ui/dist — relative to web/ — so a mount of web/ alone
# puts the build output outside the mount, where it is discarded when the
# container exits. That failure is silent: vite reports writing the files and
# `go build` then embeds the stale placeholder index.html.
node_run() { docker run --rm -v "$ROOT:/src" -w /src/web "$NODE_IMAGE" "$@"; }

usage() {
  cat <<'EOF'
usage: dev/build.sh <command>

  web      vite build -> internal/ui/dist   (MUST run before `go build`)
  go       go build ./...
  test     go vet ./... && go test ./...
  check    svelte-check
  all      web + go + test
  tidy     go mod tidy
EOF
}

case "${1:-all}" in
  web)   node_run npm install --no-audit --no-fund && node_run npm run build ;;
  go)    go_run go build ./... ;;
  test)  go_run go vet ./... && go_run go test ./... ;;
  check) node_run npm run check ;;
  tidy)  go_run go mod tidy ;;
  all)
    # Order is not arbitrary: vite writes the directory that //go:embed reads, so
    # a `go build` before `npm run build` embeds the placeholder index.html.
    node_run npm install --no-audit --no-fund
    node_run npm run build
    go_run go build ./...
    go_run go vet ./...
    go_run go test ./...
    ;;
  *) usage; exit 1 ;;
esac
