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
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func TestMain(m *testing.M) {
	// Build the binary before running tests
	wd := mustGetwd()
	cmd := exec.Command("go", "build", "-o", "mappa_test", ".")
	cmd.Dir = wd
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("failed to build test binary: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	_ = os.Remove(filepath.Join(wd, "mappa_test"))
	os.Exit(code)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

// compareOrUpdateGolden compares output against a golden file, or updates it if --update is set.
func compareOrUpdateGolden(t *testing.T, goldenPath string, actual string) {
	t.Helper()

	if *update {
		if err := os.WriteFile(goldenPath, []byte(actual), 0644); err != nil {
			t.Fatalf("Failed to update golden file: %v", err)
		}
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("Failed to read golden file %s: %v", goldenPath, err)
	}

	// Normalize line endings and trailing whitespace
	actualNorm := strings.TrimSpace(actual)
	expectedNorm := strings.TrimSpace(string(expected))

	if actualNorm != expectedNorm {
		t.Errorf("Output does not match golden file %s\nExpected:\n%s\n\nActual:\n%s", goldenPath, expectedNorm, actualNorm)
	}
}

func runCLI(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	binary := filepath.Join(mustGetwd(), "mappa_test")
	cmd := exec.Command(binary, args...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("Failed to run CLI: %v", err)
		}
	}

	return stdout, stderr, exitCode
}

func TestGenerateLocal(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "resolve", "simple-pkg")

	stdout, stderr, code := runCLI(t, "generate", "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nstdout: %s", err, stdout)
	}

	val, exists := result["imports"]
	if !exists || val == nil {
		t.Fatalf("Expected imports object, got nil")
	}
	imports, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("Expected imports object, got %T", val)
	}

	if imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import, got %v", imports["lit"])
	}
}

func TestGenerateHTMLFormat(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "resolve", "simple-pkg")

	stdout, stderr, code := runCLI(t, "generate", "--package", fixtureDir, "--format", "html")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.HasPrefix(stdout, "<script type=\"importmap\">") {
		t.Errorf("Expected HTML script tag prefix, got: %s", stdout[:min(50, len(stdout))])
	}

	if !strings.Contains(stdout, "</script>") {
		t.Error("Expected closing script tag")
	}

	// Extract JSON from script tag and verify it's valid
	jsonStart := strings.Index(stdout, "\n") + 1
	jsonEnd := strings.LastIndex(stdout, "\n</script>")
	if jsonStart > 0 && jsonEnd > jsonStart {
		jsonStr := stdout[jsonStart:jsonEnd]
		var result map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			t.Fatalf("Failed to parse embedded JSON: %v", err)
		}
	}
}

func TestGenerateOutputFile(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "resolve", "simple-pkg")
	tmpFile := filepath.Join(t.TempDir(), "importmap.json")

	stdout, stderr, code := runCLI(t, "generate", "--package", fixtureDir, "--output", tmpFile)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if stdout != "" {
		t.Errorf("Expected no stdout when writing to file, got: %s", stdout)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("Failed to parse output file JSON: %v", err)
	}

	val, exists := result["imports"]
	if !exists || val == nil {
		t.Fatalf("Expected imports object, got nil")
	}
	imports, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("Expected imports object, got %T", val)
	}
	if imports["lit"] == nil {
		t.Error("Expected lit import in output file")
	}
}

func TestTrace(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "with-deps")
	htmlFile := filepath.Join(fixtureDir, "index.html")
	goldenFile := filepath.Join(fixtureDir, "expected.json")

	// Default format (json) outputs import map
	stdout, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	compareOrUpdateGolden(t, goldenFile, stdout)
}

func TestTraceSpecifiersFormat(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "with-deps")
	htmlFile := filepath.Join(fixtureDir, "index.html")
	goldenFile := filepath.Join(fixtureDir, "expected-specifiers.json")

	stdout, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir, "--format", "specifiers")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	compareOrUpdateGolden(t, goldenFile, stdout)
}

