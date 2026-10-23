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
	"slices"
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

func TestFindImportMapTag(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/find-importmap", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	loc := FindImportMapTag(html)

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Found        bool `json:"found"`
		TagStart     int  `json:"tag_start"`
		TagEnd       int  `json:"tag_end"`
		ContentStart int  `json:"content_start"`
		ContentEnd   int  `json:"content_end"`
		Line         int  `json:"line"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	if loc.Found != expected.Found {
		t.Errorf("Found: expected %v, got %v", expected.Found, loc.Found)
	}
	if loc.TagStart != expected.TagStart {
		t.Errorf("TagStart: expected %d, got %d", expected.TagStart, loc.TagStart)
	}
	if loc.TagEnd != expected.TagEnd {
		t.Errorf("TagEnd: expected %d, got %d", expected.TagEnd, loc.TagEnd)
	}
	if loc.ContentStart != expected.ContentStart {
		t.Errorf("ContentStart: expected %d, got %d", expected.ContentStart, loc.ContentStart)
	}
	if loc.ContentEnd != expected.ContentEnd {
		t.Errorf("ContentEnd: expected %d, got %d", expected.ContentEnd, loc.ContentEnd)
	}
	if loc.Line != expected.Line {
		t.Errorf("Line: expected %d, got %d", expected.Line, loc.Line)
	}

	// Verify the content extraction makes sense
	if loc.Found {
		content := string(html[loc.ContentStart:loc.ContentEnd])
		if !strings.Contains(content, "manual-dep") {
			t.Errorf("Content should contain 'manual-dep', got: %q", content)
		}
	}
}

func TestFindImportMapTag_NotFound(t *testing.T) {
	html := []byte(`<!DOCTYPE html>
<html>
<head>
  <script type="module" src="./app.js"></script>
</head>
<body></body>
</html>`)

	loc := FindImportMapTag(html)
	if loc.Found {
		t.Errorf("Expected Found=false for HTML without import map")
	}
}

func TestFindImportMapTag_Empty(t *testing.T) {
	html := []byte(`<!DOCTYPE html>
<html>
<head>
  <script type="importmap"></script>
</head>
</html>`)

	loc := FindImportMapTag(html)
	if !loc.Found {
		t.Fatalf("Expected to find empty import map tag")
	}
	if loc.ContentStart != loc.ContentEnd {
		t.Errorf("Empty import map should have ContentStart == ContentEnd, got %d != %d",
			loc.ContentStart, loc.ContentEnd)
	}
}

func TestFindInsertPoint(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/find-insertpoint", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	pt := FindInsertPoint(html)

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Found  bool   `json:"found"`
		Offset int    `json:"offset"`
		Indent string `json:"indent"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	if pt.Found != expected.Found {
		t.Errorf("Found: expected %v, got %v", expected.Found, pt.Found)
	}
	if pt.Offset != expected.Offset {
		t.Errorf("Offset: expected %d, got %d", expected.Offset, pt.Offset)
	}
	if pt.Indent != expected.Indent {
		t.Errorf("Indent: expected %q, got %q", expected.Indent, pt.Indent)
	}

	// Verify the insertion point is before the script tag
	if pt.Found {
		afterInsert := string(html[pt.Offset:])
		if !strings.HasPrefix(afterInsert, "<script") {
			t.Errorf("Insert point should be before <script>, got: %q...", afterInsert[:min(50, len(afterInsert))])
		}
	}
}

