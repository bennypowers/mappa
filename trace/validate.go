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
	"path/filepath"
	"sort"
	"strings"

	"bennypowers.dev/mappa/fs"
)

// IssueType classifies the type of import issue.
type IssueType int

const (
	// TransitiveDep indicates the package is in node_modules but not in dependencies.
	TransitiveDep IssueType = iota
	// DevDep indicates the package is a devDependency.
	DevDep
	// NotInstalled indicates the package is not found in node_modules.
	NotInstalled
)

// String returns a human-readable description of the issue type.
func (t IssueType) String() string {
	switch t {
	case TransitiveDep:
		return "transitive dependency"
	case DevDep:
		return "devDependency"
	case NotInstalled:
		return "not installed"
	default:
		return "unknown"
	}
}

// ImportIssue represents a problem with an import statement.
type ImportIssue struct {
	File       string    // Module path where import was found
	Line       int       // Line number of the import specifier
	Specifier  string    // The full import specifier
	Package    string    // Extracted package name
	IssueType  IssueType // Type of issue
	Suggestion string    // Alternative import if available (future)
}

// ValidateImports checks all bare specifier imports in the graph against the
// provided dependencies. It returns issues for imports from transitive dependencies,
// devDependencies, or packages that are not installed.
// The rootPkgName parameter is used to skip validation for self-referencing imports
// (when a package imports itself).
func (g *ModuleGraph) ValidateImports(
	fsys fs.FileSystem,
	rootDir string,
	rootPkgName string,
	deps map[string]string,
	devDeps map[string]string,
) []ImportIssue {
	var issues []ImportIssue

	// Sort module paths for deterministic output
	paths := make([]string, 0, len(g.Modules))
	for p := range g.Modules {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		mod := g.Modules[p]

		// Skip validation for modules inside node_modules - these are external
		// dependencies and their imports are their own concern. We only validate
		// imports from the project's own source files.
		if strings.Contains(mod.Path, "/node_modules/") {
			continue
		}

		for _, imp := range mod.Imports {
			if !isBareSpecifier(imp.Specifier) {
				continue
			}

			pkgName := getPackageName(imp.Specifier)

			// Skip self-referencing imports (package importing itself)
			if pkgName == rootPkgName {
				continue
			}

			// Check if it's a direct dependency - valid, skip
			if _, ok := deps[pkgName]; ok {
				continue
			}

			issue := ImportIssue{
				File:      mod.Path,
				Line:      imp.Line,
				Specifier: imp.Specifier,
				Package:   pkgName,
			}

			// Check if it's a devDependency
			if _, ok := devDeps[pkgName]; ok {
				issue.IssueType = DevDep
				issues = append(issues, issue)
				continue
			}

			// Check if it exists in node_modules (transitive dependency)
			pkgPath := filepath.Join(rootDir, "node_modules", pkgName)
			if fsys.Exists(pkgPath) {
				issue.IssueType = TransitiveDep
				issues = append(issues, issue)
				continue
			}

			// Not installed at all
			issue.IssueType = NotInstalled
			issues = append(issues, issue)
		}
	}

	return issues
}
