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
	Short: "Trace HTML file and list bare specifiers",
	Long: `Trace an HTML file to find all ES module imports.

Outputs a JSON object with entrypoints, modules, bare specifiers,
and package names discovered during tracing.`,
	Example: `  # Trace an HTML file
  mappa trace index.html

  # Trace with a specific package directory
  mappa trace index.html --package ./src`,
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
		inputMapData, err := os.ReadFile(inputMapPath)
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
		return os.WriteFile(output, []byte(jsonOutput+"\n"), 0644)
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

	result := struct {
		Entrypoints    []string `json:"entrypoints"`
		Modules        []string `json:"modules"`
		BareSpecifiers []string `json:"bare_specifiers"`
		Packages       []string `json:"packages"`
	}{
		Entrypoints:    graph.Entrypoints,
		BareSpecifiers: graph.BareSpecifiers(),
		Packages:       graph.PackageNames(),
	}

	for p := range graph.Modules {
		result.Modules = append(result.Modules, p)
	}
	sort.Strings(result.Modules)

	out, _ := json.MarshalIndent(result, "", "  ")

	if output := viper.GetString("output"); output != "" {
		return os.WriteFile(output, append(out, '\n'), 0644)
	}
	fmt.Println(string(out))
	return nil
}
