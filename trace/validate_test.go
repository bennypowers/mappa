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
	"fmt"
	"sort"
	"testing"

	"bennypowers.dev/mappa/testutil"
)

func TestValidateImports(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/validate-imports", "/test")

	// Parse the module to build a graph
	tracer := NewTracer(mfs, "/test")
	graph, err := tracer.TraceModule("/test/module.js")
	if err != nil {
		t.Fatalf("TraceModule failed: %v", err)
	}

	// Define dependencies (from package.json)
	deps := map[string]string{
		"lit": "^3.0.0",
	}
	devDeps := map[string]string{
		"typescript": "^5.0.0",
	}

	// Validate imports (empty root package name - not testing self-referencing)
	issues := graph.ValidateImports(mfs, "/test", "", deps, devDeps)

	// Load expected output
	expectedBytes, err := mfs.ReadFile("/test/expected.json")
	if err != nil {
		t.Fatalf("Failed to read expected.json: %v", err)
	}

	var expected struct {
		Issues []struct {
			File      string `json:"file"`
			Line      int    `json:"line"`
			Specifier string `json:"specifier"`
			Package   string `json:"package"`
			IssueType string `json:"issue_type"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatalf("Failed to parse expected.json: %v", err)
	}

	if len(issues) != len(expected.Issues) {
		t.Fatalf("Expected %d issues, got %d: %+v", len(expected.Issues), len(issues), issues)
	}

	// Sort both slices for deterministic comparison (map iteration order is random)
	sort.Slice(issues, func(i, j int) bool {
		keyI := fmt.Sprintf("%s:%d:%s:%s:%s", issues[i].File, issues[i].Line, issues[i].Specifier, issues[i].Package, issues[i].IssueType.String())
		keyJ := fmt.Sprintf("%s:%d:%s:%s:%s", issues[j].File, issues[j].Line, issues[j].Specifier, issues[j].Package, issues[j].IssueType.String())
		return keyI < keyJ
	})
	sort.Slice(expected.Issues, func(i, j int) bool {
		keyI := fmt.Sprintf("%s:%d:%s:%s:%s", expected.Issues[i].File, expected.Issues[i].Line, expected.Issues[i].Specifier, expected.Issues[i].Package, expected.Issues[i].IssueType)
		keyJ := fmt.Sprintf("%s:%d:%s:%s:%s", expected.Issues[j].File, expected.Issues[j].Line, expected.Issues[j].Specifier, expected.Issues[j].Package, expected.Issues[j].IssueType)
		return keyI < keyJ
	})

	for i, exp := range expected.Issues {
		if issues[i].File != exp.File {
			t.Errorf("Issue %d: expected file %q, got %q", i, exp.File, issues[i].File)
		}
		if issues[i].Line != exp.Line {
			t.Errorf("Issue %d: expected line %d, got %d", i, exp.Line, issues[i].Line)
		}
		if issues[i].Specifier != exp.Specifier {
			t.Errorf("Issue %d: expected specifier %q, got %q", i, exp.Specifier, issues[i].Specifier)
		}
		if issues[i].Package != exp.Package {
			t.Errorf("Issue %d: expected package %q, got %q", i, exp.Package, issues[i].Package)
		}
		if issues[i].IssueType.String() != exp.IssueType {
			t.Errorf("Issue %d: expected issue type %q, got %q", i, exp.IssueType, issues[i].IssueType.String())
		}
	}
}

func TestValidateImports_NoIssues(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/validate-imports", "/test")

	// Create a graph with only direct dependency imports
	graph := &ModuleGraph{
		Modules: map[string]*Module{
			"/test/app.js": {
				Path: "/test/app.js",
				Imports: []ModuleImport{
					{Specifier: "lit", Line: 1},
					{Specifier: "lit/html.js", Line: 2},
					{Specifier: "./local.js", Line: 3}, // relative imports are ignored
				},
				Traced: true,
			},
		},
		bareSpecifiers: map[string]bool{
			"lit":         true,
			"lit/html.js": true,
		},
	}

	deps := map[string]string{"lit": "^3.0.0"}
	devDeps := map[string]string{}

	issues := graph.ValidateImports(mfs, "/test", "", deps, devDeps)

	if len(issues) != 0 {
		t.Errorf("Expected no issues, got %d: %+v", len(issues), issues)
	}
}

func TestValidateImports_SelfReference(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/validate-imports", "/test")

	// Create a graph with self-referencing imports
	graph := &ModuleGraph{
		Modules: map[string]*Module{
			"/test/app.js": {
				Path: "/test/app.js",
				Imports: []ModuleImport{
					{Specifier: "lit", Line: 1},
					{Specifier: "@rhds/elements", Line: 2},           // self-reference
					{Specifier: "@rhds/elements/lib/foo.js", Line: 3}, // self-reference subpath
				},
				Traced: true,
			},
		},
		bareSpecifiers: map[string]bool{
			"lit":                       true,
			"@rhds/elements":            true,
			"@rhds/elements/lib/foo.js": true,
		},
	}

	deps := map[string]string{"lit": "^3.0.0"}
	devDeps := map[string]string{}

	// Pass root package name to skip self-referencing imports
	issues := graph.ValidateImports(mfs, "/test", "@rhds/elements", deps, devDeps)

	// Should have no issues - lit is a dependency, and @rhds/elements is the root package
	if len(issues) != 0 {
		t.Errorf("Expected no issues for self-referencing imports, got %d: %+v", len(issues), issues)
	}
}

func TestIssueType_String(t *testing.T) {
	tests := []struct {
		issueType IssueType
		expected  string
	}{
		{TransitiveDep, "transitive dependency"},
		{DevDep, "devDependency"},
		{NotInstalled, "not installed"},
	}

	for _, tt := range tests {
		if tt.issueType.String() != tt.expected {
			t.Errorf("IssueType(%d).String() = %q, expected %q", tt.issueType, tt.issueType.String(), tt.expected)
		}
	}
}

func TestValidateImports_SkipsNodeModules(t *testing.T) {
	// This test verifies that imports from files inside node_modules are NOT
	// validated. The scenario: root package has @example/button as a dependency
	// and lit as a devDependency. @example/button imports lit. This should NOT
	// produce a warning because we skip validation for node_modules files.
	mfs := testutil.NewFixtureFS(t, "trace/transitive-no-warning", "/test")

	// Build tracer with node_modules path to follow transitive dependencies
	tracer := NewTracer(mfs, "/test").WithNodeModules("/test/node_modules")
	graph, err := tracer.TraceHTML("/test/index.html")
	if err != nil {
		t.Fatalf("TraceHTML failed: %v", err)
	}

	// Verify we traced into node_modules (the button imports lit)
	if len(graph.Modules) < 2 {
		t.Fatalf("Expected at least 2 modules (button and lit), got %d", len(graph.Modules))
	}

	// Validate with lit as a devDependency only
	deps := map[string]string{
		"@example/button": "^1.0.0",
	}
	devDeps := map[string]string{
		"lit": "^3.0.0",
	}

	issues := graph.ValidateImports(mfs, "/test", "transitive-no-warning-test", deps, devDeps)

	// Should have no issues - imports from node_modules should be skipped
	if len(issues) != 0 {
		t.Errorf("Expected no issues for imports from node_modules, got %d:", len(issues))
		for _, issue := range issues {
			t.Errorf("  %s:%d - %q (%s)", issue.File, issue.Line, issue.Specifier, issue.IssueType)
		}
	}
}
