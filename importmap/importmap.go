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
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
)

// ValidationError describes a spec violation in an import map.
type ValidationError struct {
	Key     string `json:"key,omitempty"`
	Value   string `json:"value,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e.Scope != "" && e.Key != "" {
		return fmt.Sprintf("scope %q key %q: %s", e.Scope, e.Key, e.Message)
	}
	if e.Scope != "" {
		return fmt.Sprintf("scope %q: %s", e.Scope, e.Message)
	}
	return fmt.Sprintf("key %q: %s", e.Key, e.Message)
}

// Validate checks the import map for WHATWG spec violations.
// Returns a slice of validation errors (empty if valid).
func (im *ImportMap) Validate() []*ValidationError {
	if im == nil {
		return nil
	}

	var errs []*ValidationError
	errs = append(errs, validateNullSpecifierKeys(im.nullImports, "")...)
	errs = append(errs, validateSpecifierMap(im.Imports, "")...)
	validatedScopes := make(map[string]bool, len(im.Scopes)+len(im.nullScopes))
	for scope, imports := range im.Scopes {
		validatedScopes[scope] = true
		if !isValidSpecifierValue(scope) {
			errs = append(errs, &ValidationError{
				Scope:   scope,
				Message: "scope key must be a valid URL or start with /, ./, or ../",
			})
		}
		errs = append(errs, validateSpecifierMap(imports, scope)...)
	}
	for scope, keys := range im.nullScopes {
		if !validatedScopes[scope] && !isValidSpecifierValue(scope) {
			errs = append(errs, &ValidationError{
				Scope:   scope,
				Message: "scope key must be a valid URL or start with /, ./, or ../",
			})
		}
		errs = append(errs, validateNullSpecifierKeys(keys, scope)...)
	}
	// Validation walks maps, whose iteration order is randomized. Sort so
	// callers see a stable error sequence across calls.
	slices.SortStableFunc(errs, func(a, b *ValidationError) int {
		if c := cmp.Compare(a.Scope, b.Scope); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Key, b.Key); c != 0 {
			return c
		}
		return cmp.Compare(a.Message, b.Message)
	})
	return errs
}

// validateNullSpecifierKeys applies key validation to null-valued entries,
// which live in side tables because map[string]string cannot represent them.
func validateNullSpecifierKeys(keys map[string]bool, scope string) []*ValidationError {
	var errs []*ValidationError
	for key := range keys {
		if key == "" {
			errs = append(errs, &ValidationError{
				Key:     key,
				Scope:   scope,
				Message: "specifier key must not be empty",
			})
		}
	}
	return errs
}

func validateSpecifierMap(imports map[string]string, scope string) []*ValidationError {
	var errs []*ValidationError
	for key, value := range imports {
		if key == "" {
			errs = append(errs, &ValidationError{
				Key:     key,
				Value:   value,
				Scope:   scope,
				Message: "specifier key must not be empty",
			})
		}
		keySlash := strings.HasSuffix(key, "/")
		valueSlash := strings.HasSuffix(value, "/")
		if keySlash && !valueSlash {
			errs = append(errs, &ValidationError{
				Key:     key,
				Value:   value,
				Scope:   scope,
				Message: "trailing-slash key must map to a value ending with /",
			})
		}
		if !isValidSpecifierValue(value) {
			errs = append(errs, &ValidationError{
				Key:     key,
				Value:   value,
				Scope:   scope,
				Message: "value must be a valid URL or start with /, ./, or ../",
			})
		}
	}
	return errs
}

func isValidSpecifierValue(value string) bool {
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "./") ||
		strings.HasPrefix(value, "../") {
		return true
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		return false
	}
	return u.Opaque != "" || u.Host != "" || strings.HasPrefix(u.Path, "/")
}

// ImportMap represents an ES module import map.
type ImportMap struct {
	// Imports maps module specifiers to URLs.
	Imports map[string]string `json:"imports,omitempty"`

	// Scopes maps URL prefixes to import maps that apply when the referrer
	// URL starts with the scope prefix.
	Scopes map[string]map[string]string `json:"scopes,omitempty"`

	// Integrity maps module URLs to their expected subresource integrity values.
	Integrity map[string]string `json:"integrity,omitempty"`

	// nullImports and nullScopes track keys whose value was JSON null. The
	// spec allows null to block a specifier, but map[string]string cannot
	// represent it; these side tables preserve it across round-trips.
	nullImports map[string]bool
	nullScopes  map[string]map[string]bool
}

// isJSONNull reports whether the raw JSON value is null.
func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// UnmarshalJSON parses an import map, preserving null values.
func (im *ImportMap) UnmarshalJSON(data []byte) error {
	var raw struct {
		Imports   map[string]json.RawMessage            `json:"imports"`
		Scopes    map[string]map[string]json.RawMessage `json:"scopes"`
		Integrity map[string]string                     `json:"integrity"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	im.Imports = nil
	im.nullImports = nil
	im.Scopes = nil
	im.nullScopes = nil
	im.Integrity = raw.Integrity

	if raw.Imports != nil {
		im.Imports = make(map[string]string, len(raw.Imports))
		for key, value := range raw.Imports {
			if isJSONNull(value) {
				if im.nullImports == nil {
					im.nullImports = make(map[string]bool)
				}
				im.nullImports[key] = true
				continue
			}
			var s string
			if err := json.Unmarshal(value, &s); err != nil {
				return fmt.Errorf("imports[%q]: %w", key, err)
			}
			im.Imports[key] = s
		}
	}

	if raw.Scopes != nil {
		im.Scopes = make(map[string]map[string]string, len(raw.Scopes))
		for scope, entries := range raw.Scopes {
			im.Scopes[scope] = make(map[string]string, len(entries))
			for key, value := range entries {
				if isJSONNull(value) {
					if im.nullScopes == nil {
						im.nullScopes = make(map[string]map[string]bool)
					}
					if im.nullScopes[scope] == nil {
						im.nullScopes[scope] = make(map[string]bool)
					}
					im.nullScopes[scope][key] = true
					continue
				}
				var s string
				if err := json.Unmarshal(value, &s); err != nil {
					return fmt.Errorf("scopes[%q][%q]: %w", scope, key, err)
				}
				im.Scopes[scope][key] = s
			}
		}
	}

	return nil
}

