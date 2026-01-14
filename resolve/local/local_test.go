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
	warnings []string
	debugs   []string
}

func (m *mockLogger) Warning(format string, args ...any) {
	m.warnings = append(m.warnings, fmt.Sprintf(format, args...))
}

func (m *mockLogger) Debug(format string, args ...any) {
	m.debugs = append(m.debugs, fmt.Sprintf(format, args...))
}

func TestResolver(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"simple package", "simple-pkg"},
		{"with scopes", "with-scopes"},
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
	expectedWarning := "Package 'broken-lib' has no exports or main field, only subpath imports will work"
	if !slices.Contains(logger.warnings, expectedWarning) {
		t.Errorf("Expected warning %q, got warnings: %v", expectedWarning, logger.warnings)
	}
}
