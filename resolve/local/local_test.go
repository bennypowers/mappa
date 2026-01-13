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
	"reflect"
	"testing"

	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/internal/mapfs"
	"bennypowers.dev/mappa/resolve"
	"bennypowers.dev/mappa/resolve/local"
	"bennypowers.dev/mappa/testutil"
)

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