// MarshalJSON serializes an import map, re-emitting null values.
func (im *ImportMap) MarshalJSON() ([]byte, error) {
	type output struct {
		Imports   map[string]any            `json:"imports,omitempty"`
		Scopes    map[string]map[string]any `json:"scopes,omitempty"`
		Integrity map[string]string         `json:"integrity,omitempty"`
	}

	out := output{Integrity: im.Integrity}

	if im.Imports != nil || len(im.nullImports) > 0 {
		out.Imports = make(map[string]any, len(im.Imports)+len(im.nullImports))
		for key, value := range im.Imports {
			out.Imports[key] = value
		}
		for key := range im.nullImports {
			if _, ok := out.Imports[key]; !ok {
				out.Imports[key] = nil
			}
		}
	}

	if im.Scopes != nil || len(im.nullScopes) > 0 {
		out.Scopes = make(map[string]map[string]any, len(im.Scopes)+len(im.nullScopes))
		for scope, entries := range im.Scopes {
			out.Scopes[scope] = make(map[string]any, len(entries))
			for key, value := range entries {
				out.Scopes[scope][key] = value
			}
		}
		for scope, keys := range im.nullScopes {
			if out.Scopes[scope] == nil {
				out.Scopes[scope] = make(map[string]any)
			}
			for key := range keys {
				if _, ok := out.Scopes[scope][key]; !ok {
					out.Scopes[scope][key] = nil
				}
			}
		}
	}

	return json.Marshal(out)
}

