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
