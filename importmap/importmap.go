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
