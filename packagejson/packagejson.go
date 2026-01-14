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
// Package packagejson provides parsing and export resolution for package.json files.
package packagejson

import (
	"encoding/json"
	"errors"
	"strings"

	"bennypowers.dev/mappa/fs"
)

// workspacesObjectFormat represents the object format for workspaces field.
// Used by yarn classic with nohoist: {"packages": [...], "nohoist": [...]}
type workspacesObjectFormat struct {
	Packages []string `json:"packages"`
}

// ErrNotExported is returned when a subpath is not exported by the package.
var ErrNotExported = errors.New("not exported by package.json")

// DefaultConditions is the default export condition priority for browser environments.
var DefaultConditions = []string{"browser", "import", "default"}

// ResolveOptions configures how conditional exports are resolved.
type ResolveOptions struct {
	// Conditions is the ordered list of conditions to try when resolving exports.
	// If nil, defaults to DefaultConditions.
	Conditions []string
}

// PackageJSON represents the subset of package.json relevant for import maps.
type PackageJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Main            string            `json:"main,omitempty"`
	Module          string            `json:"module,omitempty"`
	Exports         any               `json:"exports,omitempty"`
	Imports         any               `json:"imports,omitempty"`
	Dependencies    map[string]string `json:"dependencies,omitempty"`
	DevDependencies map[string]string `json:"devDependencies,omitempty"`
	RawWorkspaces   json.RawMessage   `json:"workspaces,omitempty"`
}

// WorkspacePatterns returns the workspace glob patterns from the workspaces field.
// Handles both array format ["packages/*"] and object format {"packages": ["libs/*"]}.
func (pkg *PackageJSON) WorkspacePatterns() []string {
	if len(pkg.RawWorkspaces) == 0 {
		return nil
	}

	// Try array format first (most common)
	var patterns []string
	if err := json.Unmarshal(pkg.RawWorkspaces, &patterns); err == nil {
		return patterns
	}

	// Try object format with "packages" key (yarn classic with nohoist)
	var obj workspacesObjectFormat
	if err := json.Unmarshal(pkg.RawWorkspaces, &obj); err == nil {
		return obj.Packages
	}

	return nil
}

// HasWorkspaces returns true if the package has workspace patterns defined.
func (pkg *PackageJSON) HasWorkspaces() bool {
	return len(pkg.WorkspacePatterns()) > 0
}

// ExportEntry represents a single export from a package.
type ExportEntry struct {
	Subpath string // The export subpath (e.g., ".", "./button")
	Target  string // The resolved target path (e.g., "index.js")
}

// WildcardExport represents a wildcard export pattern.
type WildcardExport struct {
	Pattern string // The pattern (e.g., "./*")
	Target  string // The target prefix (e.g., "dist/")
}

