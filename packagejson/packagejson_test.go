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

		resolved, err := pkg.ResolveExport(".")
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
			resolved, err := pkg.ResolveExport(subpath)
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

		resolved, err := pkg.ResolveExport(".")
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

		resolved, err := pkg.ResolveExport(".")
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

		resolved, err := pkg.ResolveExport(".")
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

		entries := pkg.ExportEntries()
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

	wildcards := pkg.WildcardExports()
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

			if pkg.HasTrailingSlashExport() != tt.expected {
				t.Errorf("HasTrailingSlashExport() = %v, want %v", pkg.HasTrailingSlashExport(), tt.expected)
			}
		})
	}
}
