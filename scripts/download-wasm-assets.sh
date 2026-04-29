#!/usr/bin/env bash
set -euo pipefail

# Download WASM assets from the GitHub release into npm/dist/.
# Runs from npm/ directory (pre-main-publish-script working directory).
# Uses curl for public repo access (GH_TOKEN not available in this context).

RELEASE_TAG="${1:?Usage: download-wasm-assets.sh <release-tag>}"
REPO="${GITHUB_REPOSITORY:-bennypowers/mappa}"

mkdir -p dist

for asset in mappa.wasm wasm_exec.js; do
  echo "Downloading $asset from release $RELEASE_TAG..."
  curl -fsSL \
    -H "Accept: application/octet-stream" \
    "https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${asset}" \
    -o "dist/${asset}"
done

echo "Downloaded WASM assets to dist/"
ls -la dist/mappa.wasm dist/wasm_exec.js
