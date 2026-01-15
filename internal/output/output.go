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

// Package output provides shared output utilities for mappa CLI commands.
package output

import (
	"fmt"

	"github.com/spf13/viper"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
)

// ImportMap formats and outputs an import map to stdout or a file.
// If viper's "output" flag is set, writes to that file; otherwise prints to stdout.
func ImportMap(osfs fs.FileSystem, im *importmap.ImportMap, format string) error {
	output := im.Format(format)

	if outputPath := viper.GetString("output"); outputPath != "" {
		return osfs.WriteFile(outputPath, []byte(output+"\n"), 0644)
	}
	fmt.Println(output)
	return nil
}
