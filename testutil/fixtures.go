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
// Package testutil provides testing utilities for the importmaps library.
package testutil

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"bennypowers.dev/mappa/internal/mapfs"
)

// updateGolden enables updating golden files with actual output when -update flag is set.
var updateGolden = flag.Bool("update", false, "update golden files with actual output")

// NewFixtureFS loads fixture files from testdata and returns a MapFileSystem
// with files mapped to the specified root path.
// The fixtureDir should be relative to the testdata directory.
func NewFixtureFS(t *testing.T, fixtureDir string, rootPath string) *mapfs.MapFileSystem {
	t.Helper()

	mfs := mapfs.New()

	// Try multiple possible paths since Go test changes working directory
	// based on which package is being tested.
	possiblePaths := []string{
		filepath.Join("testdata", fixtureDir),
		filepath.Join("..", "testdata", fixtureDir),
		filepath.Join("..", "..", "testdata", fixtureDir),
	}

	var fixturePath string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			fixturePath = path
			break
		}
	}
	if fixturePath == "" {
		t.Fatalf("Could not find fixtures at %s (tried all paths)", fixtureDir)
	}

	// Walk fixture directory and load all files into memory
	err := filepath.WalkDir(fixturePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(fixturePath, path)
		if err != nil {
			return err
		}

		virtualPath := filepath.Join(rootPath, relPath)
		mfs.AddFile(virtualPath, string(content), 0644)

		return nil
	})

	if err != nil {
		t.Fatalf("Failed to load fixtures from %s: %v", fixtureDir, err)
	}

	return mfs
}

// LoadFixtureFile reads a single fixture file and returns its content.
// The fixturePath should be relative to testdata/.
func LoadFixtureFile(t *testing.T, fixturePath string) []byte {
	t.Helper()

	possiblePaths := []string{
		filepath.Join("testdata", fixturePath),
		filepath.Join("..", "testdata", fixturePath),
		filepath.Join("..", "..", "testdata", fixturePath),
	}

	var content []byte
	var err error
	for _, path := range possiblePaths {
		content, err = os.ReadFile(path)
		if err == nil {
			return content
		}
	}
	t.Fatalf("Failed to read fixture %s (tried all paths): %v", fixturePath, err)
	return nil
}

// LoadGoldenFile reads a golden file (expected output) from testdata.
// If the -update flag is set, returns nil so the caller can write actual output.
func LoadGoldenFile(t *testing.T, goldenPath string) []byte {
	t.Helper()
	if *updateGolden {
		return nil
	}
	return LoadFixtureFile(t, goldenPath)
}

// UpdateGoldenFile writes actual output to the golden file when -update flag is set.
// No-ops when -update is not set. Creates parent directories as needed.
func UpdateGoldenFile(t *testing.T, goldenPath string, actual []byte) {
	t.Helper()
	if !*updateGolden {
		return
	}

	// Try multiple possible paths
	possiblePaths := []string{
		filepath.Join("testdata", goldenPath),
		filepath.Join("..", "testdata", goldenPath),
		filepath.Join("..", "..", "testdata", goldenPath),
	}

	// Use the first path that has an existing parent directory
	var targetPath string
	for _, path := range possiblePaths {
		parentDir := filepath.Dir(path)
		if _, err := os.Stat(parentDir); err == nil {
			targetPath = path
			break
		}
	}
	if targetPath == "" {
		// Default to first path and create directories
		targetPath = possiblePaths[0]
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		t.Fatalf("Failed to create directory for golden file %s: %v", goldenPath, err)
	}

	// Write the golden file
	if err := os.WriteFile(targetPath, actual, 0644); err != nil {
		t.Fatalf("Failed to write golden file %s: %v", goldenPath, err)
	}

	t.Logf("Updated golden file: %s", targetPath)
}
