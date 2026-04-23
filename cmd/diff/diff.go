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

// Package diff provides the diff command for mappa.
package diff

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
)

// Cmd is the diff cobra command that compares two import map JSON files.
var Cmd = &cobra.Command{
	Use:   "diff <file-a> <file-b>",
	Short: "Compare two import map JSON files",
	Long: `Compare two import map JSON files and show differences.

Shows added, removed, and modified entries for both imports and scopes.
Exits 0 if the maps are identical, 1 if they differ.`,
	Example: `  # Compare two import maps (colored text output)
  mappa diff old-importmap.json new-importmap.json

  # JSON output
  mappa diff old-importmap.json new-importmap.json --format json`,
	Args: cobra.ExactArgs(2),
	RunE: run,
}

func init() {
	Cmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}

func run(cmd *cobra.Command, args []string) error {
	osfs := fs.NewOSFileSystem()
	format, _ := cmd.Flags().GetString("format")
	if format != "text" && format != "json" {
		return fmt.Errorf("invalid format %q: must be 'text' or 'json'", format)
	}

	aData, err := osfs.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", args[0], err)
	}

	bData, err := osfs.ReadFile(args[1])
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", args[1], err)
	}

	a, err := importmap.Parse(aData)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", args[0], err)
	}

	b, err := importmap.Parse(bData)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", args[1], err)
	}

	d := importmap.Compare(a, b)
	w := cmd.OutOrStdout()

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(d); err != nil {
			return fmt.Errorf("failed to encode diff: %w", err)
		}
	} else {
		printTextDiff(w, d)
	}

	if !d.Empty() {
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return fmt.Errorf("import maps differ")
	}

	return nil
}

const (
	colorRed   = "\033[31m"
	colorGreen = "\033[32m"
	colorCyan  = "\033[36m"
	colorReset = "\033[0m"
)

func printTextDiff(w io.Writer, d *importmap.Diff) {
	if d.Empty() {
		_, _ = fmt.Fprintln(w, "Import maps are identical.")
		return
	}

	if !d.Imports.Empty() {
		_, _ = fmt.Fprintln(w, "imports:")
		printMapDiff(w, d.Imports, "  ")
	}

	for _, scope := range d.AddedScopes {
		_, _ = fmt.Fprintf(w, "%s+ scope %s%s\n", colorGreen, scope, colorReset)
	}

	for _, scope := range d.RemovedScopes {
		_, _ = fmt.Fprintf(w, "%s- scope %s%s\n", colorRed, scope, colorReset)
	}

	scopeKeys := make([]string, 0, len(d.Scopes))
	for k := range d.Scopes {
		scopeKeys = append(scopeKeys, k)
	}
	sort.Strings(scopeKeys)

	for _, scope := range scopeKeys {
		sd := d.Scopes[scope]
		if !sd.Empty() {
			_, _ = fmt.Fprintf(w, "scope %s:\n", scope)
			printMapDiff(w, sd, "  ")
		}
	}
}

func printMapDiff(w io.Writer, d importmap.MapDiff, indent string) {
	keys := sortedKeys(d.Added)
	for _, key := range keys {
		_, _ = fmt.Fprintf(w, "%s%s+ %s: %s%s\n", indent, colorGreen, key, d.Added[key], colorReset)
	}

	keys = sortedKeys(d.Removed)
	for _, key := range keys {
		_, _ = fmt.Fprintf(w, "%s%s- %s: %s%s\n", indent, colorRed, key, d.Removed[key], colorReset)
	}

	modKeys := make([]string, 0, len(d.Modified))
	for k := range d.Modified {
		modKeys = append(modKeys, k)
	}
	sort.Strings(modKeys)

	for _, key := range modKeys {
		vals := d.Modified[key]
		_, _ = fmt.Fprintf(w, "%s%s~ %s:%s\n", indent, colorCyan, key, colorReset)
		_, _ = fmt.Fprintf(w, "%s  %s- %s%s\n", indent, colorRed, vals[0], colorReset)
		_, _ = fmt.Fprintf(w, "%s  %s+ %s%s\n", indent, colorGreen, vals[1], colorReset)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
