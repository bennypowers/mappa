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
package fs_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"bennypowers.dev/mappa/fs"
)

// These tests exercise the OS-backed implementation, which needs a real
// filesystem; MapFileSystem cannot model roots or symlinks.

func TestOSFileSystemOpenRootConfinesPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("in"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("out"), 0o644); err != nil {
		t.Fatal(err)
	}

	osfs := fs.NewOSFileSystem()
	r, err := osfs.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	if _, err := r.ReadFile("inside.txt"); err != nil {
		t.Errorf("in-root read failed: %v", err)
	}
	if _, err := r.ReadFile(filepath.Join("..", filepath.Base(outside), "secret.txt")); err == nil {
		t.Error("rooted read escaped its root")
	}
}

func TestOSFileSystemWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "out.html")
	if err := os.WriteFile(name, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	osfs := fs.NewOSFileSystem()
	if err := osfs.WriteFileAtomic(name, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only the target file, got %d entries: %v", len(entries), entries)
	}
}

func TestOSFileSystemEvalSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	got, err := fs.NewOSFileSystem().EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("EvalSymlinks(link) = %q, want %q", got, want)
	}
}
