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
package packagejson_test

import (
	"encoding/json"
	"testing"

	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/testutil"
)

func TestParseFile(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"simple exports", "simple-exports"},
		{"subpath exports", "subpath-exports"},
		{"wildcard exports", "wildcard-exports"},
		{"conditional exports", "conditional-exports"},
		{"nested conditions", "nested-conditions"},
		{"main fallback", "main-fallback"},
		{"no exports", "no-exports"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := testutil.NewFixtureFS(t, "packagejson/"+tt.dir, "/test")

			pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
			if err != nil {
				t.Fatalf("ParseFile failed: %v", err)
			}

			if pkg.Name == "" {
				t.Error("Expected package name to be parsed")
			}
		})
	}
}

func TestResolveExport(t *testing.T) {
	t.Run("simple string export", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "packagejson/simple-exports", "/test")

		pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		expectedBytes, err := mfs.ReadFile("/test/expected.json")
		if err != nil {
			t.Fatalf("Failed to read expected.json: %v", err)
		}

		var expected struct {
			Specifier string `json:"specifier"`
			Resolved  string `json:"resolved"`
		}
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			t.Fatalf("Failed to parse expected.json: %v", err)
		}

		resolved, err := pkg.ResolveExport(".", nil)
		if err != nil {
			t.Fatalf("ResolveExport failed: %v", err)
		}
		if resolved != expected.Resolved {
			t.Errorf("Expected %q, got %q", expected.Resolved, resolved)
		}
	})

	t.Run("subpath exports", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "packagejson/subpath-exports", "/test")

		pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		expectedBytes, err := mfs.ReadFile("/test/expected.json")
		if err != nil {
			t.Fatalf("Failed to read expected.json: %v", err)
		}

		var expected struct {
			Exports map[string]string `json:"exports"`
		}
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			t.Fatalf("Failed to parse expected.json: %v", err)
		}

		for subpath, expectedResolved := range expected.Exports {
			resolved, err := pkg.ResolveExport(subpath, nil)
			if err != nil {
				t.Errorf("ResolveExport(%q) failed: %v", subpath, err)
				continue
			}
			if resolved != expectedResolved {
				t.Errorf("ResolveExport(%q) = %q, want %q", subpath, resolved, expectedResolved)
			}
		}
	})

	t.Run("conditional exports", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "packagejson/conditional-exports", "/test")

		pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		expectedBytes, err := mfs.ReadFile("/test/expected.json")
		if err != nil {
			t.Fatalf("Failed to read expected.json: %v", err)
		}

		var expected struct {
			Resolved string `json:"resolved"`
		}
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			t.Fatalf("Failed to parse expected.json: %v", err)
		}

		resolved, err := pkg.ResolveExport(".", nil)
		if err != nil {
			t.Fatalf("ResolveExport failed: %v", err)
		}
		if resolved != expected.Resolved {
			t.Errorf("Expected %q, got %q", expected.Resolved, resolved)
		}
	})

	t.Run("nested conditions", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "packagejson/nested-conditions", "/test")

		pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		expectedBytes, err := mfs.ReadFile("/test/expected.json")
		if err != nil {
			t.Fatalf("Failed to read expected.json: %v", err)
		}

		var expected struct {
			Resolved string `json:"resolved"`
		}
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			t.Fatalf("Failed to parse expected.json: %v", err)
		}

		resolved, err := pkg.ResolveExport(".", nil)
		if err != nil {
			t.Fatalf("ResolveExport failed: %v", err)
		}
		if resolved != expected.Resolved {
			t.Errorf("Expected %q, got %q", expected.Resolved, resolved)
		}
	})

	t.Run("main fallback", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "packagejson/main-fallback", "/test")

		pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		expectedBytes, err := mfs.ReadFile("/test/expected.json")
		if err != nil {
			t.Fatalf("Failed to read expected.json: %v", err)
		}

		var expected struct {
			Resolved string `json:"resolved"`
		}
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			t.Fatalf("Failed to parse expected.json: %v", err)
		}

		resolved, err := pkg.ResolveExport(".", nil)
		if err != nil {
			t.Fatalf("ResolveExport failed: %v", err)
		}
		if resolved != expected.Resolved {
			t.Errorf("Expected %q, got %q", expected.Resolved, resolved)
		}
	})
}

func TestExportEntries(t *testing.T) {
	t.Run("subpath exports enumeration", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "packagejson/subpath-exports", "/test")

		pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
		if err != nil {
			t.Fatalf("ParseFile failed: %v", err)
		}

		expectedBytes, err := mfs.ReadFile("/test/expected.json")
		if err != nil {
			t.Fatalf("Failed to read expected.json: %v", err)
		}

		var expected struct {
			Exports map[string]string `json:"exports"`
		}
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			t.Fatalf("Failed to parse expected.json: %v", err)
		}

		entries := pkg.ExportEntries(nil)
		if len(entries) != len(expected.Exports) {
			t.Errorf("Expected %d export entries, got %d", len(expected.Exports), len(entries))
		}

		found := make(map[string]bool)
		for _, e := range entries {
			found[e.Subpath] = true
		}

		for subpath := range expected.Exports {
			if !found[subpath] {
				t.Errorf("Missing export entry for %q", subpath)
			}
		}
	})
}

