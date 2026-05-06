/*
Copyright © 2026 Benny Powers <web@bennypowers.com>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/
package local_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/internal/mapfs"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
	"bennypowers.dev/mappa/resolve/local"
	"bennypowers.dev/mappa/testutil"
)

// mockLogger captures log messages for testing
type mockLogger struct {
	mu       sync.Mutex
	warnings []string
	debugs   []string
}

func (m *mockLogger) Warning(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Debug(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debugs = append(m.debugs, fmt.Sprintf(format, args...))
}

func TestResolver(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"simple package", "simple-pkg"},
		{"with scopes", "with-scopes"},
		{"scope simplification with wildcards", "with-wildcard-scopes"},
		{"wildcard subpath exports in scopes", "wildcard-subpath-scopes"},
		{"circular dependencies", "circular-deps"},
		{"nested node_modules", "nested-node-modules"},
		{"peer dependencies in scopes", "with-peer-deps"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := testutil.NewFixtureFS(t, "resolve/"+tt.dir, "/test")

			expectedData, err := mfs.ReadFile("/test/expected.json")
			if err != nil {
				t.Fatalf("Failed to read expected.json: %v", err)
			}

			var expected importmap.ImportMap
			if err := json.Unmarshal(expectedData, &expected); err != nil {
				t.Fatalf("Failed to parse expected.json: %v", err)
			}

			resolver := local.New(mfs, nil)
			result, err := resolver.Resolve("/test")
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}

			if !reflect.DeepEqual(result.Imports, expected.Imports) {
				t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
			}

			if !reflect.DeepEqual(result.Scopes, expected.Scopes) {
				t.Errorf("Scopes mismatch:\n  got:      %v\n  expected: %v", result.Scopes, expected.Scopes)
			}
		})
	}
}

func TestResolverNoPackageJSON(t *testing.T) {
	mfs := mapfs.New()
	mfs.AddDir("/empty", 0755)

	resolver := local.New(mfs, nil)
	result, err := resolver.Resolve("/empty")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(result.Imports) != 0 {
		t.Errorf("Expected empty imports, got %v", result.Imports)
	}
}

func TestResolverInterface(t *testing.T) {
	var _ resolve.Resolver = (*local.Resolver)(nil)
}

func TestResolverWorkspaceMode(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace", "/test")

	expectedData, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected importmap.ImportMap
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	// Define workspace packages
	workspacePackages := []resolve.WorkspacePackage{
		{Name: "@myorg/core", Path: "/test/packages/core"},
		{Name: "@myorg/components", Path: "/test/packages/components"},
	}

	resolver := local.New(mfs, nil).WithWorkspacePackages(workspacePackages)
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !reflect.DeepEqual(result.Imports, expected.Imports) {
		t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
	}

	// Verify workspace packages use web paths not template paths
	if result.Imports["@myorg/core"] != "/packages/core/src/index.js" {
		t.Errorf("Expected workspace package @myorg/core to use web path, got %s", result.Imports["@myorg/core"])
	}

	// Verify node_modules dependencies use template paths
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit to use template path, got %s", result.Imports["lit"])
	}
}

func TestResolverWithPackageCache(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/simple-pkg", "/test")

	cache := packagejson.NewMemoryCache()

	resolver := local.New(mfs, nil).WithPackageCache(cache)
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify the result is correct
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import, got %v", result.Imports)
	}

	// Verify the cache was populated
	rootPkg, ok := cache.Get("/test/package.json")
	if !ok {
		t.Error("Expected root package.json to be cached")
	}
	if rootPkg == nil || rootPkg.Name != "my-app" {
		t.Errorf("Expected cached root package to be 'my-app', got %v", rootPkg)
	}

	litPkg, ok := cache.Get("/test/node_modules/lit/package.json")
	if !ok {
		t.Error("Expected lit package.json to be cached")
	}
	if litPkg == nil || litPkg.Name != "lit" {
		t.Errorf("Expected cached lit package to be 'lit', got %v", litPkg)
	}
}

func TestResolverWithPrepopulatedCache(t *testing.T) {
	mfs := mapfs.New()
	mfs.AddDir("/test", 0755)
	mfs.AddDir("/test/node_modules", 0755)
	mfs.AddDir("/test/node_modules/my-pkg", 0755)

	// Only add root package.json to filesystem
	mfs.AddFile("/test/package.json", `{
		"name": "test",
		"dependencies": {"my-pkg": "1.0.0"}
	}`, 0644)

	// Pre-populate cache with my-pkg (simulating previously parsed)
	cache := packagejson.NewMemoryCache()
	cache.Set("/test/node_modules/my-pkg/package.json", &packagejson.PackageJSON{
		Name:    "my-pkg",
		Version: "1.0.0",
		Main:    "dist/index.js",
	})

	// Add empty file so Exists() returns true but ReadFile would fail if called
	mfs.AddFile("/test/node_modules/my-pkg/package.json", "", 0644)

	resolver := local.New(mfs, nil).WithPackageCache(cache)
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify my-pkg was resolved using cached data (has Main: "dist/index.js")
	expected := "/node_modules/my-pkg/dist/index.js"
	if result.Imports["my-pkg"] != expected {
		t.Errorf("Expected my-pkg import to be %q (from cache), got %q", expected, result.Imports["my-pkg"])
	}
}

func TestResolverCacheReuse(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/simple-pkg", "/test")

	cache := packagejson.NewMemoryCache()

	// First resolution
	resolver := local.New(mfs, nil).WithPackageCache(cache)
	result1, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("First resolve failed: %v", err)
	}

	// Second resolution with same cache
	result2, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Second resolve failed: %v", err)
	}

	// Results should be equivalent
	if !reflect.DeepEqual(result1.Imports, result2.Imports) {
		t.Error("Expected identical results from cached resolves")
	}
}

func TestResolverWarnsOnNoExportsOrMain(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/no-exports-pkg", "/test")

	logger := &mockLogger{}
	resolver := local.New(mfs, logger)
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Should only have trailing slash mapping, not bare specifier
	if _, ok := result.Imports["broken-lib"]; ok {
		t.Error("Expected no bare specifier mapping for package without exports/main")
	}
	if result.Imports["broken-lib/"] != "/node_modules/broken-lib/" {
		t.Errorf("Expected trailing slash mapping, got %v", result.Imports)
	}

	// Should have logged a warning
	expectedWarning := "Package 'broken-lib' has no root export or main field; only subpath imports will work"
	if !slices.Contains(logger.warnings, expectedWarning) {
		t.Errorf("Expected warning %q, got warnings: %v", expectedWarning, logger.warnings)
	}
}

func TestResolverAutoDiscoverWorkspaces(t *testing.T) {
	// Use the existing workspace fixture
	mfs := testutil.NewFixtureFS(t, "workspace", "/test")

	expectedData, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected importmap.ImportMap
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	// Don't call WithWorkspacePackages - should auto-discover
	resolver := local.New(mfs, nil)
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify workspace packages are discovered and included
	if result.Imports["@myorg/core"] != expected.Imports["@myorg/core"] {
		t.Errorf("@myorg/core import = %q, want %q",
			result.Imports["@myorg/core"], expected.Imports["@myorg/core"])
	}
	if result.Imports["@myorg/components"] != expected.Imports["@myorg/components"] {
		t.Errorf("@myorg/components import = %q, want %q",
			result.Imports["@myorg/components"], expected.Imports["@myorg/components"])
	}

	// Verify node_modules dependencies are included
	if result.Imports["lit"] != expected.Imports["lit"] {
		t.Errorf("lit import = %q, want %q", result.Imports["lit"], expected.Imports["lit"])
	}
}

func TestResolverWithExclude(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/with-exclude", "/test")

	expectedData, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected importmap.ImportMap
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	resolver := local.New(mfs, nil).WithExclude([]string{"lodash"})
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !reflect.DeepEqual(result.Imports, expected.Imports) {
		t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
	}

	// Verify lodash is excluded
	if _, ok := result.Imports["lodash"]; ok {
		t.Error("Expected lodash to be excluded from imports")
	}

	// Verify other deps are still present
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import, got %v", result.Imports["lit"])
	}
	if result.Imports["@example/utils"] != "/node_modules/@example/utils/index.js" {
		t.Errorf("Expected @example/utils import, got %v", result.Imports["@example/utils"])
	}
}

func TestResolverExplicitWorkspacesOverrideAutoDiscovery(t *testing.T) {
	// Use the existing workspace fixture
	mfs := testutil.NewFixtureFS(t, "workspace", "/test")

	// Explicitly provide only one workspace package
	workspacePackages := []resolve.WorkspacePackage{
		{Name: "@myorg/core", Path: "/test/packages/core"},
	}

	resolver := local.New(mfs, nil).WithWorkspacePackages(workspacePackages)
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Should have the explicitly provided package
	if result.Imports["@myorg/core"] != "/packages/core/src/index.js" {
		t.Errorf("@myorg/core import = %q, want /packages/core/src/index.js",
			result.Imports["@myorg/core"])
	}

	// Should NOT have the auto-discovered package (since explicit was provided)
	if _, ok := result.Imports["@myorg/components"]; ok {
		t.Error("Expected @myorg/components to not be auto-discovered when explicit packages provided")
	}
}

func TestResolverWithPathBase(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	expectedData, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected importmap.ImportMap
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	resolver := local.New(mfs, nil).WithPathBase("/test/examples/kitchen-sink")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !reflect.DeepEqual(result.Imports, expected.Imports) {
		t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
	}

	// Workspace package at serve root should be rebased to /
	if result.Imports["@examples/kitchen-sink/"] != "/" {
		t.Errorf("Expected @examples/kitchen-sink/ to be rebased to /, got %s",
			result.Imports["@examples/kitchen-sink/"])
	}

	// node_modules paths should be unchanged
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit path unchanged, got %s", result.Imports["lit"])
	}

	// Scope keys under node_modules should NOT be rebased
	if !reflect.DeepEqual(result.Scopes, expected.Scopes) {
		t.Errorf("Scopes mismatch:\n  got:      %v\n  expected: %v", result.Scopes, expected.Scopes)
	}
}

func TestResolverWithPathBaseSameAsRoot(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	expectedData, err := mfs.ReadFile("/test/expected-no-rebase.json")
	if err != nil {
		t.Fatalf("Failed to read expected-no-rebase.json: %v", err)
	}

	var expected importmap.ImportMap
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("Failed to parse expected-no-rebase.json: %v", err)
	}

	// Serve root == resolution root: no rebasing
	resolver := local.New(mfs, nil).WithPathBase("/test")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !reflect.DeepEqual(result.Imports, expected.Imports) {
		t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
	}
}

func TestResolverWithPathBaseAutoDiscovery(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	// No explicit WithWorkspacePackages -- auto-discover + serve root
	resolver := local.New(mfs, nil).WithPathBase("/test/examples/kitchen-sink")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Imports["@examples/kitchen-sink/"] != "/" {
		t.Errorf("Expected @examples/kitchen-sink/ to be rebased to /, got %s",
			result.Imports["@examples/kitchen-sink/"])
	}

	if result.Imports["@examples/kitchen-sink"] != "/src/index.js" {
		t.Errorf("Expected @examples/kitchen-sink to be /src/index.js, got %s",
			result.Imports["@examples/kitchen-sink"])
	}

	// Other workspace package paths should NOT be rebased
	if result.Imports["@myorg/lib"] != "/packages/lib/src/index.js" {
		t.Errorf("Expected @myorg/lib to remain /packages/lib/src/index.js, got %s",
			result.Imports["@myorg/lib"])
	}
}

func TestResolverWithPackageDeps(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-package-deps", "/test")

	expectedData, err := mfs.ReadFile("/test/expected-core-deps.json")
	if err != nil {
		t.Fatalf("Failed to read expected-core-deps.json: %v", err)
	}

	var expected importmap.ImportMap
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("Failed to parse expected-core-deps.json: %v", err)
	}

	resolver := local.New(mfs, nil).WithPackageDeps("/test/packages/core")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !reflect.DeepEqual(result.Imports, expected.Imports) {
		t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
	}

	// lit should be present (core dep)
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import, got %s", result.Imports["lit"])
	}

	// lodash should NOT be present (tools dep, not core)
	if _, ok := result.Imports["lodash"]; ok {
		t.Error("Expected lodash to be excluded (not a dep of core)")
	}

	// Both workspace packages should still appear in imports
	if result.Imports["@myorg/core"] != "/packages/core/src/index.js" {
		t.Errorf("Expected @myorg/core workspace package, got %s", result.Imports["@myorg/core"])
	}
	if result.Imports["@myorg/tools"] != "/packages/tools/src/index.js" {
		t.Errorf("Expected @myorg/tools workspace package, got %s", result.Imports["@myorg/tools"])
	}
}

func TestResolverWithPackageDepsFallback(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-package-deps", "/test")

	expectedData, err := mfs.ReadFile("/test/expected-all-deps.json")
	if err != nil {
		t.Fatalf("Failed to read expected-all-deps.json: %v", err)
	}

	var expected importmap.ImportMap
	if err := json.Unmarshal(expectedData, &expected); err != nil {
		t.Fatalf("Failed to parse expected-all-deps.json: %v", err)
	}

	// No WithPackageDeps: all deps from all workspace packages
	resolver := local.New(mfs, nil)
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if !reflect.DeepEqual(result.Imports, expected.Imports) {
		t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
	}
}

func TestResolverWithPackageDepsError(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-package-deps", "/test")

	resolver := local.New(mfs, nil).WithPackageDeps("/test/nonexistent")
	_, err := resolver.Resolve("/test")
	if err == nil {
		t.Fatal("Expected error for nonexistent packageDeps path")
	}
	if !strings.Contains(err.Error(), "package-deps") {
		t.Errorf("Expected error to mention package-deps, got: %v", err)
	}
}

func TestResolverWithPathBaseAndPackageDeps(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-package-deps", "/test")

	resolver := local.New(mfs, nil).
		WithPathBase("/test/packages/core").
		WithPackageDeps("/test/packages/core")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// core's path should be rebased
	if result.Imports["@myorg/core"] != "/src/index.js" {
		t.Errorf("Expected @myorg/core rebased to /src/index.js, got %s",
			result.Imports["@myorg/core"])
	}

	// lit should be present (core dep)
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import, got %s", result.Imports["lit"])
	}

	// lodash should NOT be present
	if _, ok := result.Imports["lodash"]; ok {
		t.Error("Expected lodash excluded")
	}
}

func TestResolverBuilderPropagation(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	cache := packagejson.NewMemoryCache()

	// Chain multiple builders to verify fields propagate
	resolver := local.New(mfs, nil).
		WithPathBase("/test/examples/kitchen-sink").
		WithPackageDeps("/test/examples/kitchen-sink").
		WithPackageCache(cache).
		WithConditions([]string{"browser", "import", "default"}).
		WithExclude([]string{"nonexistent-pkg"}).
		WithPackages([]string{}).
		WithIncludeRootExports()

	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// PathBase should have survived the builder chain
	if result.Imports["@examples/kitchen-sink/"] != "/" {
		t.Errorf("PathBase lost in builder chain: @examples/kitchen-sink/ = %s, want /",
			result.Imports["@examples/kitchen-sink/"])
	}

	// PackageDeps should have survived: only kitchen-sink's dep (lit) should appear
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("PackageDeps lost in builder chain: lit = %s", result.Imports["lit"])
	}
}

func TestOptionsApplyPathBaseAndPackageDeps(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	opts := &local.Options{
		PathBase:   "/test/examples/kitchen-sink",
		PackageDeps: "/test/examples/kitchen-sink",
	}

	resolver, err := opts.Apply(local.New(mfs, nil))
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Imports["@examples/kitchen-sink/"] != "/" {
		t.Errorf("PathBase not applied via Options: @examples/kitchen-sink/ = %s, want /",
			result.Imports["@examples/kitchen-sink/"])
	}
}

func TestResolverWithPathBaseOutsideRoot(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	// pathBase outside rootDir -- should be a no-op
	resolver := local.New(mfs, nil).WithPathBase("/other")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// paths should not be rebased
	if result.Imports["@examples/kitchen-sink/"] != "/examples/kitchen-sink/" {
		t.Errorf("Expected no rebasing, got @examples/kitchen-sink/ = %s",
			result.Imports["@examples/kitchen-sink/"])
	}
}

func TestResolverWithRelativePathBase(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	// Relative pathBase should be resolved relative to rootDir
	resolver := local.New(mfs, nil).WithPathBase("examples/kitchen-sink")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.Imports["@examples/kitchen-sink/"] != "/" {
		t.Errorf("Relative pathBase not normalized: @examples/kitchen-sink/ = %s, want /",
			result.Imports["@examples/kitchen-sink/"])
	}
}

func TestResolverWithRelativePackageDeps(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-package-deps", "/test")

	// Relative packageDeps should be resolved relative to rootDir
	resolver := local.New(mfs, nil).WithPackageDeps("packages/core")
	result, err := resolver.Resolve("/test")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// lit should be present (core dep), lodash should not
	if result.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import with relative packageDeps, got %s", result.Imports["lit"])
	}
	if _, ok := result.Imports["lodash"]; ok {
		t.Error("Expected lodash excluded with relative packageDeps")
	}
}

func TestResolverWithPathBaseIncrementalIdempotent(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "workspace-serve-root", "/test")

	workspacePackages := []resolve.WorkspacePackage{
		{Name: "@examples/kitchen-sink", Path: "/test/examples/kitchen-sink"},
		{Name: "@myorg/lib", Path: "/test/packages/lib"},
	}

	resolver := local.New(mfs, nil).
		WithWorkspacePackages(workspacePackages).
		WithPathBase("/test/examples/kitchen-sink")

	// Initial resolve with graph
	initial, err := resolver.ResolveWithGraph("/test")
	if err != nil {
		t.Fatalf("ResolveWithGraph failed: %v", err)
	}

	if initial.ImportMap.Imports["@examples/kitchen-sink/"] != "/" {
		t.Fatalf("Initial rebase failed: @examples/kitchen-sink/ = %s",
			initial.ImportMap.Imports["@examples/kitchen-sink/"])
	}

	// Incremental update -- simulate lit changing
	incremental, err := resolver.ResolveIncremental("/test", resolve.IncrementalUpdate{
		ChangedPackages: []string{"lit"},
		PreviousMap:     initial.ImportMap,
		PreviousGraph:   initial.DependencyGraph,
	})
	if err != nil {
		t.Fatalf("ResolveIncremental failed: %v", err)
	}

	// Rebase should still be correct (idempotent on cloned entries)
	if incremental.ImportMap.Imports["@examples/kitchen-sink/"] != "/" {
		t.Errorf("Incremental rebase wrong: @examples/kitchen-sink/ = %s, want /",
			incremental.ImportMap.Imports["@examples/kitchen-sink/"])
	}
	if incremental.ImportMap.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Incremental lit wrong: %s", incremental.ImportMap.Imports["lit"])
	}
}
