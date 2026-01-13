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
package trace

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"bennypowers.dev/mappa/testutil"
)

func TestExtractScripts(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/extract-scripts", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	scripts, err := ExtractScripts(html)
	if err != nil {
		t.Fatalf("ExtractScripts failed: %v", err)
	}

	// Load expected output
	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Scripts []struct {
			Type    string   `json:"type"`
			Src     string   `json:"src"`
			Inline  bool     `json:"inline"`
			Imports []string `json:"imports"`
		} `json:"scripts"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	if len(scripts) != len(expected.Scripts) {
		t.Fatalf("Expected %d scripts, got %d", len(expected.Scripts), len(scripts))
	}

	for i, exp := range expected.Scripts {
		if scripts[i].Type != exp.Type {
			t.Errorf("Script %d: expected type %q, got %q", i, exp.Type, scripts[i].Type)
		}
		if scripts[i].Src != exp.Src {
			t.Errorf("Script %d: expected src %q, got %q", i, exp.Src, scripts[i].Src)
		}
		if scripts[i].Inline != exp.Inline {
			t.Errorf("Script %d: expected Inline=%v, got %v", i, exp.Inline, scripts[i].Inline)
		}
		if len(scripts[i].Imports) != len(exp.Imports) {
			t.Errorf("Script %d: expected %d imports, got %d", i, len(exp.Imports), len(scripts[i].Imports))
		} else {
			for j, imp := range exp.Imports {
				if scripts[i].Imports[j] != imp {
					t.Errorf("Script %d import %d: expected %q, got %q", i, j, imp, scripts[i].Imports[j])
				}
			}
		}
	}
}

func TestExtractImports(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/extract-imports", "/test")
	js, err := mfs.ReadFile("/test/module.js")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	imports, err := ExtractImports(js)
	if err != nil {
		t.Fatalf("ExtractImports failed: %v", err)
	}

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Imports []struct {
			Specifier string `json:"specifier"`
			Dynamic   bool   `json:"dynamic"`
		} `json:"imports"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	if len(imports) != len(expected.Imports) {
		t.Fatalf("Expected %d imports, got %d: %+v", len(expected.Imports), len(imports), imports)
	}

	for i, exp := range expected.Imports {
		if imports[i].Specifier != exp.Specifier {
			t.Errorf("Import %d: expected specifier %q, got %q", i, exp.Specifier, imports[i].Specifier)
		}
		if imports[i].IsDynamic != exp.Dynamic {
			t.Errorf("Import %d: expected IsDynamic=%v, got %v", i, exp.Dynamic, imports[i].IsDynamic)
		}
	}
}

func TestExtractImports_ReExports(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/extract-reexports", "/test")
	js, err := mfs.ReadFile("/test/module.js")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	imports, err := ExtractImports(js)
	if err != nil {
		t.Fatalf("ExtractImports failed: %v", err)
	}

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Imports []struct {
			Specifier string `json:"specifier"`
			Dynamic   bool   `json:"dynamic"`
		} `json:"imports"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	if len(imports) != len(expected.Imports) {
		t.Fatalf("Expected %d imports, got %d: %+v", len(expected.Imports), len(imports), imports)
	}

	for i, exp := range expected.Imports {
		if imports[i].Specifier != exp.Specifier {
			t.Errorf("Import %d: expected %q, got %q", i, exp.Specifier, imports[i].Specifier)
		}
		if imports[i].IsDynamic {
			t.Errorf("Import %d: re-exports should not be dynamic", i)
		}
	}
}

func TestExtractImports_TypeScript(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/extract-typescript", "/test")
	ts, err := mfs.ReadFile("/test/module.ts")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	imports, err := ExtractImports(ts)
	if err != nil {
		t.Fatalf("ExtractImports failed: %v", err)
	}

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Imports []struct {
			Specifier string `json:"specifier"`
			Dynamic   bool   `json:"dynamic"`
		} `json:"imports"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	if len(imports) != len(expected.Imports) {
		t.Fatalf("Expected %d imports, got %d: %+v", len(expected.Imports), len(imports), imports)
	}

	for i, exp := range expected.Imports {
		if imports[i].Specifier != exp.Specifier {
			t.Errorf("Expected %q, got %q", exp.Specifier, imports[i].Specifier)
		}
	}
}