func TestWildcardExports(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "packagejson/wildcard-exports", "/test")

	pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Wildcard struct {
			Pattern string `json:"pattern"`
			Target  string `json:"target"`
		} `json:"wildcard"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	wildcards := pkg.WildcardExports(nil)
	if len(wildcards) != 1 {
		t.Fatalf("Expected 1 wildcard export, got %d", len(wildcards))
	}

	w := wildcards[0]
	if w.Pattern != expected.Wildcard.Pattern {
		t.Errorf("Expected pattern %q, got %q", expected.Wildcard.Pattern, w.Pattern)
	}
	if w.Target != expected.Wildcard.Target {
		t.Errorf("Expected target %q, got %q", expected.Wildcard.Target, w.Target)
	}
}

func TestHasTrailingSlashExport(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		expected bool
	}{
		{"wildcard exports", "wildcard-exports", true},
		{"main fallback", "main-fallback", true},
		{"no exports", "no-exports", true},
		{"subpath exports (no wildcard)", "subpath-exports", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := testutil.NewFixtureFS(t, "packagejson/"+tt.dir, "/test")

			pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
			if err != nil {
				t.Fatalf("ParseFile failed: %v", err)
			}

			if pkg.HasTrailingSlashExport(nil) != tt.expected {
				t.Errorf("HasTrailingSlashExport() = %v, want %v", pkg.HasTrailingSlashExport(nil), tt.expected)
			}
		})
	}
}

func TestCustomConditions(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "packagejson/production-condition", "/test")

	pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	tests := []struct {
		name       string
		conditions []string
		expected   string
	}{
		{"default conditions", nil, "dist/browser.js"},
		{"production first", []string{"production", "browser", "default"}, "dist/prod.js"},
		{"development first", []string{"development", "browser", "default"}, "dist/dev.js"},
		{"browser first", []string{"browser", "production", "default"}, "dist/browser.js"},
		{"default only", []string{"default"}, "dist/index.js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts *packagejson.ResolveOptions
			if tt.conditions != nil {
				opts = &packagejson.ResolveOptions{Conditions: tt.conditions}
			}

			resolved, err := pkg.ResolveExport(".", opts)
			if err != nil {
				t.Fatalf("ResolveExport failed: %v", err)
			}
			if resolved != tt.expected {
				t.Errorf("ResolveExport(\".\", %v) = %q, want %q", tt.conditions, resolved, tt.expected)
			}
		})
	}
}

func TestExportEntriesWithConditions(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "packagejson/production-condition", "/test")

	pkg, err := packagejson.ParseFile(mfs, "/test/package.json")
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	// With default conditions (browser first)
	entriesDefault := pkg.ExportEntries(nil)
	if len(entriesDefault) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entriesDefault))
	}
	if entriesDefault[0].Target != "dist/browser.js" {
		t.Errorf("Default conditions: expected dist/browser.js, got %s", entriesDefault[0].Target)
	}

	// With production conditions
	opts := &packagejson.ResolveOptions{Conditions: []string{"production", "default"}}
	entriesProd := pkg.ExportEntries(opts)
	if len(entriesProd) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entriesProd))
	}
	if entriesProd[0].Target != "dist/prod.js" {
		t.Errorf("Production conditions: expected dist/prod.js, got %s", entriesProd[0].Target)
	}
}

func TestWorkspacePatterns(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected []string
	}{
		{
			name:     "array format",
			json:     `{"name": "test", "workspaces": ["packages/*", "@scope/*"]}`,
			expected: []string{"packages/*", "@scope/*"},
		},
		{
			name:     "object format with packages key",
			json:     `{"name": "test", "workspaces": {"packages": ["libs/*"]}}`,
			expected: []string{"libs/*"},
		},
		{
			name:     "no workspaces field",
			json:     `{"name": "test"}`,
			expected: nil,
		},
		{
			name:     "empty array",
			json:     `{"name": "test", "workspaces": []}`,
			expected: []string{},
		},
		{
			name:     "empty object packages",
			json:     `{"name": "test", "workspaces": {"packages": []}}`,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, err := packagejson.Parse([]byte(tt.json))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			patterns := pkg.WorkspacePatterns()
			if len(patterns) != len(tt.expected) {
				t.Errorf("WorkspacePatterns() length = %d, want %d", len(patterns), len(tt.expected))
				return
			}

			for i, want := range tt.expected {
				if patterns[i] != want {
					t.Errorf("WorkspacePatterns()[%d] = %q, want %q", i, patterns[i], want)
				}
			}
		})
	}
}

func TestHasWorkspaces(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected bool
	}{
		{
			name:     "has workspaces array",
			json:     `{"name": "test", "workspaces": ["packages/*"]}`,
			expected: true,
		},
		{
			name:     "has workspaces object",
			json:     `{"name": "test", "workspaces": {"packages": ["libs/*"]}}`,
			expected: true,
		},
		{
			name:     "no workspaces",
			json:     `{"name": "test"}`,
			expected: false,
		},
		{
			name:     "empty workspaces array",
			json:     `{"name": "test", "workspaces": []}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, err := packagejson.Parse([]byte(tt.json))
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			if pkg.HasWorkspaces() != tt.expected {
				t.Errorf("HasWorkspaces() = %v, want %v", pkg.HasWorkspaces(), tt.expected)
			}
		})
	}
}