func TestFindInsertPoint_NoScripts(t *testing.T) {
	html := []byte(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Test</title>
</head>
<body></body>
</html>`)

	pt := FindInsertPoint(html)
	if !pt.Found {
		t.Fatalf("Expected to find insert point before </head>")
	}

	// Insert point should be before </head>
	afterInsert := string(html[pt.Offset:])
	if !strings.HasPrefix(afterInsert, "</head>") {
		t.Errorf("Insert point should be before </head>, got: %q...", afterInsert[:min(50, len(afterInsert))])
	}
}

func TestFindInsertPoint_NoHead(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/find-insertpoint-no-head", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Found  bool   `json:"found"`
		Offset int    `json:"offset"`
		Indent string `json:"indent"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	pt := FindInsertPoint(html)
	if !pt.Found {
		t.Fatal("Expected a fallback insert point for HTML without head")
	}
	if pt.Found != expected.Found {
		t.Errorf("Found: expected %v, got %v", expected.Found, pt.Found)
	}
	if pt.Offset != expected.Offset {
		t.Errorf("Offset: expected %d, got %d", expected.Offset, pt.Offset)
	}
	if pt.Indent != expected.Indent {
		t.Errorf("Indent: expected %q, got %q", expected.Indent, pt.Indent)
	}
	if !strings.HasPrefix(string(html[pt.Offset:]), "</body>") {
		t.Errorf("Expected insertion before </body>, got: %q", html[pt.Offset:])
	}
}

func TestTraceHTML_CircularModules(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/circular-modules", "/test")

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

	tracer := NewTracer(mfs, "/test")
	graph, err := tracer.TraceHTML("/test/index.html")
	if err != nil {
		t.Fatalf("TraceHTML with circular modules failed: %v", err)
	}

	if len(graph.Modules) != len(expected.Modules) {
		t.Errorf("Expected %d modules, got %d", len(expected.Modules), len(graph.Modules))
		for path := range graph.Modules {
			t.Logf("  Module: %s", path)
		}
	}

	if len(graph.Entrypoints) != len(expected.Entrypoints) {
		t.Errorf("Expected %d entrypoints, got %d", len(expected.Entrypoints), len(graph.Entrypoints))
	}

	bareSpecs := graph.BareSpecifiers()
	if len(bareSpecs) != len(expected.BareSpecifiers) {
		t.Errorf("Expected %d bare specifiers, got %d: %v", len(expected.BareSpecifiers), len(bareSpecs), bareSpecs)
	}
}

func TestTraceSingleSelfImports(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/self-import", "/test")

	result, err := TraceSingle(mfs, "/test/index.html", "/test", Options{})
	if err != nil {
		t.Fatalf("TraceSingle failed: %v", err)
	}
	if got := result.ImportMap.Imports["my-lib"]; got != "/index.js" {
		t.Errorf("self import my-lib = %q, want /index.js (imports: %v)", got, result.ImportMap.Imports)
	}
	if got := result.ImportMap.Imports["my-lib/button"]; got != "/button.js" {
		t.Errorf("self import my-lib/button = %q, want /button.js (imports: %v)", got, result.ImportMap.Imports)
	}
}

func TestIsBareSpecifierClassification(t *testing.T) {
	tests := []struct {
		spec string
		bare bool
	}{
		{"lit", true},
		{"lit/decorators.js", true},
		{"./local.js", false},
		{"../local.js", false},
		{"/root.js", false},
		{"node:fs", false},
		{"data:text/javascript,export{}", false},
		{"https://example.com/mod.js", false},
		{"//example.com/mod.js", false},
		{"#internal", false},
	}
	for _, tt := range tests {
		if got := isBareSpecifier(tt.spec); got != tt.bare {
			t.Errorf("isBareSpecifier(%q) = %v, want %v", tt.spec, got, tt.bare)
		}
	}
}

func TestHTMLTypeCaseInsensitive(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/html-case", "/test")
	content, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	if loc := FindImportMapTag(content); !loc.Found {
		t.Error("expected type=ImportMap to be recognized")
	}

	scripts, err := ExtractScripts(content)
	if err != nil {
		t.Fatalf("ExtractScripts failed: %v", err)
	}
	foundModule := false
	for _, s := range scripts {
		if s.Type == "module" {
			foundModule = true
			if !slices.Contains(s.Imports, "lit") {
				t.Errorf("expected inline module imports to be extracted, got %v", s.Imports)
			}
		}
	}
	if !foundModule {
		t.Error("expected type=MODULE to be normalized to module")
	}
}

func TestFindImportMapIgnoresInertElements(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/inert-importmap", "/test")
	content, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}
	loc := FindImportMapTag(content)
	if !loc.Found {
		t.Fatal("expected to find a live import map")
	}
	got := string(content[loc.ContentStart:loc.ContentEnd])
	if !strings.Contains(got, "good") {
		t.Errorf("expected live import map, got %q", got)
	}
}

