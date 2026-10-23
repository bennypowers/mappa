#!/bin/bash
# Regression tests for scripts/version.sh.
#
# Usage: test.sh

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

pass() { echo "  ok: $*"; }
fail() { echo "  FAIL: $*" >&2; exit 1; }
assert_eq() {
  local actual="$1" expected="$2" message="$3"
  [ "$actual" = "$expected" ] || fail "$message (expected '$expected', got '$actual')"
}

make_fixture() {
  local dir="$1"
  mkdir -p "$dir/npm" "$dir/bin"
  cat > "$dir/npm/package.json" <<'JSON'
{
  "name": "@pwrs/mappa",
  "version": "0.0.1",
  "optionalDependencies": {
    "@pwrs/mappa-linux-x64": "0.0.1",
    "@pwrs/mappa-darwin-x64": "0.0.1"
  }
}
JSON
  printf '{"name":"@pwrs/mappa","version":"0.0.1","lockfileVersion":3}\n' > "$dir/npm/package-lock.json"

  # Stub npm so the version script never touches the network. It snapshots the
  # updated package.json before the script's own git-based rollback runs.
  cat > "$dir/bin/npm" <<'SH'
#!/usr/bin/env bash
cp package.json "$VERSION_TEST_SNAPSHOT"
SH
  chmod +x "$dir/bin/npm"

  (
    cd "$dir"
    git init -q
    git config user.email test@example.com
    git config user.name Test
    git add npm/package.json npm/package-lock.json
    git commit -qm 'test: fixture'
  )
}

echo "invalid version is rejected before writing"
invalid="$TMP/invalid"
make_fixture "$invalid"
before=$(cat "$invalid/npm/package.json")
set +e
(cd "$invalid" && "$REPO_ROOT/scripts/version.sh" NOT-A-VERSION) >/dev/null 2>&1
status=$?
set -e
[ "$status" -eq 1 ] || fail "invalid version exited $status, expected 1"
after=$(cat "$invalid/npm/package.json")
[ "$after" = "$before" ] || fail "invalid version modified package.json"
pass "invalid version rejected and package.json unchanged"

echo "valid version updates main version and optionalDependencies"
valid="$TMP/valid"
make_fixture "$valid"
export VERSION_TEST_SNAPSHOT="$TMP/valid-snapshot.json"
set +e
# The stub npm returns without creating a lockfile, so the script rolls the
# fixture back; the snapshot captures the write that happened first.
(cd "$valid" && PATH="$valid/bin:$PATH" "$REPO_ROOT/scripts/version.sh" v1.2.3 <<< 'n') >/dev/null 2>&1
set -e
[ -f "$VERSION_TEST_SNAPSHOT" ] || fail "npm stub was not invoked"

assert_eq "$(jq -r .version "$VERSION_TEST_SNAPSHOT")" "1.2.3" "main version"
assert_eq "$(jq -r '.optionalDependencies["@pwrs/mappa-linux-x64"]' "$VERSION_TEST_SNAPSHOT")" "1.2.3" "linux optionalDependency"
assert_eq "$(jq -r '.optionalDependencies["@pwrs/mappa-darwin-x64"]' "$VERSION_TEST_SNAPSHOT")" "1.2.3" "darwin optionalDependency"
pass "main version and all optionalDependencies updated"

echo "all version script tests passed"
