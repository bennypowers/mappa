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

// Package fs provides filesystem abstractions for mappa.
package fs

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FileSystem provides an abstraction over filesystem operations.
// This interface is congruent with bennypowers.dev/cem/internal/platform.FileSystem
// to enable duck typing compatibility.
type FileSystem interface {
	// File operations
	WriteFile(name string, data []byte, perm fs.FileMode) error
	ReadFile(name string) ([]byte, error)
	Remove(name string) error

	// Directory operations
	MkdirAll(path string, perm fs.FileMode) error
	ReadDir(name string) ([]fs.DirEntry, error)
	TempDir() string

	// File system queries
	Stat(name string) (fs.FileInfo, error)
	Exists(path string) bool

	// fs.FS compatibility - allows use with fs.WalkDir
	Open(name string) (fs.File, error)
}

// SymlinkResolver is an optional capability for file systems that can resolve
// symbolic links. Callers that enforce containment should use it when
// available so a link cannot point outside an allowed root; file systems that
// do not implement it keep working with lexical containment.
type SymlinkResolver interface {
	EvalSymlinks(path string) (string, error)
}

// RootedFS is an optional capability for file systems that can confine
// operations to a directory tree. Callers should prefer it for reads of
// untrusted content so containment is enforced by the OS rather than by a
// check followed by a read.
type RootedFS interface {
	OpenRoot(name string) (*os.Root, error)
}

// AtomicWriter is an optional capability for file systems that can replace a
// file without exposing a truncated intermediate state.
type AtomicWriter interface {
	WriteFileAtomic(name string, data []byte, perm fs.FileMode) error
}

// OSFileSystem implements FileSystem using the standard os package.
type OSFileSystem struct{}

var (
	_ SymlinkResolver = (*OSFileSystem)(nil)
	_ RootedFS        = (*OSFileSystem)(nil)
	_ AtomicWriter    = (*OSFileSystem)(nil)
)

// NewOSFileSystem creates a new filesystem that uses the standard os package.
func NewOSFileSystem() *OSFileSystem {
	return &OSFileSystem{}
}

// WriteFile writes data to a file with the given permissions.
func (f *OSFileSystem) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(name, data, perm)
}

// ReadFile reads the entire contents of a file.
func (f *OSFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// Remove deletes the named file or empty directory.
func (f *OSFileSystem) Remove(name string) error {
	return os.Remove(name)
}

// MkdirAll creates a directory path and all parents that do not exist.
func (f *OSFileSystem) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

// TempDir returns the default directory for temporary files.
func (f *OSFileSystem) TempDir() string {
	return os.TempDir()
}

// Stat returns file information for the named file.
func (f *OSFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(name)
}

// Exists returns true if the path exists.
func (f *OSFileSystem) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadDir reads the named directory and returns its entries.
func (f *OSFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(name)
}

// Open opens the named file for reading.
func (f *OSFileSystem) Open(name string) (fs.File, error) {
	return os.Open(name)
}

// EvalSymlinks returns the path name after evaluating any symbolic links.
func (f *OSFileSystem) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

// OpenRoot opens name as a directory tree that confines subsequent operations.
func (f *OSFileSystem) OpenRoot(name string) (*os.Root, error) {
	return os.OpenRoot(name)
}

// WriteFileAtomic writes data to a temporary file in the target's directory
// and renames it over the target, so readers never observe a partial file.
// Existing destination permissions are preserved. A symlink destination is
// resolved so the link itself survives and its target is replaced.
func (f *OSFileSystem) WriteFileAtomic(name string, data []byte, perm fs.FileMode) error {
	target := name
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		target = resolved
	}
	mode := perm
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".mappa-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	renamed = true
	return nil
}
