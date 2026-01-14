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
package resolve_test

import (
	"testing"

	"bennypowers.dev/mappa/internal/mapfs"
	"bennypowers.dev/mappa/resolve"
)

func TestFindWorkspaceRoot(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mfs *mapfs.MapFileSystem)
		startDir string
		expected string
	}{
		{
			name: "root with node_modules",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddDir("/root/node_modules", 0755)
				mfs.AddDir("/root/packages/pkg1", 0755)
			},
			startDir: "/root/packages/pkg1",
			expected: "/root",
		},
		{
			name: "root with package.json workspaces",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddFile("/root/package.json", `{"workspaces": ["packages/*"]}`, 0644)
				mfs.AddDir("/root/packages/pkg1", 0755)
			},
			startDir: "/root/packages/pkg1",
			expected: "/root",
		},
		{
			name: "root with .git",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddDir("/root/.git", 0755)
				mfs.AddDir("/root/packages/pkg1", 0755)
			},
			startDir: "/root/packages/pkg1",
			expected: "/root",
		},
		{
			name: "nested node_modules",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddDir("/root/node_modules", 0755)
				mfs.AddDir("/root/packages/pkg1/node_modules", 0755)
			},
			startDir: "/root/packages/pkg1",
			expected: "/root/packages/pkg1", // Should find the closest one
		},
		{
			name: "no root found",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddDir("/root/packages/pkg1", 0755)
			},
			startDir: "/root/packages/pkg1",
			expected: "/root/packages/pkg1", // Stops at root directory
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mapfs.New()
			if tt.setup != nil {
				tt.setup(mfs)
			}

			result := resolve.FindWorkspaceRoot(mfs, tt.startDir)
			if result != tt.expected {
				t.Errorf("FindWorkspaceRoot() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestToWebPath(t *testing.T) {
	tests := []struct {
		rootDir  string
		fullPath string
		expected string
	}{
		{"/app", "/app/node_modules/lit/index.js", "/node_modules/lit/index.js"},
		{"/app", "/app/src/main.js", "/src/main.js"},
		{"/app", "/other/file.js", ""}, // Outside root
		{"/app", "/app", ""},           // Same as root
	}

	for _, tt := range tests {
		result := resolve.ToWebPath(tt.rootDir, tt.fullPath)
		if result != tt.expected {
			t.Errorf("ToWebPath(%q, %q) = %q, want %q", tt.rootDir, tt.fullPath, result, tt.expected)
		}
	}
}

func TestDiscoverWorkspacePackages(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mfs *mapfs.MapFileSystem)
		rootDir  string
		expected []resolve.WorkspacePackage
		wantErr  bool
	}{
		{
			name: "simple packages/* pattern",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddFile("/root/package.json", `{"workspaces": ["packages/*"]}`, 0644)
				mfs.AddDir("/root/packages/core", 0755)
				mfs.AddFile("/root/packages/core/package.json", `{"name": "@myorg/core"}`, 0644)
				mfs.AddDir("/root/packages/utils", 0755)
				mfs.AddFile("/root/packages/utils/package.json", `{"name": "@myorg/utils"}`, 0644)
			},
			rootDir: "/root",
			expected: []resolve.WorkspacePackage{
				{Name: "@myorg/core", Path: "/root/packages/core"},
				{Name: "@myorg/utils", Path: "/root/packages/utils"},
			},
		},
		{
			name: "object format workspaces",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddFile("/root/package.json", `{"workspaces": {"packages": ["libs/*"]}}`, 0644)
				mfs.AddDir("/root/libs/common", 0755)
				mfs.AddFile("/root/libs/common/package.json", `{"name": "common"}`, 0644)
			},
			rootDir: "/root",
			expected: []resolve.WorkspacePackage{
				{Name: "common", Path: "/root/libs/common"},
			},
		},
		{
			name: "scoped package pattern @scope/*",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddFile("/root/package.json", `{"workspaces": ["@myorg/*"]}`, 0644)
				mfs.AddDir("/root/@myorg/core", 0755)
				mfs.AddFile("/root/@myorg/core/package.json", `{"name": "@myorg/core"}`, 0644)
				mfs.AddDir("/root/@myorg/utils", 0755)
				mfs.AddFile("/root/@myorg/utils/package.json", `{"name": "@myorg/utils"}`, 0644)
			},
			rootDir: "/root",
			expected: []resolve.WorkspacePackage{
				{Name: "@myorg/core", Path: "/root/@myorg/core"},
				{Name: "@myorg/utils", Path: "/root/@myorg/utils"},
			},
		},
		{
			name: "no workspaces field",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddFile("/root/package.json", `{"name": "root"}`, 0644)
			},
			rootDir:  "/root",
			expected: nil,
		},
		{
			name: "skip directories without package.json",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddFile("/root/package.json", `{"workspaces": ["packages/*"]}`, 0644)
				mfs.AddDir("/root/packages/valid", 0755)
				mfs.AddFile("/root/packages/valid/package.json", `{"name": "valid"}`, 0644)
				mfs.AddDir("/root/packages/invalid", 0755) // No package.json
			},
			rootDir: "/root",
			expected: []resolve.WorkspacePackage{
				{Name: "valid", Path: "/root/packages/valid"},
			},
		},
		{
			name: "multiple patterns",
			setup: func(mfs *mapfs.MapFileSystem) {
				mfs.AddFile("/root/package.json", `{"workspaces": ["packages/*", "apps/*"]}`, 0644)
				mfs.AddDir("/root/packages/lib", 0755)
				mfs.AddFile("/root/packages/lib/package.json", `{"name": "lib"}`, 0644)
				mfs.AddDir("/root/apps/web", 0755)
				mfs.AddFile("/root/apps/web/package.json", `{"name": "web"}`, 0644)
			},
			rootDir: "/root",
			expected: []resolve.WorkspacePackage{
				{Name: "lib", Path: "/root/packages/lib"},
				{Name: "web", Path: "/root/apps/web"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mapfs.New()
			if tt.setup != nil {
				tt.setup(mfs)
			}

			packages, err := resolve.DiscoverWorkspacePackages(mfs, tt.rootDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverWorkspacePackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(packages) != len(tt.expected) {
				t.Errorf("DiscoverWorkspacePackages() returned %d packages, want %d", len(packages), len(tt.expected))
				return
			}

			// Build a map for easier comparison (order may vary)
			expectedMap := make(map[string]string)
			for _, pkg := range tt.expected {
				expectedMap[pkg.Name] = pkg.Path
			}

			for _, pkg := range packages {
				expectedPath, ok := expectedMap[pkg.Name]
				if !ok {
					t.Errorf("Unexpected package %q", pkg.Name)
					continue
				}
				if pkg.Path != expectedPath {
					t.Errorf("Package %q path = %q, want %q", pkg.Name, pkg.Path, expectedPath)
				}
			}
		})
	}
}
