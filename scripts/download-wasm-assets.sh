#!/usr/bin/env bash
set -euo pipefail

# Download WASM assets from the GitHub release into npm/dist/.
# Runs from npm/ directory (pre-main-publish-script working directory).
# Uses curl for public repo access (GH_TOKEN not available in this context).

RELEASE_TAG="${1:?Usage: download-wasm-assets.sh <release-tag>}"
REPO="${GITHUB_REPOSITORY:-bennypowers/mappa}"
MAX_RETRIES=5

mkdir -p dist

for asset in mappa.wasm wasm_exec.js; do
  attempt=0
  while true; do
    attempt=$((attempt + 1))
    echo "Downloading $asset (attempt $attempt/$MAX_RETRIES)..."
    if curl -fsSL \
      -H "Accept: application/octet-stream" \
      "https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${asset}" \
      -o "dist/${asset}"; then
      break
    fi
    if [ "$attempt" -ge "$MAX_RETRIES" ]; then
      echo "Failed to download $asset after $MAX_RETRIES attempts"
      exit 1
    fi
    delay=$((1 << (attempt - 1)))
    echo "Retrying in ${delay}s..."
    sleep "$delay"
  done
done

echo "Downloaded WASM assets to dist/"
ls -la dist/mappa.wasm dist/wasm_exec.js
