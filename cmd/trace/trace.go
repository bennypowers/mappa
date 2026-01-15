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

// Package trace provides the trace command for mappa.
package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/trace"
)

// Cmd is the trace cobra command that analyzes HTML files to find ES module
// imports and generates minimal import maps containing only the specifiers used.
var Cmd = &cobra.Command{
	Use:   "trace [file.html...]",
	Short: "Trace HTML files and generate minimal import maps",
	Long: `Trace HTML files to find all ES module imports and generate import maps.

For a single file, outputs an import map containing only the specifiers actually used.
For multiple files (via arguments or --glob), outputs NDJSON with one import map per line.
Use --format specifiers for debugging to see the raw trace output.`,
	Example: `  # Trace a single HTML file
  mappa trace index.html

  # Trace multiple files (NDJSON output)
  mappa trace file1.html file2.html file3.html

  # Trace files matching a glob pattern
  mappa trace --glob "_site/**/*.html"

  # Parallel processing with custom worker count
  mappa trace --glob "_site/**/*.html" -j 8

  # Custom URL template for resolved paths
  mappa trace index.html --template "/assets/{package}/{path}"

  # Output as HTML script tag (single file only)
  mappa trace index.html --format html`,
	RunE: run,
}

func init() {
	Cmd.Flags().StringP("format", "f", "json", "Output format (json, html, specifiers)")
	Cmd.Flags().String("template", "", "URL template (default: /node_modules/{package}/{path})")
	Cmd.Flags().StringSlice("conditions", nil, "Export condition priority (e.g., production,browser,import,default)")
	Cmd.Flags().String("glob", "", "Glob pattern to match HTML files (e.g., \"_site/**/*.html\")")
	Cmd.Flags().IntP("jobs", "j", 0, "Number of parallel workers (default: number of CPUs)")
}

func run(cmd *cobra.Command, args []string) error {
	osfs := fs.NewOSFileSystem()

	absRoot, err := filepath.Abs(viper.GetString("package"))
	if err != nil {
		return fmt.Errorf("invalid package directory: %w", err)
	}

	// Collect files from args and glob pattern, deduplicating by absolute path
	seen := make(map[string]struct{})
	var files []string

	for _, arg := range args {
		absPath, err := filepath.Abs(arg)
		if err != nil {
			return fmt.Errorf("invalid file path %q: %w", arg, err)
		}
		if _, exists := seen[absPath]; !exists {
			seen[absPath] = struct{}{}
			files = append(files, absPath)
		}
	}

	// Add files from glob pattern
	globPattern, _ := cmd.Flags().GetString("glob")
	if globPattern != "" {
		matches, err := doublestar.FilepathGlob(globPattern)
		if err != nil {
			return fmt.Errorf("invalid glob pattern: %w", err)
		}
		for _, match := range matches {
			absPath, err := filepath.Abs(match)
			if err != nil {
				return fmt.Errorf("invalid file path %q: %w", match, err)
			}
			if _, exists := seen[absPath]; !exists {
				seen[absPath] = struct{}{}
				files = append(files, absPath)
			}
		}
	}

	if len(files) == 0 {
		return fmt.Errorf("no files to trace: provide file arguments or use --glob")
	}

	format, _ := cmd.Flags().GetString("format")

	// Validate format flag
	switch format {
	case "json", "html", "specifiers":
		// valid
	default:
		return fmt.Errorf("invalid format %q: must be one of json, html, specifiers", format)
	}

	// Build trace options from flags
	templateArg, _ := cmd.Flags().GetString("template")
	conditions, _ := cmd.Flags().GetStringSlice("conditions")
	parallel, _ := cmd.Flags().GetInt("jobs")

	opts := trace.Options{
		Template:   templateArg,
		Conditions: conditions,
		Parallel:   parallel,
	}

	// Single file mode
	if len(files) == 1 {
		return runSingle(osfs, files[0], absRoot, format, opts)
	}

	// Batch mode
	return runBatch(osfs, files, absRoot, format, opts)
}

func runSingle(osfs fs.FileSystem, file, absRoot, format string, opts trace.Options) error {
	// Handle specifiers format separately
	if format == "specifiers" {
		result, issues, err := trace.TraceSpecifiers(osfs, file, absRoot)
		if err != nil {
			return fmt.Errorf("failed to trace: %w", err)
		}

		// Print warnings to stderr
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "Warning: %s:%d\n", issue.File, issue.Line)
			fmt.Fprintf(os.Stderr, "  Import %q references %s %q\n", issue.Specifier, issue.IssueType, issue.Package)
		}

		out, _ := json.MarshalIndent(result, "", "  ")
		if output := viper.GetString("output"); output != "" {
			return osfs.WriteFile(output, append(out, '\n'), 0644)
		}
		fmt.Println(string(out))
		return nil
	}

	result, err := trace.TraceSingle(osfs, file, absRoot, opts)
	if err != nil {
		return fmt.Errorf("failed to trace: %w", err)
	}

	// Print warnings to stderr
	for _, issue := range result.Issues {
		fmt.Fprintf(os.Stderr, "Warning: %s:%d\n", issue.File, issue.Line)
		fmt.Fprintf(os.Stderr, "  Import %q references %s %q\n", issue.Specifier, issue.IssueType, issue.Package)
	}

	return outputImportMap(osfs, result.ImportMap, format)
}

func runBatch(osfs fs.FileSystem, files []string, absRoot, format string, opts trace.Options) error {
	// html format doesn't make sense for batch mode
	if format == "html" {
		return fmt.Errorf("--format html is not supported for batch mode (multiple files)")
	}

	// Run batch trace
	results := trace.TraceBatch(osfs, files, absRoot, opts)

	// Collect results and output NDJSON
	encoder := json.NewEncoder(os.Stdout)
	var allWarnings []trace.Warning
	var errorCount int
	var totalCount int

	for result := range results {
		totalCount++
		if result.Error != "" {
			errorCount++
		}
		// Collect warnings for serial output
		allWarnings = append(allWarnings, result.Warnings...)
		// Clear warnings from JSON output (they go to stderr)
		result.Warnings = nil
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding result for %s: %v\n", result.File, err)
		}
	}

	// Output warnings serially to stderr
	for _, w := range allWarnings {
		fmt.Fprintf(os.Stderr, "Warning: %s:%d\n", w.File, w.Line)
		fmt.Fprintf(os.Stderr, "  Import %q references %s %q\n", w.Specifier, w.IssueType, w.Package)
	}

	if errorCount == totalCount {
		return fmt.Errorf("all %d files failed to trace", errorCount)
	}
	return nil
}

// outputImportMap formats and writes an import map to stdout or file.
func outputImportMap(osfs fs.FileSystem, im *importmap.ImportMap, format string) error {
	output := im.Format(format)

	if outputPath := viper.GetString("output"); outputPath != "" {
		return osfs.WriteFile(outputPath, []byte(output+"\n"), 0644)
	}
	fmt.Println(output)
	return nil
}
