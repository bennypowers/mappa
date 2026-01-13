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
package resolve

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Template represents a URL template with variable placeholders.
// Supported variables:
//   - {package} - Full package name (e.g., "@scope/name" or "name")
//   - {name} - Package name without scope
//   - {scope} - Scope without @ prefix (empty for unscoped)
//   - {version} - Resolved version (for CDN resolution)
//   - {path} - Relative path within the package
type Template struct {
	pattern   string
	variables []string
}

var variablePattern = regexp.MustCompile(`\{(\w+)\}`)

// ParseTemplate parses a URL template pattern.
func ParseTemplate(pattern string) (*Template, error) {
	if pattern == "" {
		return nil, fmt.Errorf("template pattern cannot be empty")
	}

	matches := variablePattern.FindAllStringSubmatch(pattern, -1)
	var variables []string
	for _, match := range matches {
		variables = append(variables, match[1])
	}

	// Validate variables
	validVars := map[string]bool{
		"package": true,
		"name":    true,
		"scope":   true,
		"version": true,
		"path":    true,
	}
	for _, v := range variables {
		if !validVars[v] {
			return nil, fmt.Errorf("unknown template variable: {%s}", v)
		}
	}

	return &Template{
		pattern:   pattern,
		variables: variables,
	}, nil
}

// Expand substitutes variables in the template with actual values.
func (t *Template) Expand(pkg, version, path string) string {
	name, scope := SplitPackageName(pkg)

	result := t.pattern
	result = strings.ReplaceAll(result, "{package}", pkg)
	result = strings.ReplaceAll(result, "{name}", name)
	result = strings.ReplaceAll(result, "{scope}", scope)
	result = strings.ReplaceAll(result, "{version}", version)
	result = strings.ReplaceAll(result, "{path}", path)

	return result
}

// Pattern returns the original template pattern.
func (t *Template) Pattern() string {
	return t.pattern
}

// Variables returns the list of variables used in the template.
func (t *Template) Variables() []string {
	return t.variables
}

// HasVersion returns true if the template contains a {version} variable.
// Templates with version variables require lockfile resolution (CDN mode).
func (t *Template) HasVersion() bool {
	return slices.Contains(t.variables, "version")
}

// SplitPackageName splits a package name into name and scope.
// For "@scope/name" returns ("name", "scope").
// For "name" returns ("name", "").
func SplitPackageName(pkg string) (name, scope string) {
	if strings.HasPrefix(pkg, "@") {
		parts := strings.SplitN(pkg, "/", 2)
		if len(parts) == 2 {
			return parts[1], strings.TrimPrefix(parts[0], "@")
		}
		return pkg, ""
	}
	return pkg, ""
}

// DefaultLocalTemplate is the default template for local resolution.
const DefaultLocalTemplate = "/node_modules/{package}/{path}"
