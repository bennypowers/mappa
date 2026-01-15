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
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// ExtractScripts parses HTML content and extracts all script tags.
// Uses Go's html package for fast parsing instead of tree-sitter.
func ExtractScripts(content []byte) ([]ScriptTag, error) {
	doc, err := html.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	var scripts []ScriptTag
	extractScriptsFromNode(doc, &scripts)

	return scripts, nil
}

// extractScriptsFromNode recursively walks the HTML tree to find script elements.
func extractScriptsFromNode(n *html.Node, scripts *[]ScriptTag) {
	if n.Type == html.ElementNode && n.Data == "script" {
		script := ScriptTag{}

		// Extract attributes
		for _, attr := range n.Attr {
			switch attr.Key {
			case "type":
				script.Type = attr.Val
			case "src":
				script.Src = attr.Val
			}
		}

		// Extract inline content
		if script.Src == "" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			rawContent := strings.TrimSpace(n.FirstChild.Data)
			if rawContent != "" {
				script.Content = rawContent
				script.Inline = true
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

		*scripts = append(*scripts, script)
	}

	// Recurse into children
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractScriptsFromNode(c, scripts)
	}
}
