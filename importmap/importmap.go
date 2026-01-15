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
// Package importmap provides types and operations for ES module import maps.
// See https://developer.mozilla.org/en-US/docs/Web/HTML/Element/script/type/importmap
package importmap

import (
	"encoding/json"
	"maps"
	"strings"
)

// ImportMap represents an ES module import map.
type ImportMap struct {
	// Imports maps module specifiers to URLs.
	Imports map[string]string `json:"imports,omitempty"`

	// Scopes maps URL prefixes to import maps that apply when the referrer
	// URL starts with the scope prefix.
	Scopes map[string]map[string]string `json:"scopes,omitempty"`

	// Integrity maps module URLs to their expected subresource integrity values.
	Integrity map[string]string `json:"integrity,omitempty"`
}

// Parse parses JSON data into an ImportMap.
func Parse(data []byte) (*ImportMap, error) {
	var im ImportMap
	if err := json.Unmarshal(data, &im); err != nil {
		return nil, err
	}
	return &im, nil
}

// Merge combines this import map with another, with the other taking precedence.
// The result is a new ImportMap; neither input is modified.
func (im *ImportMap) Merge(other *ImportMap) *ImportMap {
	if im == nil {
		if other == nil {
			return &ImportMap{}
		}
		return other.Clone()
	}
	if other == nil {
		return im.Clone()
	}

	result := &ImportMap{
		Imports:   make(map[string]string),
		Scopes:    make(map[string]map[string]string),
		Integrity: make(map[string]string),
	}

	// Copy base imports, then override with other's imports
	maps.Copy(result.Imports, im.Imports)
	maps.Copy(result.Imports, other.Imports)

	// Copy base scopes
	for scope, imports := range im.Scopes {
		result.Scopes[scope] = make(map[string]string, len(imports))
		maps.Copy(result.Scopes[scope], imports)
	}
	// Merge other's scopes
	for scope, imports := range other.Scopes {
		if result.Scopes[scope] == nil {
			result.Scopes[scope] = make(map[string]string, len(imports))
		}
		maps.Copy(result.Scopes[scope], imports)
	}

	// Copy base integrity, then override with other's
	maps.Copy(result.Integrity, im.Integrity)
	maps.Copy(result.Integrity, other.Integrity)

	// Clean up empty maps
	if len(result.Imports) == 0 {
		result.Imports = nil
	}
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}
	if len(result.Integrity) == 0 {
		result.Integrity = nil
	}

	return result
}

// Clone creates a deep copy of the import map.
func (im *ImportMap) Clone() *ImportMap {
	if im == nil {
		return nil
	}

	result := &ImportMap{}

	if im.Imports != nil {
		result.Imports = make(map[string]string, len(im.Imports))
		maps.Copy(result.Imports, im.Imports)
	}

	if im.Scopes != nil {
		result.Scopes = make(map[string]map[string]string, len(im.Scopes))
		for scope, imports := range im.Scopes {
			result.Scopes[scope] = make(map[string]string, len(imports))
			maps.Copy(result.Scopes[scope], imports)
		}
	}

	if im.Integrity != nil {
		result.Integrity = make(map[string]string, len(im.Integrity))
		maps.Copy(result.Integrity, im.Integrity)
	}

	return result
}

// Simplify removes import entries that are covered by trailing-slash keys.
// For example, if "lit/" exists, "lit/html.js" is redundant and removed.
// The same simplification is applied to each scope.
// Returns a new ImportMap; the original is not modified.
func (im *ImportMap) Simplify() *ImportMap {
	if im == nil {
		return nil
	}

	result := &ImportMap{}

	// Simplify imports
	if im.Imports != nil {
		result.Imports = simplifyImports(im.Imports)
	}

	// Simplify each scope, omitting scopes that become empty
	if im.Scopes != nil {
		result.Scopes = make(map[string]map[string]string, len(im.Scopes))
		for scope, imports := range im.Scopes {
			simplified := simplifyImports(imports)
			if len(simplified) > 0 {
				result.Scopes[scope] = simplified
			}
		}
	}

	// Copy integrity as-is
	if im.Integrity != nil {
		result.Integrity = make(map[string]string, len(im.Integrity))
		maps.Copy(result.Integrity, im.Integrity)
	}

	// Clean up empty maps
	if len(result.Imports) == 0 {
		result.Imports = nil
	}
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}
	if len(result.Integrity) == 0 {
		result.Integrity = nil
	}

	return result
}

// simplifyImports removes entries covered by trailing-slash keys.
func simplifyImports(imports map[string]string) map[string]string {
	// First, collect all trailing-slash keys
	trailingSlashKeys := make(map[string]bool)
	for key := range imports {
		if strings.HasSuffix(key, "/") {
			trailingSlashKeys[key] = true
		}
	}

	// If no trailing-slash keys, return a copy as-is
	if len(trailingSlashKeys) == 0 {
		result := make(map[string]string, len(imports))
		maps.Copy(result, imports)
		return result
	}

	// Filter out entries covered by trailing-slash keys
	result := make(map[string]string)
	for key, value := range imports {
		// Keep trailing-slash keys themselves
		if strings.HasSuffix(key, "/") {
			result[key] = value
			continue
		}

		// Check if this key is covered by any trailing-slash key
		covered := false
		for tsKey := range trailingSlashKeys {
			prefix := strings.TrimSuffix(tsKey, "/")
			if strings.HasPrefix(key, prefix+"/") {
				covered = true
				break
			}
		}

		if !covered {
			result[key] = value
		}
	}

	return result
}

// ToJSON converts the import map to an indented JSON string.
// Returns an empty string if the import map is nil or entirely empty.
func (im *ImportMap) ToJSON() string {
	if im == nil || (len(im.Imports) == 0 && len(im.Scopes) == 0 && len(im.Integrity) == 0) {
		return ""
	}

	bytes, err := json.MarshalIndent(im, "", "  ")
	if err != nil {
		return ""
	}

	return string(bytes)
}

// MarshalJSON implements json.Marshaler.
func (im *ImportMap) MarshalJSON() ([]byte, error) {
	type alias ImportMap
	return json.Marshal((*alias)(im))
}

// ToHTML wraps the import map JSON in an HTML script tag.
func (im *ImportMap) ToHTML() string {
	jsonStr := im.ToJSON()
	if jsonStr == "" {
		jsonStr = "{}"
	}
	return "<script type=\"importmap\">\n" + jsonStr + "\n</script>"
}

// Format returns the import map in the specified format.
// Supported formats: "json" (default), "html".
// Returns empty JSON object "{}" if the import map is empty.
func (im *ImportMap) Format(format string) string {
	switch format {
	case "html":
		return im.ToHTML()
	default:
		jsonStr := im.ToJSON()
		if jsonStr == "" {
			return "{}"
		}
		return jsonStr
	}
}