func TestTraceHTML(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/graph", "/test")

	tracer := NewTracer(mfs, "/test")
	graph, err := tracer.TraceHTML("/test/index.html")
	if err != nil {
		t.Fatalf("TraceHTML failed: %v", err)
	}

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Entrypoints    []string `json:"entrypoints"`
		Modules        []string `json:"modules"`
		BareSpecifiers []string `json:"bare_specifiers"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	// Check entrypoints
	if len(graph.Entrypoints) != len(expected.Entrypoints) {
		t.Errorf("Expected %d entrypoints, got %d", len(expected.Entrypoints), len(graph.Entrypoints))
	}

	// Check modules count
	if len(graph.Modules) != len(expected.Modules) {
		t.Errorf("Expected %d modules, got %d", len(expected.Modules), len(graph.Modules))
		for path := range graph.Modules {
			t.Logf("  Module: %s", path)
		}
	}

	// Check bare specifiers
	bareSpecs := graph.BareSpecifiers()
	sort.Strings(bareSpecs)
	sort.Strings(expected.BareSpecifiers)

	if len(bareSpecs) != len(expected.BareSpecifiers) {
		t.Errorf("Expected %d bare specifiers, got %d: %v", len(expected.BareSpecifiers), len(bareSpecs), bareSpecs)
	}

	for i, exp := range expected.BareSpecifiers {
		if i >= len(bareSpecs) {
			break
		}
		if bareSpecs[i] != exp {
			t.Errorf("Bare specifier %d: expected %q, got %q", i, exp, bareSpecs[i])
		}
	}
}

func TestPackageNames(t *testing.T) {
	graph := &ModuleGraph{
		bareSpecifiers: map[string]bool{
			"lit":                         true,
			"lit/decorators.js":           true,
			"lit/directives/class-map.js": true,
			"@lit/reactive-element":       true,
			"@scope/package/subpath":      true,
		},
	}

	packages := graph.PackageNames()
	sort.Strings(packages)

	expected := []string{
		"@lit/reactive-element",
		"@scope/package",
		"lit",
	}

	if len(packages) != len(expected) {
		t.Fatalf("Expected %d packages, got %d: %v", len(expected), len(packages), packages)
	}

	for i, exp := range expected {
		if packages[i] != exp {
			t.Errorf("Package %d: expected %q, got %q", i, exp, packages[i])
		}
	}
}

func TestIsBareSpecifier(t *testing.T) {
	tests := []struct {
		specifier string
		expected  bool
	}{
		{"lit", true},
		{"lit/decorators.js", true},
		{"@scope/package", true},
		{"./local.js", false},
		{"../parent.js", false},
		{"/absolute.js", false},
		{"https://example.com/module.js", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isBareSpecifier(tt.specifier)
		if result != tt.expected {
			t.Errorf("isBareSpecifier(%q) = %v, expected %v", tt.specifier, result, tt.expected)
		}
	}
}

func TestTraceModule(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/graph", "/test")

	tracer := NewTracer(mfs, "/test")
	graph, err := tracer.TraceModule("/test/components/nav.js")
	if err != nil {
		t.Fatalf("TraceModule failed: %v", err)
	}

	// nav.js imports lit, lit/decorators.js, and lit/directives/class-map.js
	expectedBare := []string{"lit", "lit/decorators.js", "lit/directives/class-map.js"}
	bareSpecs := graph.BareSpecifiers()
	sort.Strings(bareSpecs)
	sort.Strings(expectedBare)

	if len(bareSpecs) != len(expectedBare) {
		t.Errorf("Expected %d bare specifiers, got %d: %v", len(expectedBare), len(bareSpecs), bareSpecs)
	}

	// Should only have one module (nav.js itself)
	if len(graph.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(graph.Modules))
	}

	// Check entrypoint is set
	if len(graph.Entrypoints) != 1 || !strings.HasSuffix(graph.Entrypoints[0], "nav.js") {
		t.Errorf("Expected nav.js as entrypoint, got %v", graph.Entrypoints)
	}
}

func TestResolvePath(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/graph", "/project")
	tracer := NewTracer(mfs, "/project")

	tests := []struct {
		baseDir   string
		specifier string
		expected  string
	}{
		{"/project", "./app.js", filepath.Join("/project", "app.js")},
		{"/project/src", "../lib.js", filepath.Join("/project", "lib.js")},
		{"/project/src", "/vendor/lib.js", filepath.Join("/project", "vendor/lib.js")}, // web-style absolute
	}

	for _, tt := range tests {
		result := tracer.resolvePath(tt.baseDir, tt.specifier)
		if result != tt.expected {
			t.Errorf("resolvePath(%q, %q) = %q, expected %q", tt.baseDir, tt.specifier, result, tt.expected)
		}
	}
}
