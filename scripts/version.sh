#!/bin/bash
# Version management script for mappa
# Updates npm/package.json and optionalDependencies versions
#
# Usage: ./scripts/version.sh <version>
# Example: ./scripts/version.sh 0.0.4

set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <version>"
  echo "Example: $0 0.0.4"
  exit 1
fi

VERSION="$1"
# Remove 'v' prefix if present
VERSION="${VERSION#v}"

# Validate VERSION is semver format before writing anything
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: Invalid version format '$VERSION'. Expected semver like 0.0.4"
  exit 1
fi

echo "Updating version to: $VERSION"

if ! command -v jq &> /dev/null; then
  echo "Error: jq is required to update npm/package.json"
  exit 1
fi

# Update npm/package.json main version
echo "Updating npm/package.json..."
jq --arg v "$VERSION" ".version = \$v" npm/package.json > npm/package.json.tmp
mv npm/package.json.tmp npm/package.json

# Update optionalDependencies versions
echo "Updating npm/package.json optionalDependencies..."
jq --arg v "$VERSION" ".optionalDependencies |= with_entries(.value = \$v)" npm/package.json > npm/package.json.tmp
mv npm/package.json.tmp npm/package.json

# Update package-lock.json
echo "Updating package-lock.json..."
(cd npm && npm install) || {
  echo "Error: npm install failed"
  git checkout -- npm/package.json
  exit 1
}

# Show changes
echo ""
echo "Version updated in:"
echo "  - npm/package.json"
echo "  - npm/package-lock.json"
echo ""
echo "Changes:"
git diff npm/package.json npm/package-lock.json

# Check if there are changes
if ! git diff --quiet npm/package.json npm/package-lock.json; then
  echo ""
  read -p "Commit version changes? (y/n) " -n 1 -r
  echo
  if [[ $REPLY =~ ^[Yy]$ ]]; then
    git add npm/package.json npm/package-lock.json
    git commit -m "chore: prepare version $VERSION"
    echo "✓ Version changes committed"
    echo ""
    echo "Next steps:"
    echo "  make release v$VERSION  (to tag, push, and create GitHub release)"
  else
    echo "Version changes rejected by user."
    git checkout -- npm/package.json npm/package-lock.json
    exit 1
  fi
else
  echo ""
  echo "Error: No changes detected. Version may already be set."
  exit 1
fi
