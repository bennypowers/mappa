#!/usr/bin/env bash
# Runs the js/wasm transport tests under Node.
#
# Usage: scripts/test-wasm.sh
set -euo pipefail

cd "$(dirname "$0")/.."

url_file=$(mktemp)
server=""
cleanup() {
  rm -f "$url_file"
  if [ -n "$server" ]; then
    kill "$server" 2>/dev/null || true
  fi
}
trap cleanup EXIT

node scripts/wasm-test-server.mjs "$url_file" &
server=$!

for _ in $(seq 1 100); do
  [ -s "$url_file" ] && break
  sleep 0.1
done
if [ ! -s "$url_file" ]; then
  echo "wasm test server did not start" >&2
  exit 1
fi

root=$(go env GOROOT)

# wasm_exec_node passes the whole environment to the wasm program, and Node
# rejects the binary when it exceeds the exec argument limit, so trim it.
env -i \
  HOME="$HOME" \
  PATH="$PATH:$root/lib/wasm" \
  GOOS=js GOARCH=wasm \
  CGO_ENABLED=0 \
  MAPPA_WASM_TEST_SERVER_URL="$(cat "$url_file")" \
  go test ./cdn/ -run 'TestHTTPFetcherWASM' -count=1
