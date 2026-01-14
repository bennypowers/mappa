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

	// Validate imports
	issues := graph.ValidateImports(mfs, "/test", deps, devDeps)

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

	issues := graph.ValidateImports(mfs, "/test", deps, devDeps)

	if len(issues) != 0 {
		t.Errorf("Expected no issues, got %d: %+v", len(issues), issues)
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
