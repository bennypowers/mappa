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
package trace

import (
	"fmt"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// ExtractImports parses JavaScript/TypeScript content and extracts all import specifiers.
func ExtractImports(content []byte) ([]ModuleImport, error) {
	qm, err := GetQueryManager()
	if err != nil {
		return nil, err
	}

	parser := getTSParser()
	defer putTSParser(parser)

	tree := parser.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse content")
	}
	defer tree.Close()

	query, err := qm.Query("typescript", "imports")
	if err != nil {
		return nil, err
	}

	cursor := ts.NewQueryCursor()
	defer cursor.Close()

	var imports []ModuleImport
	matches := cursor.Matches(query, tree.RootNode(), content)
	captureNames := query.CaptureNames()

	for {
		match := matches.Next()
		if match == nil {
			break
		}

		for _, capture := range match.Captures {
			name := captureNames[capture.Index]
			text := capture.Node.Utf8Text(content)
			line := int(capture.Node.StartPosition().Row) + 1 // 1-indexed

			switch name {
			case "import.spec":
				imports = append(imports, ModuleImport{
					Specifier: text,
					IsDynamic: false,
					Line:      line,
				})
			case "dynamicImport.spec":
				imports = append(imports, ModuleImport{
					Specifier: text,
					IsDynamic: true,
					Line:      line,
				})
			case "reexport.spec":
				imports = append(imports, ModuleImport{
					Specifier: text,
					IsDynamic: false,
					Line:      line,
				})
			}
		}
	}

	return imports, nil
}