func TestFindInsertPointWithoutHead(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/no-head", "/test")
	content, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}
	point := FindInsertPoint(content)
	if !point.Found {
		t.Fatal("expected a fallback insertion point without <head>")
	}
	before := string(content[:point.Offset])
	if !strings.Contains(before, "<body>") || strings.Contains(before, "<script") {
		t.Errorf("expected insertion before first script in body, offset %d: %q", point.Offset, before)
	}
}

func TestTraceIgnoresURLAndSubpathSpecifiers(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/url-specifiers", "/test")
	graph, err := NewTracer(mfs, "/test").TraceHTML("/test/index.html")
	if err != nil {
		t.Fatalf("TraceHTML failed: %v", err)
	}
	if specs := graph.BareSpecifiers(); len(specs) != 0 {
		t.Errorf("URL and # specifiers must not be treated as bare: %v", specs)
	}
	if len(graph.Errors) != 0 {
		t.Errorf("expected no graph errors, got: %v", graph.Errors)
	}
}

func TestTraceRejectsPathsOutsideRoot(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/path-traversal", "/test")
	graph, err := NewTracer(mfs, "/test").TraceHTML("/test/index.html")
	if err != nil {
		t.Fatalf("TraceHTML failed: %v", err)
	}
	if len(graph.Errors) == 0 {
		t.Fatal("expected a containment error for a path outside the root")
	}
	if !strings.Contains(graph.Errors[0].Error(), "outside allowed roots") {
		t.Errorf("unexpected error: %v", graph.Errors[0])
	}
}

func TestResolvePathStripsQueryAndFragment(t *testing.T) {
	tr := NewTracer(nil, "/site")
	if got := tr.resolvePath("/site/js", "./app.js?v=1#frag"); got != "/site/js/app.js" {
		t.Errorf("relative path with suffix = %q", got)
	}
	if got := tr.resolvePath("/site/js", "/assets/app.js?v=1"); got != "/site/assets/app.js" {
		t.Errorf("absolute path with suffix = %q", got)
	}
}

func TestTraceSingleWorkspaceImport(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/workspace", "/test")
	result, err := TraceSingle(mfs, "/test/packages/app/index.html", "/test/packages/app", Options{})
	if err != nil {
		t.Fatalf("TraceSingle failed: %v", err)
	}
	if got := result.ImportMap.Imports["@ws/lib"]; got != "/packages/lib/index.js" {
		t.Errorf("@ws/lib = %q, want workspace source path (imports: %v)", got, result.ImportMap.Imports)
	}
}

func TestTraceConditions(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/conditions", "/test")

	browser, err := TraceSingle(mfs, "/test/index.html", "/test", Options{Conditions: []string{"browser"}})
	if err != nil {
		t.Fatalf("TraceSingle with browser condition failed: %v", err)
	}
	if got := browser.ImportMap.Imports["cond-pkg"]; got != "/node_modules/cond-pkg/browser.js" {
		t.Errorf("browser condition = %q", got)
	}

	def, err := TraceSingle(mfs, "/test/index.html", "/test", Options{Conditions: []string{"default"}})
	if err != nil {
		t.Fatalf("TraceSingle with default condition failed: %v", err)
	}
	if got := def.ImportMap.Imports["cond-pkg"]; got != "/node_modules/cond-pkg/index.js" {
		t.Errorf("default condition = %q", got)
	}
}

