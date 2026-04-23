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
	if !strings.Contains(string(result), `<script type="importmap">`) {
		t.Error("Result should contain importmap script tag")
	}
	if !strings.Contains(string(result), `"lit"`) {
		t.Error("Result should contain lit import")
	}
	if !strings.Contains(string(result), `<script type="module"`) {
		t.Error("Result should preserve existing module script")
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
	if strings.Contains(string(result), `"old"`) {
		t.Error("Result should not contain old import")
	}
	if !strings.Contains(string(result), `"new-pkg"`) {
		t.Error("Result should contain new import")
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
	_, _, err = buildNewContent(html, loc, im)
	if err == nil {
		t.Error("Expected error when no <head> tag exists")
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
	if count == 0 {
		t.Fatal("Expected at least one result from InjectBatch")
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
	if count == 0 {
		t.Fatal("Expected at least one result from InjectBatch")
	}
}

func TestInjectBatch_MultipleFiles(t *testing.T) {
	mfs := setupInjectFixture(t)
	mfs.AddFile("/project/other.html", `<!DOCTYPE html>
<html>
<head>
  <title>Other</title>
  <script type="module">
    import { LitElement } from 'lit';
  </script>
</head>
<body></body>
</html>`, 0644)

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
