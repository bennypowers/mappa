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
// Package resolve provides interfaces and utilities for import map resolution.
package resolve

import (
	"path/filepath"
	"strings"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
)

// Resolver generates import map entries for packages.
type Resolver interface {
	// Resolve generates an ImportMap for a project rooted at the given directory.
	Resolve(rootDir string) (*importmap.ImportMap, error)
}

// Logger is an interface for logging messages during resolution.
type Logger interface {
	Warning(format string, args ...any)
	Debug(format string, args ...any)
}

// WorkspacePackage represents a package in a monorepo workspace.
type WorkspacePackage struct {
	Name string // Package name from package.json
	Path string // Absolute path to package directory
}

// FindWorkspaceRoot walks up the directory tree to find the workspace root.
// Returns the directory containing node_modules, workspace configuration, or .git.
func FindWorkspaceRoot(fs fs.FileSystem, startDir string) string {
	dir := startDir
	for {
		// Check if node_modules exists in this directory
		nodeModulesPath := filepath.Join(dir, "node_modules")
		if stat, err := fs.Stat(nodeModulesPath); err == nil && stat.IsDir() {
			return dir
		}

		// Check if there's a package.json with workspaces field
		pkgPath := filepath.Join(dir, "package.json")
		if pkg, err := packagejson.ParseFile(fs, pkgPath); err == nil && len(pkg.Workspaces) > 0 {
			return dir
		}

		// Check for .git directory (repository root is a reasonable workspace root)
		gitDir := filepath.Join(dir, ".git")
		if stat, err := fs.Stat(gitDir); err == nil && stat.IsDir() {
			return dir
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			return startDir
		}
		dir = parent
	}
}

// ToWebPath converts a filesystem path relative to rootDir into a web path.
// e.g., "node_modules/lit" -> "/node_modules/lit"
func ToWebPath(rootDir, fullPath string) string {
	relPath, err := filepath.Rel(rootDir, fullPath)
	if err != nil {
		return ""
	}
	if relPath == "." {
		return ""
	}
	if strings.HasPrefix(relPath, "..") {
		return ""
	}
	return "/" + filepath.ToSlash(relPath)
}