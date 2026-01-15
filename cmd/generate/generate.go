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
	Cmd.Flags().String("template", "", "URL template (default: /node_modules/{package}/{path})")
	Cmd.Flags().StringSlice("conditions", nil, "Export condition priority (e.g., production,browser,import,default)")

	_ = viper.BindPFlag("format", Cmd.Flags().Lookup("format"))
	_ = viper.BindPFlag("input-map", Cmd.Flags().Lookup("input-map"))
	_ = viper.BindPFlag("include-package", Cmd.Flags().Lookup("include-package"))
	_ = viper.BindPFlag("template", Cmd.Flags().Lookup("template"))
	_ = viper.BindPFlag("conditions", Cmd.Flags().Lookup("conditions"))
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

	// Simplify the import map to remove entries covered by trailing-slash keys
	simplifiedMap := generatedMap.Simplify()

	return output.ImportMap(osfs, simplifiedMap, format)
}