func TestTraceHTMLFormat(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "with-deps")
	htmlFile := filepath.Join(fixtureDir, "index.html")

	stdout, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir, "--format", "html")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.HasPrefix(stdout, "<script type=\"importmap\">") {
		t.Errorf("Expected HTML script tag prefix, got: %s", stdout[:min(50, len(stdout))])
	}

	if !strings.Contains(stdout, "</script>") {
		t.Error("Expected closing script tag")
	}

	// Check that lit is in the import map
	if !strings.Contains(stdout, "\"lit\"") {
		t.Error("Expected 'lit' in import map")
	}
}

func TestTraceWithTemplate(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "with-deps")
	htmlFile := filepath.Join(fixtureDir, "index.html")
	goldenFile := filepath.Join(fixtureDir, "expected-template.json")

	stdout, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir, "--template", "/assets/{package}/{path}")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	compareOrUpdateGolden(t, goldenFile, stdout)
}

func TestTraceMissingFile(t *testing.T) {
	_, stderr, code := runCLI(t, "trace", "nonexistent.html")
	if code == 0 {
		t.Error("Expected non-zero exit code for missing file")
	}

	if !strings.Contains(stderr, "Error") {
		t.Errorf("Expected error message, got: %s", stderr)
	}
}

func TestTraceMissingArg(t *testing.T) {
	_, stderr, code := runCLI(t, "trace")
	if code == 0 {
		t.Error("Expected non-zero exit code for missing argument")
	}

	if !strings.Contains(stderr, "no files to trace") {
		t.Errorf("Expected 'no files to trace' error, got: %s", stderr)
	}
}

func TestTraceInvalidFormat(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "with-deps")
	htmlFile := filepath.Join(fixtureDir, "index.html")

	_, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir, "--format", "invalid")
	if code == 0 {
		t.Error("Expected non-zero exit code for invalid format")
	}

	if !strings.Contains(stderr, "invalid format") {
		t.Errorf("Expected 'invalid format' error, got: %s", stderr)
	}

	if !strings.Contains(stderr, "json, html, specifiers") {
		t.Errorf("Expected allowed formats in error, got: %s", stderr)
	}
}

func TestTraceBatchMultipleArgs(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "batch")
	file1 := filepath.Join(fixtureDir, "page1.html")
	file2 := filepath.Join(fixtureDir, "page2.html")

	stdout, stderr, code := runCLI(t, "trace", file1, file2, "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should output NDJSON (one JSON object per line)
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 NDJSON lines, got %d: %s", len(lines), stdout)
	}

	// Parse each line as JSON
	for i, line := range lines {
		var result map[string]any
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("Failed to parse NDJSON line %d: %v\nline: %s", i, err, line)
		}

		// Each result should have "file" and "imports" keys
		if result["file"] == nil {
			t.Errorf("Line %d: expected 'file' key", i)
		}
		if result["imports"] == nil {
			t.Errorf("Line %d: expected 'imports' key", i)
		}
	}
}

func TestTraceBatchGlob(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "batch")
	globPattern := filepath.Join(fixtureDir, "**", "*.html")

	stdout, stderr, code := runCLI(t, "trace", "--glob", globPattern, "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should find all 3 HTML files
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("Expected 3 NDJSON lines (page1.html, page2.html, subdir/page3.html), got %d: %s", len(lines), stdout)
	}

	// Parse each line and track found files
	foundFiles := make(map[string]bool)
	for i, line := range lines {
		var result map[string]any
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("Failed to parse NDJSON line %d: %v", i, err)
		}

		// Track found files
		if file, ok := result["file"].(string); ok {
			foundFiles[filepath.Base(file)] = true
		}

		// Verify imports contain "lit"
		imports, ok := result["imports"].(map[string]any)
		if !ok {
			t.Errorf("Line %d: expected imports object", i)
			continue
		}
		if imports["lit"] == nil {
			t.Errorf("Line %d: expected 'lit' in imports", i)
		}
	}

	// Verify all expected files were processed
	expectedFiles := []string{"page1.html", "page2.html", "page3.html"}
	for _, f := range expectedFiles {
		if !foundFiles[f] {
			t.Errorf("Expected file %q not found in output", f)
		}
	}
}

func TestTraceBatchJobs(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "batch")
	file1 := filepath.Join(fixtureDir, "page1.html")
	file2 := filepath.Join(fixtureDir, "page2.html")

	// Test with explicit jobs flag
	stdout, stderr, code := runCLI(t, "trace", file1, file2, "--package", fixtureDir, "-j", "2")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 NDJSON lines, got %d", len(lines))
	}
}

