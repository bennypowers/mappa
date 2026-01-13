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
package resolve_test

import (
	"reflect"
	"testing"

	"bennypowers.dev/mappa/resolve"
)

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		wantErr   bool
		wantVars  []string
		wantPanic bool
	}{
		{
			name:     "standard template",
			pattern:  "/node_modules/{package}/{path}",
			wantVars: []string{"package", "path"},
		},
		{
			name:     "custom cdn template",
			pattern:  "https://cdn.example.com/{package}@{version}/{path}",
			wantVars: []string{"package", "version", "path"},
		},
		{
			name:     "scope and name separate",
			pattern:  "/libs/{scope}/{name}/{path}",
			wantVars: []string{"scope", "name", "path"},
		},
		{
			name:    "invalid variable",
			pattern: "/foo/{invalid}/{path}",
			wantErr: true,
		},
		{
			name:    "empty pattern",
			pattern: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := resolve.ParseTemplate(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if tmpl.Pattern() != tt.pattern {
					t.Errorf("Pattern() = %v, want %v", tmpl.Pattern(), tt.pattern)
				}
				if !reflect.DeepEqual(tmpl.Variables(), tt.wantVars) {
					t.Errorf("Variables() = %v, want %v", tmpl.Variables(), tt.wantVars)
				}
			}
		})
	}
}

func TestTemplate_Expand(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		pkg      string
		version  string
		path     string
		expected string
	}{
		{
			name:     "standard local",
			pattern:  "/node_modules/{package}/{path}",
			pkg:      "lit",
			version:  "2.0.0",
			path:     "index.js",
			expected: "/node_modules/lit/index.js",
		},
		{
			name:     "standard local with scope",
			pattern:  "/node_modules/{package}/{path}",
			pkg:      "@lit/reactive-element",
			version:  "1.0.0",
			path:     "decorators.js",
			expected: "/node_modules/@lit/reactive-element/decorators.js",
		},
		{
			name:     "split scope and name",
			pattern:  "/deps/{scope}/{name}/{path}",
			pkg:      "@lit/reactive-element",
			version:  "1.0.0",
			path:     "decorators.js",
			expected: "/deps/lit/reactive-element/decorators.js",
		},
		{
			name:     "unscoped package with scope var",
			pattern:  "/deps/{scope}/{name}/{path}",
			pkg:      "lit",
			version:  "2.0.0",
			path:     "index.js",
			expected: "/deps//lit/index.js", // Empty scope
		},
		{
			name:     "cdn style",
			pattern:  "https://unpkg.com/{package}@{version}/{path}",
			pkg:      "lit",
			version:  "2.0.0",
			path:     "index.js",
			expected: "https://unpkg.com/lit@2.0.0/index.js",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := resolve.ParseTemplate(tt.pattern)
			if err != nil {
				t.Fatalf("ParseTemplate failed: %v", err)
			}

			result := tmpl.Expand(tt.pkg, tt.version, tt.path)
			if result != tt.expected {
				t.Errorf("Expand() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTemplate_HasVersion(t *testing.T) {
	tests := []struct {
		pattern  string
		expected bool
	}{
		{"/node_modules/{package}/{path}", false},
		{"https://cdn.com/{package}@{version}/{path}", true},
	}

	for _, tt := range tests {
		tmpl, err := resolve.ParseTemplate(tt.pattern)
		if err != nil {
			t.Fatalf("ParseTemplate(%q) failed: %v", tt.pattern, err)
		}
		if tmpl.HasVersion() != tt.expected {
			t.Errorf("HasVersion() = %v, want %v", tmpl.HasVersion(), tt.expected)
		}
	}
}

func TestSplitPackageName(t *testing.T) {
	tests := []struct {
		pkg       string
		wantName  string
		wantScope string
	}{
		{"lit", "lit", ""},
		{"@lit/reactive-element", "reactive-element", "lit"},
		{"@scope/nested/package", "nested/package", "scope"}, // Should split on first slash only
		{"invalid-scope@", "invalid-scope@", ""},             // Not a valid scoped package, treat as name
	}

	for _, tt := range tests {
		name, scope := resolve.SplitPackageName(tt.pkg)
		if name != tt.wantName {
			t.Errorf("SplitPackageName(%q) name = %q, want %q", tt.pkg, name, tt.wantName)
		}
		if scope != tt.wantScope {
			t.Errorf("SplitPackageName(%q) scope = %q, want %q", tt.pkg, scope, tt.wantScope)
		}
	}
}
