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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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

	// Default format (json) outputs import map
	stdout, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir)
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nstdout: %s", err, stdout)
	}

	// Should have imports key (import map format)
	imports, ok := result["imports"].(map[string]any)
	if !ok || imports == nil {
		t.Fatal("Expected imports object in output")
	}

	// Check that lit is in the imports
	if imports["lit"] == nil {
		t.Error("Expected 'lit' in imports")
	}
}

func TestTraceSpecifiersFormat(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "trace", "extract-scripts")
	htmlFile := filepath.Join(fixtureDir, "index.html")

	stdout, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir, "--format", "specifiers")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	var result struct {
		Entrypoints    []string `json:"entrypoints"`
		Modules        []string `json:"modules"`
		BareSpecifiers []string `json:"bare_specifiers"`
		Packages       []string `json:"packages"`
	}

	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nstdout: %s", err, stdout)
	}

	if len(result.Entrypoints) == 0 {
		t.Error("Expected at least one entrypoint")
	}

	// Check that bare specifiers were found
	if !slices.Contains(result.BareSpecifiers, "lit") {
		t.Errorf("Expected 'lit' in bare specifiers, got %v", result.BareSpecifiers)
	}
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

	stdout, stderr, code := runCLI(t, "trace", htmlFile, "--package", fixtureDir, "--template", "/assets/{package}/{path}")
	if code != 0 {
		t.Fatalf("Expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nstdout: %s", err, stdout)
	}

	imports, ok := result["imports"].(map[string]any)
	if !ok {
		t.Fatal("Expected imports object")
	}

	// Check that lit import uses the custom template
	litPath, ok := imports["lit"].(string)
	if !ok {
		t.Fatal("Expected 'lit' import")
	}
	if !strings.HasPrefix(litPath, "/assets/lit/") {
		t.Errorf("Expected lit path to start with /assets/lit/, got %s", litPath)
	}
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

	if !strings.Contains(stderr, "accepts 1 arg") {
		t.Errorf("Expected argument error, got: %s", stderr)
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