func TestTraceSingleFiltersUnrelatedScopes(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/scope-closure", "/test")
	result, err := TraceSingle(mfs, "/test/index.html", "/test", Options{})
	if err != nil {
		t.Fatalf("TraceSingle failed: %v", err)
	}
	if got := result.ImportMap.Imports["lit"]; got == "" {
		t.Fatalf("expected lit import, got %v", result.ImportMap.Imports)
	}
	if _, ok := result.ImportMap.Scopes["/node_modules/lit/"]; !ok {
		t.Errorf("expected lit scope in %v", result.ImportMap.Scopes)
	}
	if _, ok := result.ImportMap.Scopes["/node_modules/@lit/reactive-element/"]; !ok {
		t.Errorf("expected transitive reactive-element scope in %v", result.ImportMap.Scopes)
	}
	if _, ok := result.ImportMap.Scopes["/node_modules/used/"]; !ok {
		t.Errorf("expected scope for second specifier in %v", result.ImportMap.Scopes)
	}
	for scope := range result.ImportMap.Scopes {
		if strings.Contains(scope, "unrelated") {
			t.Errorf("unrelated scope leaked into map: %q (all: %v)", scope, result.ImportMap.Scopes)
		}
	}
	if got := result.ImportMap.Scopes["/node_modules/lit/"]["@lit/reactive-element"]; got != "/node_modules/@lit/reactive-element/index.js" {
		t.Errorf("lit scope lost transitive resolution: %q", got)
	}
}

func TestTraceBatchFiltersUnrelatedScopes(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/scope-closure", "/test")
	opts := Options{}
	for result := range TraceBatch(mfs, []string{"/test/index.html", "/test/other.html"}, "/test", opts) {
		if result.Error != "" {
			t.Fatalf("unexpected error: %s", result.Error)
		}
		for scope := range result.Scopes {
			if strings.Contains(scope, "unrelated") {
				t.Errorf("unrelated scope leaked into batch result: %q (%v)", scope, result.Scopes)
			}
		}
		if got := result.Scopes["/node_modules/lit/"]["@lit/reactive-element"]; got != "/node_modules/@lit/reactive-element/index.js" {
			t.Errorf("batch result lost transitive resolution: %q", got)
		}
	}
}

// TestTraceFiltersUnrelatedWildcardTrailingSlash ensures a package with a
// wildcard export does not leak its trailing-slash key (and therefore its
// scope) into the map when the page never imports it.
func TestTraceFiltersUnrelatedWildcardTrailingSlash(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/scope-closure-wildcard", "/test")
	result, err := TraceSingle(mfs, "/test/index.html", "/test", Options{})
	if err != nil {
		t.Fatalf("TraceSingle failed: %v", err)
	}
	if _, ok := result.ImportMap.Imports["unrelated/"]; ok {
		t.Errorf("unrelated wildcard key leaked into imports: %v", result.ImportMap.Imports)
	}
	for scope := range result.ImportMap.Scopes {
		if strings.Contains(scope, "unrelated") {
			t.Errorf("unrelated wildcard scope leaked into map: %q (%v)", scope, result.ImportMap.Scopes)
		}
	}
	// Reachability for the packages that are imported must still work.
	if got := result.ImportMap.Scopes["/node_modules/lit/"]["@lit/reactive-element"]; got != "/node_modules/@lit/reactive-element/index.js" {
		t.Errorf("lit lost transitive resolution: %q", got)
	}
	if got := result.ImportMap.Scopes["/node_modules/@lit/reactive-element/"]["lit-html"]; got != "/node_modules/lit-html/index.js" {
		t.Errorf("reactive-element lost transitive resolution: %q", got)
	}
}

