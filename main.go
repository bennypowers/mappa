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
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
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
	Use:   "trace <file.html>",
	Short: "Trace HTML file and generate minimal import map",
	Long: `Trace an HTML file to find all ES module imports and generate an import map.

By default, outputs an import map containing only the specifiers actually used.
Use --format specifiers for debugging to see the raw trace output.`,
	Example: `  # Trace an HTML file and output minimal import map
  mappa trace index.html

  # Custom URL template for resolved paths
  mappa trace index.html --template "/assets/{package}/{path}"

  # Output as HTML script tag
  mappa trace index.html --format html

  # Output raw specifiers (debugging)
  mappa trace index.html --format specifiers`,
	Args: cobra.ExactArgs(1),
	RunE: runTrace,
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

	// Bind flags to viper
	_ = viper.BindPFlag("package", rootCmd.PersistentFlags().Lookup("package"))
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("format", generateCmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("input-map", generateCmd.Flags().Lookup("input-map"))
	_ = viper.BindPFlag("include-package", generateCmd.Flags().Lookup("include-package"))
	_ = viper.BindPFlag("template", generateCmd.Flags().Lookup("template"))
	_ = viper.BindPFlag("conditions", generateCmd.Flags().Lookup("conditions"))

	// Add commands
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(traceCmd)
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

	jsonOutput := generatedMap.ToJSON()
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

	if viper.GetString("format") == "html" {
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
	htmlFile := args[0]
	osfs := fs.NewOSFileSystem()

	absPath, err := filepath.Abs(htmlFile)
	if err != nil {
		return fmt.Errorf("invalid file path: %w", err)
	}

	absRoot, err := filepath.Abs(viper.GetString("package"))
	if err != nil {
		return fmt.Errorf("invalid package directory: %w", err)
	}

	tracer := trace.NewTracer(osfs, absRoot)
	graph, err := tracer.TraceHTML(absPath)
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

	format, _ := cmd.Flags().GetString("format")

	// Handle specifiers format (legacy output for debugging)
	if format == "specifiers" {
		return runTraceSpecifiers(cmd, osfs, graph, issues)
	}

	// Generate import map from traced specifiers
	templateArg, _ := cmd.Flags().GetString("template")
	if templateArg == "" {
		templateArg = resolve.DefaultLocalTemplate
	}
	conditions, _ := cmd.Flags().GetStringSlice("conditions")

	// Build resolver with traced packages only
	resolver := local.New(osfs, nil).WithPackages(graph.BareSpecifiers())
	resolver, err = resolver.WithTemplate(templateArg)
	if err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}
	if len(conditions) > 0 {
		resolver = resolver.WithConditions(conditions)
	}

	// Find workspace root for node_modules resolution
	workspaceRoot := resolve.FindWorkspaceRoot(osfs, absRoot)
	generatedMap, err := resolver.Resolve(workspaceRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve: %w", err)
	}

	// Filter the import map to only include traced specifiers
	bareSpecifiers := make(map[string]bool)
	for _, spec := range graph.BareSpecifiers() {
		bareSpecifiers[spec] = true
	}

	filteredImports := make(map[string]string)
	for key, value := range generatedMap.Imports {
		if bareSpecifiers[key] {
			filteredImports[key] = value
		}
	}

	filteredMap := &importmap.ImportMap{
		Imports: filteredImports,
		Scopes:  generatedMap.Scopes,
	}

	// Clean up empty scopes
	if len(filteredMap.Scopes) == 0 {
		filteredMap.Scopes = nil
	}

	jsonOutput := filteredMap.ToJSON()
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