func TestTraceBatchHTMLFormatError(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "batch")
	file1 := filepath.Join(fixtureDir, "page1.html")
	file2 := filepath.Join(fixtureDir, "page2.html")

	// HTML format should fail for batch mode
	_, stderr, code := runCLI(t, "trace", file1, file2, "--package", fixtureDir, "--format", "html")
	if code == 0 {
		t.Error("Expected non-zero exit code for html format in batch mode")
	}

	if !strings.Contains(stderr, "not supported for batch mode") {
		t.Errorf("Expected 'not supported for batch mode' error, got: %s", stderr)
	}
}

// TestTraceDeepImportNotInExports verifies that traced bare specifiers that
// are deep imports (not listed in package exports) are still resolved.
// This is a regression test for https://github.com/bennypowers/mappa/issues/15
func TestTraceDeepImportNotInExports(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "unresolvable")
	file := filepath.Join(fixtureDir, "page.html")

	stdout, stderr, code := runCLI(t, "trace", file, "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Parse output
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nstdout: %s", err, stdout)
	}

	// The traced specifier @example/core/button/button.js should be in imports
	// even though it's not in the package's exports field
	imports, ok := result["imports"].(map[string]any)
	if !ok {
		t.Fatalf("Expected 'imports' key in output, got keys: %v", result)
	}

	if imports["@example/core/button/button.js"] == nil {
		t.Errorf("Expected '@example/core/button/button.js' in imports, got: %v", imports)
	}
}

// TestTraceBatchDeepImportNotInExports verifies that batch mode correctly resolves
// traced bare specifiers that are deep imports (not listed in package exports).
// This is a regression test for https://github.com/bennypowers/mappa/issues/20
func TestTraceBatchDeepImportNotInExports(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "unresolvable")
	file1 := filepath.Join(fixtureDir, "page.html")
	file2 := filepath.Join(fixtureDir, "page2.html")

	stdout, stderr, code := runCLI(t, "trace", file1, file2, "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Parse NDJSON lines
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("Expected 2 NDJSON lines, got %d: %s", len(lines), stdout)
	}

	for i, line := range lines {
		var result map[string]any
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("Failed to parse NDJSON line %d: %v", i, err)
		}

		imports, ok := result["imports"].(map[string]any)
		if !ok {
			t.Errorf("Line %d: expected 'imports' object", i)
			continue
		}

		// Both pages import @example/core/button/button.js which is not in exports
		if imports["@example/core/button/button.js"] == nil {
			t.Errorf("Line %d: expected '@example/core/button/button.js' in imports, got: %v", i, imports)
		}
	}
}

// TestTraceTransitiveDependencies verifies that trace follows bare specifier
// imports into node_modules and traces their transitive dependencies.
// This is a regression test for https://github.com/bennypowers/mappa/issues/22
func TestTraceTransitiveDependencies(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "transitive")
	file := filepath.Join(fixtureDir, "index.html")

	stdout, stderr, code := runCLI(t, "trace", file, "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Parse output
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nstdout: %s", err, stdout)
	}

	imports, ok := result["imports"].(map[string]any)
	if !ok {
		t.Fatalf("Expected 'imports' key in output, got keys: %v", result)
	}

	// HTML imports @example/button/button.js which internally imports:
	// - lit (from 'lit')
	// - lit/decorators.js (from 'lit/decorators.js')
	// All three should be in the imports
	expectedImports := []string{
		"@example/button/button.js",
		"lit",
		"lit/decorators.js",
	}

	for _, spec := range expectedImports {
		if imports[spec] == nil {
			t.Errorf("Expected %q in imports, got: %v", spec, imports)
		}
	}
}