// Parse parses package.json data.
func Parse(data []byte) (*PackageJSON, error) {
	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

// ParseFile parses a package.json file.
func ParseFile(fs fs.FileSystem, path string) (*PackageJSON, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// ResolveExport resolves a subpath export to its target file path.
// The subpath should be "." for the main export or "./subpath" for subpath exports.
// Returns the resolved path without leading "./".
// Pass nil for opts to use DefaultConditions.
func (pkg *PackageJSON) ResolveExport(subpath string, opts *ResolveOptions) (string, error) {
	if pkg.Exports == nil {
		// Fall back to main field
		if pkg.Main != "" {
			if subpath == "." {
				return trimDotSlash(pkg.Main), nil
			}
			return "", ErrNotExported
		}
		return "", ErrNotExported
	}

	// Handle string export (simple case)
	if exportStr, ok := pkg.Exports.(string); ok {
		if subpath == "." {
			return trimDotSlash(exportStr), nil
		}
		return "", ErrNotExported
	}

	// Handle exports map
	exportsMap, ok := pkg.Exports.(map[string]any)
	if !ok {
		return "", ErrNotExported
	}

	// Check if this is a condition-only export (no subpaths)
	hasSubpaths := false
	for key := range exportsMap {
		if strings.HasPrefix(key, ".") {
			hasSubpaths = true
			break
		}
	}

	if !hasSubpaths {
		// This is a condition-only export for the main entry
		if subpath == "." {
			return resolveConditionsWithOpts(exportsMap, opts)
		}
		return "", ErrNotExported
	}

	// Look up the subpath
	exportValue, ok := exportsMap[subpath]
	if !ok {
		return "", ErrNotExported
	}

	return resolveExportValueWithOpts(exportValue, opts)
}

// ExportEntries returns all non-wildcard export entries from the package.
// Pass nil for opts to use DefaultConditions.
func (pkg *PackageJSON) ExportEntries(opts *ResolveOptions) []ExportEntry {
	var entries []ExportEntry

	if pkg.Exports == nil {
		// No exports field - check main
		if pkg.Main != "" {
			entries = append(entries, ExportEntry{
				Subpath: ".",
				Target:  trimDotSlash(pkg.Main),
			})
		}
		return entries
	}

	// Handle string export
	if exportStr, ok := pkg.Exports.(string); ok {
		entries = append(entries, ExportEntry{
			Subpath: ".",
			Target:  trimDotSlash(exportStr),
		})
		return entries
	}

	// Handle exports map
	exportsMap, ok := pkg.Exports.(map[string]any)
	if !ok {
		return entries
	}

	// Check if this is a condition-only export
	hasSubpaths := false
	for key := range exportsMap {
		if strings.HasPrefix(key, ".") {
			hasSubpaths = true
			break
		}
	}

	if !hasSubpaths {
		// Condition-only export for main entry
		if resolved, err := resolveConditionsWithOpts(exportsMap, opts); err == nil {
			entries = append(entries, ExportEntry{
				Subpath: ".",
				Target:  resolved,
			})
		}
		return entries
	}

	// Process each subpath
	for subpath, exportValue := range exportsMap {
		// Skip wildcards
		if strings.Contains(subpath, "*") {
			continue
		}

		resolved, err := resolveExportValueWithOpts(exportValue, opts)
		if err != nil {
			continue
		}

		entries = append(entries, ExportEntry{
			Subpath: subpath,
			Target:  resolved,
		})
	}

	return entries
}

// WildcardExports returns all wildcard export patterns from the package.
// Pass nil for opts to use DefaultConditions.
func (pkg *PackageJSON) WildcardExports(opts *ResolveOptions) []WildcardExport {
	var wildcards []WildcardExport

	exportsMap, ok := pkg.Exports.(map[string]any)
	if !ok {
		return wildcards
	}

	for pattern, targetValue := range exportsMap {
		if !strings.Contains(pattern, "*") {
			continue
		}

		// Resolve the target value (handles strings, conditions, and arrays)
		targetStr := resolveWildcardTargetWithOpts(targetValue, opts)
		if targetStr == "" || !strings.Contains(targetStr, "*") {
			continue
		}

		// Extract the prefix before the wildcard
		target := trimDotSlash(targetStr)
		wildcardIdx := strings.Index(target, "*")
		targetPrefix := target[:wildcardIdx]

		wildcards = append(wildcards, WildcardExport{
			Pattern: pattern,
			Target:  targetPrefix,
		})
	}

	return wildcards
}

// resolveWildcardTargetWithOpts resolves a wildcard export value with custom conditions.
// Handles plain strings, conditional exports (maps), and fallback arrays.
func resolveWildcardTargetWithOpts(value any, opts *ResolveOptions) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		// Conditional export - try to resolve using configured conditions
		if result, err := resolveConditionsWithOpts(v, opts); err == nil {
			return result
		}
	case []any:
		// Fallback array - return first valid wildcard target
		for _, item := range v {
			if result := resolveWildcardTargetWithOpts(item, opts); result != "" {
				return result
			}
		}
	}
	return ""
}

// HasTrailingSlashExport returns true if the package should have a trailing slash import.
// Pass nil for opts to use DefaultConditions.
func (pkg *PackageJSON) HasTrailingSlashExport(opts *ResolveOptions) bool {
	if len(pkg.WildcardExports(opts)) > 0 {
		return true
	}
	if pkg.Exports == nil {
		return true
	}
	return false
}

// resolveExportValueWithOpts resolves an export value with custom conditions.
func resolveExportValueWithOpts(value any, opts *ResolveOptions) (string, error) {
	switch v := value.(type) {
	case string:
		return trimDotSlash(v), nil
	case map[string]any:
		return resolveConditionsWithOpts(v, opts)
	}
	return "", ErrNotExported
}

// resolveConditionsWithOpts resolves a conditional export map to a path.
// Tries each condition in opts.Conditions order, recursing into nested maps.
func resolveConditionsWithOpts(conditions map[string]any, opts *ResolveOptions) (string, error) {
	conditionList := DefaultConditions
	if opts != nil && len(opts.Conditions) > 0 {
		conditionList = opts.Conditions
	}

	for _, cond := range conditionList {
		if value, ok := conditions[cond]; ok {
			if valueMap, ok := value.(map[string]any); ok {
				if result, err := resolveConditionsWithOpts(valueMap, opts); err == nil {
					return result, nil
				}
			} else if valueStr, ok := value.(string); ok {
				return trimDotSlash(valueStr), nil
			}
		}
	}

	return "", ErrNotExported
}

// trimDotSlash removes a leading "./" from a path.
func trimDotSlash(path string) string {
	return strings.TrimPrefix(path, "./")
}
