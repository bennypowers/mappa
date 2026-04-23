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

// Package generate provides the generate command for mappa.
package generate

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/internal/output"
	"bennypowers.dev/mappa/resolve"
	"bennypowers.dev/mappa/resolve/local"
)

// Cmd is the generate cobra command that creates import maps from package.json dependencies.
var Cmd = &cobra.Command{
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

  # Exclude packages from the generated map
  mappa generate --exclude lodash --exclude underscore

  # Merge with an existing import map (input map takes precedence)
  mappa generate --input-map manual-imports.json

  # Output as HTML script tag
  mappa generate --format html`,
	RunE: run,
}

func init() {
	Cmd.Flags().StringP("format", "f", "json", "Output format (json, html)")
	Cmd.Flags().String("input-map", "", "Import map file to merge with generated output")
	Cmd.Flags().StringArray("include-package", nil, "Additional packages to include (can be repeated)")
	Cmd.Flags().StringArray("exclude", nil, "Exclude packages from the generated map (can be repeated)")
	Cmd.Flags().String("template", "", "URL template (default: /node_modules/{package}/{path})")
	Cmd.Flags().StringSlice("conditions", nil, "Export condition priority (e.g., production,browser,import,default)")
	Cmd.Flags().IntP("optimize", "O", 1, "Optimization level: 0=none, 1=simplify+dedup (default)")
	Cmd.Flags().Bool("strict", false, "Exit non-zero on import map validation warnings")

	_ = viper.BindPFlag("format", Cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("input-map", Cmd.Flags().Lookup("input-map"))
	_ = viper.BindPFlag("include-package", Cmd.Flags().Lookup("include-package"))
	_ = viper.BindPFlag("exclude", Cmd.Flags().Lookup("exclude"))
	_ = viper.BindPFlag("template", Cmd.Flags().Lookup("template"))
	_ = viper.BindPFlag("conditions", Cmd.Flags().Lookup("conditions"))
	_ = viper.BindPFlag("optimize", Cmd.Flags().Lookup("optimize"))
	_ = viper.BindPFlag("strict", Cmd.Flags().Lookup("strict"))
}

func run(cmd *cobra.Command, args []string) error {
	osfs := fs.NewOSFileSystem()
	absRoot, err := filepath.Abs(viper.GetString("package"))
	if err != nil {
		return fmt.Errorf("invalid package directory: %w", err)
	}

	// Validate format flag
	format := viper.GetString("format")
	if format != "json" && format != "html" {
		return fmt.Errorf("invalid format %q: must be 'json' or 'html'", format)
	}

	// Get additional packages and exclusions
	includePackages := viper.GetStringSlice("include-package")
	excludePackages := viper.GetStringSlice("exclude")

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
	if len(excludePackages) > 0 {
		resolver = resolver.WithExclude(excludePackages)
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

	optimizeLevel := viper.GetInt("optimize")
	outputMap := generatedMap
	if optimizeLevel >= 1 {
		outputMap = outputMap.Simplify()
	}

	if errs := outputMap.Validate(); len(errs) > 0 {
		for _, e := range errs {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", e)
		}
		if viper.GetBool("strict") {
			return fmt.Errorf("import map has %d validation warning(s)", len(errs))
		}
	}

	return output.ImportMap(osfs, outputMap, format)
}