// Lookup resolves a bare specifier against this import map, honoring exact
// entries and the longest matching trailing-slash key. A null entry blocks the
// specifier, so a matching null key reports no resolution.
func (im *ImportMap) Lookup(specifier string) (string, bool) {
	if im == nil {
		return "", false
	}
	if im.nullImports[specifier] {
		return "", false
	}
	if value, ok := im.Imports[specifier]; ok {
		return value, true
	}
	var (
		bestKey   string
		bestValue string
		bestNull  bool
	)
	for key, value := range im.Imports {
		if !strings.HasSuffix(key, "/") || !strings.HasPrefix(specifier, key) {
			continue
		}
		if len(key) > len(bestKey) {
			bestKey = key
			bestValue = value + specifier[len(key):]
			bestNull = false
		}
	}
	for key := range im.nullImports {
		if !strings.HasSuffix(key, "/") || !strings.HasPrefix(specifier, key) {
			continue
		}
		if len(key) > len(bestKey) {
			bestKey = key
			bestValue = ""
			bestNull = true
		}
	}
	if bestKey == "" || bestNull {
		return "", false
	}
	return bestValue, true
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

	// Merge nulls with other taking precedence.
	for key := range im.nullImports {
		if _, ok := other.Imports[key]; ok || other.nullImports[key] {
			continue
		}
		if result.nullImports == nil {
			result.nullImports = make(map[string]bool)
		}
		result.nullImports[key] = true
		delete(result.Imports, key)
	}
	for key := range other.nullImports {
		if result.nullImports == nil {
			result.nullImports = make(map[string]bool)
		}
		result.nullImports[key] = true
		delete(result.Imports, key)
	}

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

	// Merge null scope entries with other taking precedence.
	for scope, keys := range im.nullScopes {
		for key := range keys {
			if other.Scopes[scope] != nil {
				if _, ok := other.Scopes[scope][key]; ok {
					continue
				}
			}
			if other.nullScopes[scope][key] {
				continue
			}
			if result.nullScopes == nil {
				result.nullScopes = make(map[string]map[string]bool)
			}
			if result.nullScopes[scope] == nil {
				result.nullScopes[scope] = make(map[string]bool)
			}
			result.nullScopes[scope][key] = true
			delete(result.Scopes[scope], key)
		}
	}
	for scope, keys := range other.nullScopes {
		for key := range keys {
			if result.nullScopes == nil {
				result.nullScopes = make(map[string]map[string]bool)
			}
			if result.nullScopes[scope] == nil {
				result.nullScopes[scope] = make(map[string]bool)
			}
			result.nullScopes[scope][key] = true
			delete(result.Scopes[scope], key)
		}
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

	if len(im.nullImports) > 0 {
		result.nullImports = make(map[string]bool, len(im.nullImports))
		maps.Copy(result.nullImports, im.nullImports)
	}

	if len(im.nullScopes) > 0 {
		result.nullScopes = make(map[string]map[string]bool, len(im.nullScopes))
		for scope, keys := range im.nullScopes {
			result.nullScopes[scope] = make(map[string]bool, len(keys))
			maps.Copy(result.nullScopes[scope], keys)
		}
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

	// Simplify each scope, omitting scopes that become empty or redundant
	if im.Scopes != nil {
		result.Scopes = make(map[string]map[string]string, len(im.Scopes))
		for scope, imports := range im.Scopes {
			simplified := simplifyImports(imports)
			if len(simplified) == 0 {
				continue
			}
			// Remove scope entries redundant with top-level imports
			if result.Imports != nil {
				deduplicated := make(map[string]string)
				for key, value := range simplified {
					if topLevel, ok := result.Imports[key]; ok && topLevel == value {
						continue
					}
					covered := false
					for tlKey, tlValue := range result.Imports {
						if !strings.HasSuffix(tlKey, "/") {
							continue
						}
						if rel, ok := strings.CutPrefix(key, tlKey); ok && tlValue+rel == value {
							covered = true
							break
						}
					}
					if !covered {
						deduplicated[key] = value
					}
				}
				simplified = deduplicated
			}
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

	// Preserve explicit null entries.
	if len(im.nullImports) > 0 {
		result.nullImports = make(map[string]bool, len(im.nullImports))
		maps.Copy(result.nullImports, im.nullImports)
	}
	if len(im.nullScopes) > 0 {
		result.nullScopes = make(map[string]map[string]bool, len(im.nullScopes))
		for scope, keys := range im.nullScopes {
			result.nullScopes[scope] = make(map[string]bool, len(keys))
			maps.Copy(result.nullScopes[scope], keys)
		}
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

// SimplifyEntries removes entries from a specifier map that are covered by
// trailing-slash keys. Useful for simplifying scope entries independently.
func SimplifyEntries(imports map[string]string) map[string]string {
	return simplifyImports(imports)
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

	// Filter out entries whose target matches the trailing-slash expansion.
	// An explicit entry like "lit/foo": "/custom/override.js" is kept when
	// its target differs from what "lit/" would resolve to.
	result := make(map[string]string)
	for key, value := range imports {
		if strings.HasSuffix(key, "/") {
			result[key] = value
			continue
		}

		covered := false
		for tsKey := range trailingSlashKeys {
			if strings.HasPrefix(key, tsKey) {
				relPath := key[len(tsKey):]
				baseTarget := imports[tsKey]
				if baseTarget+relPath == value {
					covered = true
					break
				}
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
	if im == nil || (len(im.Imports) == 0 && len(im.Scopes) == 0 && len(im.Integrity) == 0 &&
		len(im.nullImports) == 0 && len(im.nullScopes) == 0) {
		return ""
	}

	bytes, err := json.MarshalIndent(im, "", "  ")
	if err != nil {
		return ""
	}

	return string(bytes)
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
