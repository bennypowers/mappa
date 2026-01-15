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

// Package version provides the version command for mappa.
package version

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"bennypowers.dev/mappa/internal/version"
)

// Cmd is the version command.
var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print version information for mappa.`,
	RunE:  run,
}

func init() {
	Cmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}

func run(cmd *cobra.Command, args []string) error {
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