// TestTraceConditionsTransitiveScope covers tracer-side condition selection:
// the entry file picked by the condition imports a different bare specifier,
// and the condition also changes the transitive scope entry.
func TestTraceConditionsTransitiveScope(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/conditions", "/test")

	browser, err := TraceSingle(mfs, "/test/index.html", "/test", Options{Conditions: []string{"browser"}})
	if err != nil {
		t.Fatalf("TraceSingle with browser condition failed: %v", err)
	}
	if _, ok := browser.ImportMap.Imports["browser-only"]; !ok {
		t.Errorf("browser trace should follow browser-only, got %v", browser.ImportMap.Imports)
	}
	if _, ok := browser.ImportMap.Imports["default-only"]; ok {
		t.Errorf("browser trace should not follow default-only, got %v", browser.ImportMap.Imports)
	}
	if got := browser.ImportMap.Scopes["/node_modules/cond-pkg/"]["cond-child"]; got != "/node_modules/cond-child/browser.js" {
		t.Errorf("browser transitive scope = %q, want browser.js (%v)", got, browser.ImportMap.Scopes)
	}

	def, err := TraceSingle(mfs, "/test/index.html", "/test", Options{Conditions: []string{"default"}})
	if err != nil {
		t.Fatalf("TraceSingle with default condition failed: %v", err)
	}
	if _, ok := def.ImportMap.Imports["default-only"]; !ok {
		t.Errorf("default trace should follow default-only, got %v", def.ImportMap.Imports)
	}
	if _, ok := def.ImportMap.Imports["browser-only"]; ok {
		t.Errorf("default trace should not follow browser-only, got %v", def.ImportMap.Imports)
	}
	if got := def.ImportMap.Scopes["/node_modules/cond-pkg/"]["cond-child"]; got != "/node_modules/cond-child/index.js" {
		t.Errorf("default transitive scope = %q, want index.js (%v)", got, def.ImportMap.Scopes)
	}
}

// TestTraceWorkspaceNodeModules covers following a bare specifier into the
// workspace-root node_modules tree from a workspace member.
func TestTraceWorkspaceNodeModules(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/workspace", "/test")
	result, err := TraceSingle(mfs, "/test/packages/app/index.html", "/test/packages/app", Options{})
	if err != nil {
		t.Fatalf("TraceSingle failed: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected node_modules to be traceable, got errors: %v", result.Errors)
	}
	if got := result.ImportMap.Imports["lit"]; got != "/node_modules/lit/index.js" {
		t.Errorf("lit = %q, want node_modules path (%v)", got, result.ImportMap.Imports)
	}
	if got := result.ImportMap.Scopes["/node_modules/lit/"]["@lit/reactive-element"]; got != "/node_modules/@lit/reactive-element/index.js" {
		t.Errorf("lit transitive scope = %q (%v)", got, result.ImportMap.Scopes)
	}
}

// TestTraceBatchResultsAreIndependent ensures the per-specifier-set memo does
// not hand out maps that siblings mutate through one another.
func TestTraceBatchResultsAreIndependent(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/scope-closure", "/test")
	var results []BatchResult
	for result := range TraceBatch(mfs, []string{"/test/index.html", "/test/other.html"}, "/test", Options{Parallel: 1}) {
		if result.Error != "" {
			t.Fatalf("unexpected error: %s", result.Error)
		}
		results = append(results, result)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	results[0].Imports["lit"] = "mutated"
	for scope := range results[0].Scopes {
		results[0].Scopes[scope]["@lit/reactive-element"] = "mutated"
	}

	for _, result := range results[1:] {
		if got := result.Imports["lit"]; got == "mutated" {
			t.Errorf("mutating one result changed a sibling's imports")
		}
		for scope, entries := range result.Scopes {
			if entries["@lit/reactive-element"] == "mutated" {
				t.Errorf("mutating one result changed a sibling's scope %q", scope)
			}
		}
	}
}

func TestExtractScriptsIgnoresInertSubtrees(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/inert-scripts", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}
	scripts, err := ExtractScripts(html)
	if err != nil {
		t.Fatalf("ExtractScripts failed: %v", err)
	}
	if len(scripts) != 1 {
		t.Fatalf("expected only the live script, got %d: %+v", len(scripts), scripts)
	}
	if scripts[0].Src != "./live.js" {
		t.Errorf("expected ./live.js, got %q", scripts[0].Src)
	}
}

func TestFindInsertPointIgnoresInertScripts(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/inert-insertpoint", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}
	point := FindInsertPoint(html)
	if !point.Found {
		t.Fatal("expected an insert point")
	}
	after := string(html[point.Offset:])
	if !strings.HasPrefix(after, "</head>") {
		t.Errorf("expected insertion before </head>, got %q", after)
	}
}
