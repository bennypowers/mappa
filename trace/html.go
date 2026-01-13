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
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// ExtractScripts parses HTML content and extracts all script tags.
func ExtractScripts(content []byte) ([]ScriptTag, error) {
	qm, err := GetQueryManager()
	if err != nil {
		return nil, err
	}

	parser := getHTMLParser()
	defer putHTMLParser(parser)

	tree := parser.Parse(content, nil)
	defer tree.Close()

	query, err := qm.Query("html", "scriptTags")
	if err != nil {
		return nil, err
	}

	cursor := ts.NewQueryCursor()
	defer cursor.Close()

	var scripts []ScriptTag
	matches := cursor.Matches(query, tree.RootNode(), content)
	captureNames := query.CaptureNames()

	for {
		match := matches.Next()
		if match == nil {
			break
		}

		script := ScriptTag{}
		var currentAttrName string

		for _, capture := range match.Captures {
			name := captureNames[capture.Index]
			text := capture.Node.Utf8Text(content)

			switch name {
			case "attr.name":
				currentAttrName = text
			case "attr.value":
				switch currentAttrName {
				case "type":
					script.Type = text
				case "src":
					script.Src = text
				}
			case "content":
				rawContent := strings.TrimSpace(text)
				if rawContent != "" && script.Src == "" {
					script.Content = rawContent
					script.Inline = true
				}
			}
		}

		// Parse imports from inline content (best-effort; syntax errors are ignored)
		// Handle both type="module" (static + dynamic) and regular scripts (dynamic only)
		if script.Inline && script.Content != "" {
			imports, _ := ExtractImports([]byte(script.Content))
			for _, imp := range imports {
				// For non-module scripts, only include dynamic imports
				if script.Type == "module" || imp.IsDynamic {
					script.Imports = append(script.Imports, imp.Specifier)
				}
			}
		}

		scripts = append(scripts, script)
	}

	return scripts, nil
}