// TestTraceTransitiveDependenciesSpecifiers verifies that the specifiers format
// includes all transitive bare specifiers.
func TestTraceTransitiveDependenciesSpecifiers(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "transitive")
	file := filepath.Join(fixtureDir, "index.html")

	stdout, stderr, code := runCLI(t, "trace", file, "--package", fixtureDir, "-f", "specifiers")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Parse output
	var result struct {
		BareSpecifiers []string `json:"bare_specifiers"`
		Packages       []string `json:"packages"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nstdout: %s", err, stdout)
	}

	// Should have transitive specifiers
	expectedSpecifiers := []string{
		"@example/button/button.js",
		"lit",
		"lit/decorators.js",
	}

	specSet := make(map[string]bool)
	for _, s := range result.BareSpecifiers {
		specSet[s] = true
	}

	for _, spec := range expectedSpecifiers {
		if !specSet[spec] {
			t.Errorf("Expected %q in bare_specifiers, got: %v", spec, result.BareSpecifiers)
		}
	}

	// Should have both packages
	pkgSet := make(map[string]bool)
	for _, p := range result.Packages {
		pkgSet[p] = true
	}

	if !pkgSet["@example/button"] {
		t.Errorf("Expected '@example/button' in packages, got: %v", result.Packages)
	}
	if !pkgSet["lit"] {
		t.Errorf("Expected 'lit' in packages, got: %v", result.Packages)
	}
}

func TestHelp(t *testing.T) {
	stdout, _, code := runCLI(t, "--help")
	if code != 0 {
		t.Fatalf("Expected exit code 0 for help, got %d", code)
	}

	expectedStrings := []string{
		"mappa",
		"generate",
		"trace",
		"--package",
		"--output",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(stdout, s) {
			t.Errorf("Expected %q in help output", s)
		}
	}
}

func TestGenerateHelp(t *testing.T) {
	stdout, _, code := runCLI(t, "generate", "--help")
	if code != 0 {
		t.Fatalf("Expected exit code 0 for help, got %d", code)
	}

	expectedStrings := []string{
		"--template",
		"--format",
		"--include-package",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(stdout, s) {
			t.Errorf("Expected %q in generate help output", s)
		}
	}
}

func TestTraceHelp(t *testing.T) {
	stdout, _, code := runCLI(t, "trace", "--help")
	if code != 0 {
		t.Fatalf("Expected exit code 0 for help, got %d", code)
	}

	expectedStrings := []string{
		"--template",
		"--format",
		"json, html, specifiers",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(stdout, s) {
			t.Errorf("Expected %q in trace help output", s)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	_, stderr, code := runCLI(t, "unknown")
	if code == 0 {
		t.Error("Expected non-zero exit code for unknown command")
	}

	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("Expected 'unknown command' error, got: %s", stderr)
	}
}

func TestGenerateInvalidTemplate(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "resolve", "simple-pkg")

	// Invalid template variable should fail
	_, stderr, code := runCLI(t, "generate", "--package", fixtureDir, "--template", "{invalid}")
	if code == 0 {
		t.Error("Expected non-zero exit code for invalid template")
	}

	if !strings.Contains(stderr, "unknown template variable") {
		t.Errorf("Expected 'unknown template variable' error, got: %s", stderr)
	}
}

func TestGenerateEmptyProject(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, stderr, code := runCLI(t, "generate", "--package", tmpDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should output empty import map
	if strings.TrimSpace(stdout) != "{}" {
		t.Errorf("Expected empty import map {}, got: %s", stdout)
	}
}

func TestShortFlags(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "resolve", "simple-pkg")

	stdout, stderr, code := runCLI(t, "generate", "-p", fixtureDir, "-f", "json")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if result["imports"] == nil {
		t.Error("Expected imports in output")
	}
}

func TestInjectHelp(t *testing.T) {
	stdout, _, code := runCLI(t, "inject", "--help")
	if code != 0 {
		t.Fatalf("Expected exit code 0 for help, got %d", code)
	}

	expectedStrings := []string{
		"--glob",
		"--template",
		"--dry-run",
		"-j",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(stdout, s) {
			t.Errorf("Expected %q in inject help output", s)
		}
	}
}

func TestInjectMissingGlob(t *testing.T) {
	_, stderr, code := runCLI(t, "inject")
	if code == 0 {
		t.Error("Expected non-zero exit code for missing --glob")
	}

	if !strings.Contains(stderr, `"glob" not set`) {
		t.Errorf("Expected 'glob not set' error, got: %s", stderr)
	}
}

func TestInjectDryRun(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "inject", "with-existing")
	globPattern := filepath.Join(fixtureDir, "*.html")

	stdout, stderr, code := runCLI(t, "inject", "--glob", globPattern, "--package", fixtureDir, "--dry-run")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should show "would update" in dry-run mode
	if !strings.Contains(stdout, "would update") && !strings.Contains(stdout, "would be modified") {
		t.Errorf("Expected 'would update' or 'would be modified' in dry-run output, got: %s", stdout)
	}
}

func TestInjectUpdateExisting(t *testing.T) {
	// Copy fixture to temp dir to avoid modifying test fixtures
	fixtureDir := filepath.Join("testdata", "inject", "with-existing")
	tmpDir := t.TempDir()

	// Copy files
	copyFile(t, filepath.Join(fixtureDir, "index.html"), filepath.Join(tmpDir, "index.html"))
	copyFile(t, filepath.Join(fixtureDir, "package.json"), filepath.Join(tmpDir, "package.json"))
	copyDir(t, filepath.Join(fixtureDir, "node_modules"), filepath.Join(tmpDir, "node_modules"))

	globPattern := filepath.Join(tmpDir, "*.html")

	stdout, stderr, code := runCLI(t, "inject", "--glob", globPattern, "--package", tmpDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should show summary
	if !strings.Contains(stdout, "Injected") {
		t.Errorf("Expected 'Injected' in output, got: %s", stdout)
	}

	// Read the modified file
	content, err := os.ReadFile(filepath.Join(tmpDir, "index.html"))
	if err != nil {
		t.Fatalf("Failed to read modified file: %v", err)
	}

	// Should contain both manual-dep (preserved) and lit (traced)
	if !strings.Contains(string(content), "manual-dep") {
		t.Error("Expected 'manual-dep' to be preserved in import map")
	}
	if !strings.Contains(string(content), "\"lit\"") {
		t.Error("Expected 'lit' to be added to import map")
	}
}

func TestInjectInsertNew(t *testing.T) {
	// Copy fixture to temp dir
	fixtureDir := filepath.Join("testdata", "inject", "no-importmap")
	tmpDir := t.TempDir()

	copyFile(t, filepath.Join(fixtureDir, "index.html"), filepath.Join(tmpDir, "index.html"))
	copyFile(t, filepath.Join(fixtureDir, "package.json"), filepath.Join(tmpDir, "package.json"))
	copyDir(t, filepath.Join(fixtureDir, "node_modules"), filepath.Join(tmpDir, "node_modules"))

	globPattern := filepath.Join(tmpDir, "*.html")

	stdout, stderr, code := runCLI(t, "inject", "--glob", globPattern, "--package", tmpDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should show "new" in stats
	if !strings.Contains(stdout, "new") {
		t.Errorf("Expected 'new' in output for inserted import map, got: %s", stdout)
	}

	// Read the modified file
	content, err := os.ReadFile(filepath.Join(tmpDir, "index.html"))
	if err != nil {
		t.Fatalf("Failed to read modified file: %v", err)
	}

	// Should contain an import map with lit
	if !strings.Contains(string(content), "importmap") {
		t.Error("Expected import map script tag to be inserted")
	}
	if !strings.Contains(string(content), "\"lit\"") {
		t.Error("Expected 'lit' in import map")
	}
}

func TestInjectJSONFormat(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "inject", "with-existing")
	globPattern := filepath.Join(fixtureDir, "*.html")

	stdout, stderr, code := runCLI(t, "inject", "--glob", globPattern, "--package", fixtureDir, "--dry-run", "--format", "json")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should output JSON lines
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, line := range lines {
		var result map[string]any
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			// The last line might be stats, try parsing that
			var stats map[string]int
			if err := json.Unmarshal([]byte(line), &stats); err != nil {
				t.Fatalf("Failed to parse JSON line: %v\nline: %s", err, line)
			}
		}
	}
}

// Helper functions for copying files/dirs
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("Failed to write %s: %v", dst, err)
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("Failed to read dir %s: %v", src, err)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("Failed to create dir %s: %v", dst, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			copyDir(t, srcPath, dstPath)
		} else {
			copyFile(t, srcPath, dstPath)
		}
	}
}
