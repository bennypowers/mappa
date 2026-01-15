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

// Package inject provides the inject command for mappa.
package inject

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/inject"
)

// Cmd is the inject command.
var Cmd = &cobra.Command{
	Use:   "inject",
	Short: "Trace HTML files and inject import maps in-place",
	Long: `Trace HTML files and update their import map script tags in-place.

For each file, traces module imports to generate a minimal import map,
merges with any existing manual imports (traced imports take precedence),
and writes the result back to the file.`,
	Example: `  # Inject import maps into all HTML files
  mappa inject --glob "_site/**/*.html"

  # Custom URL template
  mappa inject --glob "_site/**/*.html" --template "/assets/packages/{package}/{path}"

  # Parallel processing with custom worker count
  mappa inject --glob "_site/**/*.html" -j 8

  # Dry run to see what would change
  mappa inject --glob "_site/**/*.html" --dry-run`,
	RunE: run,
}

func init() {
	Cmd.Flags().String("glob", "", "Glob pattern to match HTML files (required)")
	Cmd.Flags().String("template", "", "URL template (default: /node_modules/{package}/{path})")
	Cmd.Flags().StringSlice("conditions", nil, "Export condition priority (e.g., production,browser,import,default)")
	Cmd.Flags().IntP("jobs", "j", 0, "Number of parallel workers (default: number of CPUs)")
	Cmd.Flags().Bool("dry-run", false, "Show what would change without modifying files")
	Cmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}

func run(cmd *cobra.Command, args []string) error {
	osfs := fs.NewOSFileSystem()

	absRoot, err := filepath.Abs(viper.GetString("package"))
	if err != nil {
		return fmt.Errorf("invalid package directory: %w", err)
	}

	// Collect files from glob pattern
	globPattern, _ := cmd.Flags().GetString("glob")
	if globPattern == "" {
		return fmt.Errorf("--glob is required")
	}

	matches, err := doublestar.FilepathGlob(globPattern)
	if err != nil {
		return fmt.Errorf("invalid glob pattern: %w", err)
	}

	if len(matches) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: no files matched the glob pattern")
		return nil
	}

	// Deduplicate by absolute path
	seen := make(map[string]struct{})
	var files []string
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

	// Get flags
	templateArg, _ := cmd.Flags().GetString("template")
	conditions, _ := cmd.Flags().GetStringSlice("conditions")
	parallel, _ := cmd.Flags().GetInt("jobs")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	format, _ := cmd.Flags().GetString("format")

	opts := inject.Options{
		Template:   templateArg,
		Conditions: conditions,
		Parallel:   parallel,
		DryRun:     dryRun,
	}

	// Run inject
	results := inject.InjectBatch(osfs, files, absRoot, opts)

	// Collect results
	var stats inject.Stats
	stats.Total = len(files)

	encoder := json.NewEncoder(os.Stdout)
	for result := range results {
		if result.Error != "" {
			stats.Errors++
			if format == "json" {
				_ = encoder.Encode(result)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %s: %s\n", result.File, result.Error)
			}
		} else if result.Modified {
			if result.Inserted {
				stats.Inserted++
			} else {
				stats.Updated++
			}
			if format == "json" {
				_ = encoder.Encode(result)
			} else if dryRun {
				action := "would update"
				if result.Inserted {
					action = "would insert into"
				}
				fmt.Printf("%s %s\n", action, result.File)
			}
		} else {
			stats.Skipped++
		}
	}

	// Output summary
	if format == "text" {
		if dryRun {
			fmt.Printf("\nDry run: %d files would be modified (%d updated, %d new), %d unchanged, %d errors\n",
				stats.Updated+stats.Inserted, stats.Updated, stats.Inserted, stats.Skipped, stats.Errors)
		} else {
			fmt.Printf("Injected: %d files modified (%d updated, %d new), %d unchanged, %d errors\n",
				stats.Updated+stats.Inserted, stats.Updated, stats.Inserted, stats.Skipped, stats.Errors)
		}
	} else {
		statsJSON, _ := json.Marshal(stats)
		fmt.Println(string(statsJSON))
	}

	if stats.Errors == stats.Total {
		return fmt.Errorf("all %d files failed", stats.Errors)
	}

	return nil
}
