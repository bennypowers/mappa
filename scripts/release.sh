#!/bin/bash
# Release script for mappa
# Creates version commit, pushes it, then uses gh to tag and create release
#
# Usage: ./scripts/release.sh <version|patch|minor|major>
# Example: ./scripts/release.sh v0.0.4
# Example: ./scripts/release.sh patch

set -e

# Pre-flight checks
for cmd in git npm gh; do
  if ! command -v "$cmd" &> /dev/null; then
    echo "Error: $cmd is required but not found in PATH"
    exit 1
  fi
done

if [ -z "$1" ]; then
  echo "Error: VERSION or bump type is required"
  echo "Usage: $0 <version|patch|minor|major>"
  echo "  $0 v0.0.4   - Release explicit version"
  echo "  $0 patch    - Bump patch version (0.0.x)"
  echo "  $0 minor    - Bump minor version (0.x.0)"
  echo "  $0 major    - Bump major version (x.0.0)"
  exit 1
fi

INPUT="$1"

# Check if input is a bump type (patch/minor/major)
if [[ "$INPUT" =~ ^(patch|minor|major)$ ]]; then
  BUMP_TYPE="$INPUT"

  # Get the latest tag
  LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
  echo "Latest tag: $LATEST_TAG"

  # Remove 'v' prefix if present
  CURRENT_VERSION="${LATEST_TAG#v}"

  # Parse version components
  if [[ ! "$CURRENT_VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    echo "Error: Latest tag '$LATEST_TAG' is not a valid semver version"
    echo "Expected format: v0.0.0"
    exit 1
  fi

  MAJOR="${BASH_REMATCH[1]}"
  MINOR="${BASH_REMATCH[2]}"
  PATCH="${BASH_REMATCH[3]}"

  # Bump the appropriate component
  case "$BUMP_TYPE" in
    patch)
      PATCH=$((PATCH + 1))
      ;;
    minor)
      MINOR=$((MINOR + 1))
      PATCH=0
      ;;
    major)
      MAJOR=$((MAJOR + 1))
      MINOR=0
      PATCH=0
      ;;
  esac

  VERSION="v${MAJOR}.${MINOR}.${PATCH}"
  echo "Bumping $BUMP_TYPE: $LATEST_TAG → $VERSION"
  echo ""
else
  # Use explicit version
  VERSION="$INPUT"
fi

echo "Checking if tag $VERSION already exists..."
if git rev-parse "$VERSION" >/dev/null 2>&1; then
  echo "Error: Tag $VERSION already exists"
  echo "Use 'git tag -d $VERSION' to delete locally if needed"
  exit 1
fi
echo "✓ Tag $VERSION does not exist"
echo ""

echo "Creating release $VERSION..."
echo ""

echo "Step 1: Updating version files and committing..."
./scripts/version.sh "$VERSION" || {
  echo ""
  echo "Error: Version update was rejected or failed"
  echo "Release flow aborted"
  exit 1
}
echo ""

# Strip 'v' prefix for version comparison
CHECK_VERSION="${VERSION#v}"

echo "Step 2: Verifying version sync..."
if command -v jq &> /dev/null; then
  NPM_VERSION=$(jq -r '.version' npm/package.json)
else
  NPM_VERSION=$(node -e "process.stdout.write(JSON.parse(require('fs').readFileSync('npm/package.json','utf8')).version)")
fi

if [ "$NPM_VERSION" != "$CHECK_VERSION" ]; then
  echo "  ✗ npm/package.json has version $NPM_VERSION (expected $CHECK_VERSION)"
  exit 1
fi
echo "  ✓ npm/package.json: $NPM_VERSION"
echo ""

echo "Step 3: Pushing version commit..."
git push
echo ""

echo "Step 4: Creating GitHub release (gh will tag and push)..."
gh release create "$VERSION"
