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
// Command mappa generates and works with ES module import maps.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/internal/version"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
	"bennypowers.dev/mappa/resolve/local"
	"bennypowers.dev/mappa/trace"
)

var rootCmd = &cobra.Command{
	Use:   "mappa",
	Short: "Generate and work with ES module import maps",
	Long:  `mappa generates ES module import maps from package.json dependencies.`,
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate import map from package.json",
	Long: `Generate an import map from package.json dependencies.

By default, generates local /node_modules paths. Use --template for custom paths.`,
	Example: `  # Generate import map with local paths (default)
  mappa generate

  # Custom local paths
  mappa generate --template "/assets/packages/{package}/{path}"

  # Include additional packages (e.g., devDependencies)
  mappa generate --include-package fuse.js

  # Merge with an existing import map (input map takes precedence)
  mappa generate --input-map manual-imports.json

  # Output as HTML script tag
  mappa generate --format html`,
	RunE: runGenerate,
}

var traceCmd = &cobra.Command{
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
	RunE: runTrace,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print version information for mappa.`,
	RunE:  runVersion,
}

func init() {
	// Root flags (persistent across all commands)
	rootCmd.PersistentFlags().StringP("package", "p", ".", "Package directory")
	rootCmd.PersistentFlags().StringP("output", "o", "", "Output file (default: stdout)")

	// Generate command flags
	generateCmd.Flags().StringP("format", "f", "json", "Output format (json, html)")
	generateCmd.Flags().String("input-map", "", "Import map file to merge with generated output")
	generateCmd.Flags().StringArray("include-package", nil, "Additional packages to include (can be repeated)")
	generateCmd.Flags().String("template", "", "URL template (default: /node_modules/{package}/{path})")
	generateCmd.Flags().StringSlice("conditions", nil, "Export condition priority (e.g., production,browser,import,default)")

	// Trace command flags
	traceCmd.Flags().StringP("format", "f", "json", "Output format (json, html, specifiers)")
	traceCmd.Flags().String("template", "", "URL template (default: /node_modules/{package}/{path})")
	traceCmd.Flags().StringSlice("conditions", nil, "Export condition priority (e.g., production,browser,import,default)")
	traceCmd.Flags().String("glob", "", "Glob pattern to match HTML files (e.g., \"_site/**/*.html\")")
	traceCmd.Flags().IntP("jobs", "j", 0, "Number of parallel workers (default: number of CPUs)")

	// Bind flags to viper
	_ = viper.BindPFlag("package", rootCmd.PersistentFlags().Lookup("package"))
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("format", generateCmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("input-map", generateCmd.Flags().Lookup("input-map"))
	_ = viper.BindPFlag("include-package", generateCmd.Flags().Lookup("include-package"))
	_ = viper.BindPFlag("template", generateCmd.Flags().Lookup("template"))
	_ = viper.BindPFlag("conditions", generateCmd.Flags().Lookup("conditions"))

	// Version command flags
	versionCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	// Add commands
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(traceCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runGenerate(cmd *cobra.Command, args []string) error {
	osfs := fs.NewOSFileSystem()
	absRoot, err := filepath.Abs(viper.GetString("package"))
	if err != nil {
		return fmt.Errorf("invalid package directory: %w", err)
	}

	// Get additional packages
	includePackages := viper.GetStringSlice("include-package")

	// Parse input map if provided
	var inputMap *importmap.ImportMap
	if inputMapPath := viper.GetString("input-map"); inputMapPath != "" {
		inputMapData, err := osfs.ReadFile(inputMapPath)
		if err != nil {
			return fmt.Errorf("failed to read input map: %w", err)
		}
		inputMap, err = importmap.Parse(inputMapData)
		if err != nil {
			return fmt.Errorf("failed to parse input map: %w", err)
		}
	}

	// Get URL template (default to local node_modules)
	templateArg := viper.GetString("template")
	if templateArg == "" {
		templateArg = resolve.DefaultLocalTemplate
	}

	// Build resolver
	resolver := local.New(osfs, nil)
	if len(includePackages) > 0 {
		resolver = resolver.WithPackages(includePackages)
	}
	resolver, err = resolver.WithTemplate(templateArg)
	if err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}
	if inputMap != nil {
		resolver = resolver.WithInputMap(inputMap)
	}
	if conditions := viper.GetStringSlice("conditions"); len(conditions) > 0 {
		resolver = resolver.WithConditions(conditions)
	}

	generatedMap, err := resolver.Resolve(absRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve: %w", err)
	}

	return outputImportMap(osfs, generatedMap.ToJSON(), viper.GetString("format"))
}

// outputImportMap pretty-prints an import map and writes it to stdout or file.
func outputImportMap(osfs fs.FileSystem, jsonOutput string, format string) error {
	if jsonOutput == "" {
		jsonOutput = "{}"
	}

	// Pretty print
	var pretty map[string]any
	if err := json.Unmarshal([]byte(jsonOutput), &pretty); err == nil {
		if prettyBytes, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			jsonOutput = string(prettyBytes)
		}
	}

	if format == "html" {
		jsonOutput = fmt.Sprintf(`<script type="importmap">
%s
</script>`, jsonOutput)
	}

	if output := viper.GetString("output"); output != "" {
		return osfs.WriteFile(output, []byte(jsonOutput+"\n"), 0644)
	}
	fmt.Println(jsonOutput)
	return nil
}

func runTrace(cmd *cobra.Command, args []string) error {
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

	// Single file mode: use original behavior for backward compatibility
	if len(files) == 1 {
		return runTraceSingle(cmd, osfs, files[0], absRoot, format)
	}

	// Batch mode: NDJSON output with parallel processing
	// html format doesn't make sense for batch mode
	if format == "html" {
		return fmt.Errorf("--format html is not supported for batch mode (multiple files)")
	}

	return runTraceBatch(cmd, osfs, files, absRoot, format)
}

// runTraceSingle traces a single file with the original output format.
func runTraceSingle(cmd *cobra.Command, osfs fs.FileSystem, htmlFile, absRoot, format string) error {
	tracer := trace.NewTracer(osfs, absRoot)
	graph, err := tracer.TraceHTML(htmlFile)
	if err != nil {
		return fmt.Errorf("failed to trace: %w", err)
	}

	// Parse package.json for dependency validation
	pkgPath := filepath.Join(absRoot, "package.json")
	pkg, err := packagejson.ParseFile(osfs, pkgPath)
	var issues []trace.ImportIssue
	if err != nil {
		// If no package.json, skip validation entirely
		issues = nil
	} else {
		// Validate imports against dependencies
		issues = graph.ValidateImports(osfs, absRoot, pkg.Dependencies, pkg.DevDependencies)
	}

	// Print warnings to stderr
	for _, issue := range issues {
		fmt.Fprintf(os.Stderr, "Warning: %s:%d\n", issue.File, issue.Line)
		fmt.Fprintf(os.Stderr, "  Import %q references %s %q\n", issue.Specifier, issue.IssueType, issue.Package)
	}

	// Handle specifiers format (legacy output for debugging)
	if format == "specifiers" {
		return runTraceSpecifiers(cmd, osfs, graph, issues)
	}

	// Get bare specifiers once for reuse
	bareSpecs := graph.BareSpecifiers()

	// Generate import map from traced specifiers
	templateArg, _ := cmd.Flags().GetString("template")
	if templateArg == "" {
		templateArg = resolve.DefaultLocalTemplate
	}
	conditions, _ := cmd.Flags().GetStringSlice("conditions")

	// Build resolver for the traced packages
	resolver := local.New(osfs, nil).WithPackages(bareSpecs)
	resolver, err = resolver.WithTemplate(templateArg)
	if err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}
	if len(conditions) > 0 {
		resolver = resolver.WithConditions(conditions)
	}

	// Find workspace root for node_modules resolution
	workspaceRoot := resolve.FindWorkspaceRoot(osfs, absRoot)

	// Generate full import map for scopes (transitive dependencies)
	generatedMap, err := resolver.Resolve(workspaceRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve: %w", err)
	}

	// Resolve traced specifiers directly using ResolveSpecifiers
	// This handles deep imports that may not be in package exports
	tracedImports := resolver.ResolveSpecifiers(workspaceRoot, bareSpecs)

	filteredMap := &importmap.ImportMap{
		Imports: tracedImports,
		Scopes:  generatedMap.Scopes,
	}

	// Clean up empty scopes
	if len(filteredMap.Scopes) == 0 {
		filteredMap.Scopes = nil
	}

	return outputImportMap(osfs, filteredMap.ToJSON(), format)
}

// traceWarning represents a single import validation warning.
type traceWarning struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Specifier string `json:"specifier"`
	IssueType string `json:"issue_type"`
	Package   string `json:"package"`
}

// traceResult holds the result of tracing a single file.
type traceResult struct {
	File     string            `json:"file"`
	Imports  map[string]string `json:"imports"`
	Error    string            `json:"error,omitempty"`
	Warnings []traceWarning    `json:"warnings,omitempty"`
}

// runTraceBatch traces multiple files in parallel with NDJSON output.
func runTraceBatch(cmd *cobra.Command, osfs fs.FileSystem, files []string, absRoot, format string) error {
	// Get parallelism setting
	parallel, _ := cmd.Flags().GetInt("jobs")
	if parallel <= 0 {
		parallel = runtime.NumCPU()
	}

	// Get template and conditions
	templateArg, _ := cmd.Flags().GetString("template")
	if templateArg == "" {
		templateArg = resolve.DefaultLocalTemplate
	}
	conditions, _ := cmd.Flags().GetStringSlice("conditions")

	// Find workspace root once for all files
	workspaceRoot := resolve.FindWorkspaceRoot(osfs, absRoot)

	// Create shared tracer
	tracer := trace.NewTracer(osfs, absRoot)

	// Parse package.json once for dependency validation
	pkgPath := filepath.Join(absRoot, "package.json")
	pkg, _ := packagejson.ParseFile(osfs, pkgPath)

	// Create channels for work distribution
	jobs := make(chan string, len(files))
	results := make(chan traceResult, len(files))

	// Start worker goroutines
	var wg sync.WaitGroup
	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for htmlFile := range jobs {
				result := traceFile(tracer, osfs, htmlFile, absRoot, workspaceRoot, templateArg, conditions, pkg, format)
				results <- result
			}
		}()
	}

	// Send jobs
	for _, file := range files {
		jobs <- file
	}
	close(jobs)

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results and output NDJSON, then print warnings serially
	encoder := json.NewEncoder(os.Stdout)
	var allWarnings []traceWarning
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

// traceFile traces a single file and returns the result.
func traceFile(tracer *trace.Tracer, osfs fs.FileSystem, htmlFile, absRoot, workspaceRoot, templateArg string, conditions []string, pkg *packagejson.PackageJSON, format string) traceResult {
	result := traceResult{File: htmlFile}

	graph, err := tracer.TraceHTML(htmlFile)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Validate imports if package.json was parsed
	if pkg != nil {
		issues := graph.ValidateImports(osfs, absRoot, pkg.Dependencies, pkg.DevDependencies)
		for _, issue := range issues {
			result.Warnings = append(result.Warnings, traceWarning{
				File:      issue.File,
				Line:      issue.Line,
				Specifier: issue.Specifier,
				IssueType: issue.IssueType.String(),
				Package:   issue.Package,
			})
		}
	}

	// Handle specifiers format
	if format == "specifiers" {
		// For specifiers format in batch mode, include specifier data instead of imports
		result.Imports = nil
		// We'll just return the bare specifiers as a simple map for consistency
		bareSpecs := graph.BareSpecifiers()
		result.Imports = make(map[string]string)
		for _, spec := range bareSpecs {
			result.Imports[spec] = spec // Map specifier to itself for visibility
		}
		return result
	}

	// Get bare specifiers once for reuse
	bareSpecs := graph.BareSpecifiers()
	if len(bareSpecs) == 0 {
		result.Imports = make(map[string]string)
		return result
	}

	// Build resolver for the traced packages
	resolver := local.New(osfs, nil).WithPackages(bareSpecs)
	resolver, err = resolver.WithTemplate(templateArg)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(conditions) > 0 {
		resolver = resolver.WithConditions(conditions)
	}

	// Resolve traced specifiers directly using ResolveSpecifiers
	// This handles deep imports that may not be in package exports
	result.Imports = resolver.ResolveSpecifiers(workspaceRoot, bareSpecs)

	return result
}

// runTraceSpecifiers outputs the legacy specifiers format for debugging.
func runTraceSpecifiers(_ *cobra.Command, osfs fs.FileSystem, graph *trace.ModuleGraph, issues []trace.ImportIssue) error {
	type IssueJSON struct {
		File      string `json:"file"`
		Line      int    `json:"line"`
		Specifier string `json:"specifier"`
		Package   string `json:"package"`
		IssueType string `json:"issue_type"`
	}

	result := struct {
		Entrypoints    []string    `json:"entrypoints"`
		Modules        []string    `json:"modules"`
		BareSpecifiers []string    `json:"bare_specifiers"`
		Packages       []string    `json:"packages"`
		Issues         []IssueJSON `json:"issues,omitempty"`
	}{
		Entrypoints:    graph.Entrypoints,
		BareSpecifiers: graph.BareSpecifiers(),
		Packages:       graph.PackageNames(),
	}

	for p := range graph.Modules {
		result.Modules = append(result.Modules, p)
	}
	sort.Strings(result.Modules)

	for _, issue := range issues {
		result.Issues = append(result.Issues, IssueJSON{
			File:      issue.File,
			Line:      issue.Line,
			Specifier: issue.Specifier,
			Package:   issue.Package,
			IssueType: issue.IssueType.String(),
		})
	}

	out, _ := json.MarshalIndent(result, "", "  ")

	if output := viper.GetString("output"); output != "" {
		return osfs.WriteFile(output, append(out, '\n'), 0644)
	}
	fmt.Println(string(out))
	return nil
}

func runVersion(cmd *cobra.Command, args []string) error {
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return fmt.Errorf("error reading format flag: %w", err)
	}
	switch format {
	case "json":
		buildInfo := version.GetBuildInfo()
		out, err := json.MarshalIndent(buildInfo, "", "  ")
		if err != nil {
			return fmt.Errorf("error marshaling version info: %w", err)
		}
		fmt.Println(string(out))
	default:
		fmt.Printf("mappa %s\n", version.GetVersion())
	}
	return nil
}
