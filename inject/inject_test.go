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
package inject

import (
	"strings"
	"testing"

	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/internal/mapfs"
	"bennypowers.dev/mappa/testutil"
	"bennypowers.dev/mappa/trace"
)

func TestBuildNewContent_InsertNew(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "inject/build-insert-new", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	im := &importmap.ImportMap{
		Imports: map[string]string{
			"lit": "/node_modules/lit/index.js",
		},
	}

	loc := trace.FindImportMapTag(html)
	result, inserted, err := buildNewContent(html, loc, im)
	if err != nil {
		t.Fatalf("buildNewContent failed: %v", err)
	}
	if !inserted {
		t.Error("Expected inserted=true for new import map")
	}

	testutil.UpdateGoldenFile(t, "inject/build-insert-new/expected.html", result)
	golden := testutil.LoadGoldenFile(t, "inject/build-insert-new/expected.html")
	if golden != nil && string(result) != string(golden) {
		t.Errorf("Output mismatch.\n  got:\n%s\n  want:\n%s", string(result), string(golden))
	}
}

func TestBuildNewContent_ReplaceExisting(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "inject/build-replace-existing", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	im := &importmap.ImportMap{
		Imports: map[string]string{
			"new-pkg": "/new-pkg/index.js",
		},
	}

	loc := trace.FindImportMapTag(html)
	if !loc.Found {
		t.Fatal("Expected to find existing import map tag")
	}

	result, inserted, err := buildNewContent(html, loc, im)
	if err != nil {
		t.Fatalf("buildNewContent failed: %v", err)
	}
	if inserted {
		t.Error("Expected inserted=false when replacing existing")
	}

	testutil.UpdateGoldenFile(t, "inject/build-replace-existing/expected.html", result)
	golden := testutil.LoadGoldenFile(t, "inject/build-replace-existing/expected.html")
	if golden != nil && string(result) != string(golden) {
		t.Errorf("Output mismatch.\n  got:\n%s\n  want:\n%s", string(result), string(golden))
	}
}

func TestBuildNewContent_NoHead(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "inject/build-no-head", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	im := &importmap.ImportMap{
		Imports: map[string]string{"lit": "/lit.js"},
	}

	loc := trace.FindImportMapTag(html)
	result, inserted, err := buildNewContent(html, loc, im)
	if err != nil {
		t.Fatalf("Expected fallback insertion without <head>, got: %v", err)
	}
	if !inserted {
		t.Error("Expected inserted=true")
	}
	if !strings.Contains(string(result), "importmap") {
		t.Errorf("Expected import map in output, got: %s", result)
	}
}

func TestBuildNewContent_EmptyMapEmitsValidJSON(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "inject/build-insert-new", "/test")
	html, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	loc := trace.FindImportMapTag(html)
	result, _, err := buildNewContent(html, loc, &importmap.ImportMap{})
	if err != nil {
		t.Fatalf("buildNewContent failed: %v", err)
	}
	if !strings.Contains(string(result), "{}") {
		t.Errorf("expected empty map to serialize as {}, got: %s", result)
	}
}

func TestExtractIndent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		offset   int
		expected string
	}{
		{
			name:     "two spaces",
			content:  "\n  </script>",
			offset:   3,
			expected: "  ",
		},
		{
			name:     "tab indent",
			content:  "\n\t\t</script>",
			offset:   3,
			expected: "\t\t",
		},
		{
			name:     "no indent",
			content:  "\n</script>",
			offset:   1,
			expected: "",
		},
		{
			name:     "at start of content",
			content:  "  </script>",
			offset:   2,
			expected: "  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractIndent([]byte(tt.content), tt.offset)
			if result != tt.expected {
				t.Errorf("extractIndent(%q, %d) = %q, want %q", tt.content, tt.offset, result, tt.expected)
			}
		})
	}
}

func TestIndentLines(t *testing.T) {
	input := "{\n  \"imports\": {}\n}"
	result := indentLines(input, "    ")
	for line := range strings.SplitSeq(result, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "    ") {
			t.Errorf("Expected line to start with 4 spaces, got %q", line)
		}
	}
}

