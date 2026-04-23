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

// Package validate provides the validate command for mappa.
package validate

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
)

// Cmd is the validate cobra command that checks import maps for WHATWG spec violations.
var Cmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate an import map for spec violations",
	Long: `Validate an import map file against the WHATWG import map specification.

Reads an import map from a file argument or stdin, checks for spec violations
(trailing-slash consistency, URL format, scope key validity), and reports errors.
Exits 0 if the import map is valid, 1 if there are violations.`,
	Example: `  # Validate an import map file
  mappa validate importmap.json

  # Validate from stdin
  cat importmap.json | mappa validate

  # Machine-readable JSON output
  mappa validate importmap.json --format json`,
	Args: cobra.MaximumNArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}

func run(cmd *cobra.Command, args []string) error {
	var data []byte
	var err error

	if len(args) == 1 {
		osfs := fs.NewOSFileSystem()
		data, err = osfs.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
	} else {
		data, err = io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
	}

	im, err := importmap.Parse(data)
	if err != nil {
		return fmt.Errorf("failed to parse import map: %w", err)
	}

	errs := im.Validate()

	format, _ := cmd.Flags().GetString("format")
	if format != "text" && format != "json" {
		return fmt.Errorf("invalid format %q: must be 'text' or 'json'", format)
	}

	if format == "json" {
		out, err := json.MarshalIndent(errs, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal errors: %w", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
	} else {
		for _, e := range errs {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", e)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("import map has %d validation error(s)", len(errs))
	}

	return nil
}
