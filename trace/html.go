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

// ImportMapLocation describes an existing import map script tag in HTML.
type ImportMapLocation struct {
	Found        bool // True if an import map tag was found
	TagStart     int  // Byte offset of opening <script
	TagEnd       int  // Byte offset after closing </script>
	ContentStart int  // Byte offset of JSON content start
	ContentEnd   int  // Byte offset of JSON content end
	Line         int  // 1-indexed line number for warnings
}

// InsertPoint describes where to insert a new import map in HTML.
type InsertPoint struct {
	Found  bool   // True if a valid insertion point was found
	Offset int    // Byte offset for insertion
	Indent string // Whitespace to use for indentation
}

// FindImportMapTag locates the first <script type="importmap"> tag in HTML content.
// Returns byte positions for the tag and its content.
func FindImportMapTag(content []byte) ImportMapLocation {
	tokenizer := html.NewTokenizer(bytes.NewReader(content))
	offset := 0
	line := 1

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}

		raw := tokenizer.Raw()
		rawLen := len(raw)

		// Count newlines for line tracking
		linesBefore := bytes.Count(content[offset:offset+rawLen], []byte("\n"))

		if tt == html.StartTagToken {
			tagName, hasAttr := tokenizer.TagName()
			if string(tagName) == "script" && hasAttr {
				// Check if type="importmap"
				isImportMap := false
				for {
					key, val, more := tokenizer.TagAttr()
					if string(key) == "type" && string(val) == "importmap" {
						isImportMap = true
					}
					if !more {
						break
					}
				}

				if isImportMap {
					tagStart := offset
					tagLine := line + linesBefore

					// Move past opening tag
					offset += rawLen
					line += linesBefore

					// Get text content
					switch tt = tokenizer.Next(); tt {
					case html.TextToken:
						textRaw := tokenizer.Raw()
						contentStart := offset
						contentEnd := offset + len(textRaw)
						offset += len(textRaw)
						line += bytes.Count(textRaw, []byte("\n"))

						// Get closing tag
						if tt = tokenizer.Next(); tt == html.EndTagToken {
							endRaw := tokenizer.Raw()
							tagEnd := offset + len(endRaw)

							return ImportMapLocation{
								Found:        true,
								TagStart:     tagStart,
								TagEnd:       tagEnd,
								ContentStart: contentStart,
								ContentEnd:   contentEnd,
								Line:         tagLine,
							}
						}
					case html.EndTagToken:
						// Empty import map: <script type="importmap"></script>
						endRaw := tokenizer.Raw()
						tagEnd := offset + len(endRaw)

						return ImportMapLocation{
							Found:        true,
							TagStart:     tagStart,
							TagEnd:       tagEnd,
							ContentStart: offset,
							ContentEnd:   offset,
							Line:         tagLine,
						}
					}
				}
			}
		}

		offset += rawLen
		line += linesBefore
	}

	return ImportMapLocation{Found: false}
}

// FindInsertPoint locates where to insert a new import map in HTML content.
// Prefers: before first <script> in <head>, or before </head> if no scripts.
func FindInsertPoint(content []byte) InsertPoint {
	tokenizer := html.NewTokenizer(bytes.NewReader(content))
	offset := 0
	inHead := false
	headEnd := -1
	firstScriptInHead := -1
	var indent string

	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}

		raw := tokenizer.Raw()
		rawLen := len(raw)

		switch tt {
		case html.StartTagToken:
			tagName, _ := tokenizer.TagName()
			tagNameStr := string(tagName)

			if tagNameStr == "head" {
				inHead = true
			} else if tagNameStr == "script" && inHead && firstScriptInHead == -1 {
				// Found first script in head - insert before it
				firstScriptInHead = offset

				// Capture preceding whitespace for indentation
				indent = extractIndent(content, offset)
			}
		case html.EndTagToken:
			tagName, _ := tokenizer.TagName()
			if string(tagName) == "head" && inHead {
				headEnd = offset
				if indent == "" {
					indent = extractIndent(content, offset)
				}
				inHead = false
			}
		}

		offset += rawLen
	}

	// Prefer inserting before first script in head
	if firstScriptInHead != -1 {
		return InsertPoint{
			Found:  true,
			Offset: firstScriptInHead,
			Indent: indent,
		}
	}

	// Fall back to before </head>
	if headEnd != -1 {
		return InsertPoint{
			Found:  true,
			Offset: headEnd,
			Indent: indent,
		}
	}

	return InsertPoint{Found: false}
}

// extractIndent extracts leading whitespace from the line containing the given offset.
func extractIndent(content []byte, offset int) string {
	// Find start of line
	lineStart := offset
	for lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart--
	}

	// Extract whitespace
	var indent strings.Builder
	for i := lineStart; i < offset; i++ {
		if content[i] == ' ' || content[i] == '\t' {
			indent.WriteByte(content[i])
		} else {
			break
		}
	}

	return indent.String()
}

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