func TestIndentLines_EmptyLines(t *testing.T) {
	input := "line1\n\nline3"
	result := indentLines(input, "  ")
	lines := strings.Split(result, "\n")
	if lines[1] != "" {
		t.Errorf("Empty lines should remain empty, got %q", lines[1])
	}
}

func TestInjectBatch_DryRun(t *testing.T) {
	mfs := setupInjectFixture(t)

	results := InjectBatch(mfs, []string{"/project/index.html"}, "/project", Options{
		DryRun: true,
	})

	var count int
	for r := range results {
		count++
		if r.Error != "" {
			t.Errorf("Unexpected error: %s", r.Error)
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 result, got %d", count)
	}

	content, err := mfs.ReadFile("/project/index.html")
	if err != nil {
		t.Fatalf("Failed to read file after DryRun: %v", err)
	}
	if strings.Contains(string(content), `"importmap"`) {
		t.Error("DryRun should not modify files")
	}
}

func TestInjectBatch_InvalidTemplate(t *testing.T) {
	mfs := setupInjectFixture(t)

	results := InjectBatch(mfs, []string{"/project/index.html"}, "/project", Options{
		Template: "/assets/{bogus_var}/{path}",
	})

	var count int
	for r := range results {
		count++
		if r.Error == "" {
			t.Error("Expected error for invalid template variable")
		}
	}
	if count != 1 {
		t.Fatalf("Expected exactly 1 result, got %d", count)
	}
}

func TestInjectBatch_MissingFile(t *testing.T) {
	mfs := setupInjectFixture(t)

	results := InjectBatch(mfs, []string{"/project/missing.html"}, "/project", Options{})

	var count int
	for r := range results {
		count++
		if r.Error == "" {
			t.Error("Expected error for missing file")
		}
		if r.File != "/project/missing.html" {
			t.Errorf("Expected file path in result, got %s", r.File)
		}
	}
	if count != 1 {
		t.Fatalf("Expected exactly 1 result, got %d", count)
	}
}

func TestInjectBatch_MultipleFiles(t *testing.T) {
	mfs := setupInjectFixture(t)

	results := InjectBatch(mfs, []string{
		"/project/index.html",
		"/project/other.html",
	}, "/project", Options{DryRun: true})

	var count int
	for r := range results {
		count++
		if r.Error != "" {
			t.Errorf("Unexpected error for %s: %s", r.File, r.Error)
		}
	}
	if count != 2 {
		t.Errorf("Expected 2 results, got %d", count)
	}
}

func setupInjectFixture(t *testing.T) *mapfs.MapFileSystem {
	t.Helper()
	return testutil.NewFixtureFS(t, "inject/no-importmap", "/project")
}

func TestInjectBatchLeavesUnchangedWhenEmpty(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "inject/no-imports", "/test")
	before, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	var results []Result
	for result := range InjectBatch(mfs, []string{"/test/index.html"}, "/test", Options{}) {
		results = append(results, result)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != "" {
		t.Fatalf("unexpected error: %s", results[0].Error)
	}
	if results[0].Modified {
		t.Error("expected file to be left unchanged when there is nothing to inject")
	}
	after, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture after inject: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("file changed unexpectedly:\n%s", after)
	}
}

func TestInjectSelfImportUsesSourcePath(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/self-import", "/test")
	var results []Result
	for result := range InjectBatch(mfs, []string{"/test/index.html"}, "/test", Options{}) {
		results = append(results, result)
	}
	if len(results) != 1 || results[0].Error != "" {
		t.Fatalf("unexpected results: %+v", results)
	}
	content, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read index.html: %v", err)
	}
	html := string(content)
	if !strings.Contains(html, `"my-lib": "/index.js"`) {
		t.Errorf("self import should point at source, got:\n%s", html)
	}
	if strings.Contains(html, "/node_modules/my-lib") {
		t.Errorf("self import must not point at node_modules, got:\n%s", html)
	}
}

