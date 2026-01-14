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
	"path/filepath"
	"strings"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/packagejson"
)

// DiscoverWorkspacePackages finds all workspace packages based on the
// workspaces field in the root package.json.
// Returns nil if no workspaces are defined.
func DiscoverWorkspacePackages(fsys fs.FileSystem, rootDir string) ([]WorkspacePackage, error) {
	rootPkgPath := filepath.Join(rootDir, "package.json")
	rootPkg, err := packagejson.ParseFile(fsys, rootPkgPath)
	if err != nil {
		return nil, err
	}

	patterns := rootPkg.WorkspacePatterns()
	if len(patterns) == 0 {
		return nil, nil
	}

	var packages []WorkspacePackage

	for _, pattern := range patterns {
		dirs, err := expandWorkspacePattern(fsys, rootDir, pattern)
		if err != nil {
			continue // skip patterns that can't be expanded
		}

		for _, dir := range dirs {
			pkg, err := parseWorkspacePackage(fsys, dir)
			if err != nil {
				continue // skip directories without valid package.json
			}
			packages = append(packages, pkg)
		}
	}

	return packages, nil
}

// expandWorkspacePattern expands a workspace glob pattern to matching directories.
// Supports patterns like "packages/*", "@scope/*", "libs/*/".
func expandWorkspacePattern(fsys fs.FileSystem, rootDir, pattern string) ([]string, error) {
	// Normalize pattern: remove trailing slash
	pattern = strings.TrimSuffix(pattern, "/")

	// Handle patterns with single-level wildcard at the end
	if strings.HasSuffix(pattern, "/*") {
		baseDir := strings.TrimSuffix(pattern, "/*")
		fullBase := filepath.Join(rootDir, baseDir)

		entries, err := fsys.ReadDir(fullBase)
		if err != nil {
			return nil, err
		}

		var dirs []string
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, filepath.Join(fullBase, entry.Name()))
			}
		}
		return dirs, nil
	}

	// Handle literal directory (no wildcard)
	if !strings.Contains(pattern, "*") {
		fullPath := filepath.Join(rootDir, pattern)
		if fsys.Exists(fullPath) {
			return []string{fullPath}, nil
		}
		return nil, nil
	}

	// Complex patterns with wildcards in the middle are not supported
	// Could be enhanced in the future
	return nil, nil
}

// parseWorkspacePackage reads a package.json from a directory and returns
// a WorkspacePackage with its name and path.
func parseWorkspacePackage(fsys fs.FileSystem, dir string) (WorkspacePackage, error) {
	pkgPath := filepath.Join(dir, "package.json")
	pkg, err := packagejson.ParseFile(fsys, pkgPath)
	if err != nil {
		return WorkspacePackage{}, err
	}

	if pkg.Name == "" {
		return WorkspacePackage{}, fmt.Errorf("package at %s has no name", dir)
	}

	return WorkspacePackage{
		Name: pkg.Name,
		Path: dir,
	}, nil
}