func TestInjectFiltersUnrelatedScopes(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/scope-closure", "/test")
	for result := range InjectBatch(mfs, []string{"/test/index.html"}, "/test", Options{}) {
		if result.Error != "" {
			t.Fatalf("unexpected error: %s", result.Error)
		}
	}
	content, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read index.html: %v", err)
	}
	html := string(content)
	if strings.Contains(html, "unrelated") {
		t.Errorf("unrelated scope leaked into injected map:\n%s", html)
	}
	if !strings.Contains(html, "@lit/reactive-element") {
		t.Errorf("injected map lost transitive scope:\n%s", html)
	}
}

// TestInjectFailsOnUnreadableModule ensures a fatal graph error becomes a
// failed Result instead of a partial map with a success claim.
func TestInjectFailsOnUnreadableModule(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/missing-module", "/test")
	before, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	var results []Result
	for result := range InjectBatch(mfs, []string{"/test/index.html"}, "/test", Options{}) {
		results = append(results, result)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error == "" {
		t.Fatal("expected an error for an unreadable module")
	}
	if results[0].Modified {
		t.Error("a failed trace must not report the file as modified")
	}

	after, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read fixture after inject: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("file changed despite a failed trace:\n%s", after)
	}
	if strings.Contains(string(after), "importmap") {
		t.Error("must not inject a partial import map")
	}
}

// TestInjectHonorsConditions checks parity with the trace command for the
// same fixture and options.
func TestInjectHonorsConditions(t *testing.T) {
	traceFS := testutil.NewFixtureFS(t, "trace/conditions", "/test")
	injectFS := testutil.NewFixtureFS(t, "trace/conditions", "/test")
	opts := Options{Conditions: []string{"browser"}}

	traced, err := trace.TraceSingle(traceFS, "/test/index.html", "/test", trace.Options{Conditions: opts.Conditions})
	if err != nil {
		t.Fatalf("TraceSingle failed: %v", err)
	}

	for result := range InjectBatch(injectFS, []string{"/test/index.html"}, "/test", opts) {
		if result.Error != "" {
			t.Fatalf("unexpected inject error: %s", result.Error)
		}
		if !result.Modified {
			t.Fatal("expected inject to write an import map")
		}
	}

	content, err := injectFS.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read injected fixture: %v", err)
	}
	loc := trace.FindImportMapTag(content)
	if !loc.Found {
		t.Fatal("expected an injected import map")
	}
	injected, err := importmap.Parse(content[loc.ContentStart:loc.ContentEnd])
	if err != nil {
		t.Fatalf("failed to parse injected import map: %v", err)
	}

	if got := injected.Imports["cond-pkg"]; got != "/node_modules/cond-pkg/browser.js" {
		t.Errorf("inject condition resolution = %q, want browser.js (%v)", got, injected.Imports)
	}
	if got, want := injected.Imports["cond-pkg"], traced.ImportMap.Imports["cond-pkg"]; got != want {
		t.Errorf("inject/trace condition mismatch: inject %q, trace %q", got, want)
	}
}

// TestInjectBatchMemoizedResultsAreSafe runs two pages with identical
// specifier sets through the batch memo and checks both receive the same map.
func TestInjectBatchMemoizedResultsAreSafe(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/scope-closure", "/test")
	var results []Result
	for result := range InjectBatch(mfs, []string{"/test/index.html", "/test/other.html"}, "/test", Options{Parallel: 1}) {
		results = append(results, result)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, result := range results {
		if result.Error != "" {
			t.Fatalf("unexpected error for %s: %s", result.File, result.Error)
		}
		if !result.Modified {
			t.Errorf("expected %s to be modified", result.File)
		}
	}

	index, err := mfs.ReadFile("/test/index.html")
	if err != nil {
		t.Fatalf("Failed to read index.html: %v", err)
	}
	other, err := mfs.ReadFile("/test/other.html")
	if err != nil {
		t.Fatalf("Failed to read other.html: %v", err)
	}
	if string(index) != string(other) {
		t.Errorf("memoized maps diverged between identical pages:\nindex:\n%s\nother:\n%s", index, other)
	}
}
