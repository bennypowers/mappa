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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"bennypowers.dev/mappa/fs"
)

// symlinkFS is a FileSystem stand-in whose EvalSymlinks is driven by a table,
// so containment can be tested without touching the real filesystem.
type symlinkFS struct {
	fs.FileSystem
	links map[string]string
}

func (f *symlinkFS) EvalSymlinks(path string) (string, error) {
	if target, ok := f.links[path]; ok {
		return target, nil
	}
	return path, nil
}

func TestAllowPathRejectsSymlinkEscape(t *testing.T) {
	tracer := NewTracer(&symlinkFS{links: map[string]string{
		"/ws/node_modules/evil.js": "/etc/passwd",
		"/ws/node_modules/loop.js": "/ws/outside/loop.js",
		"/ws/node_modules/ok.js":   "/ws/node_modules/pkg/index.js",
	}}, "/ws/packages/app").
		WithNodeModules("/ws/node_modules")

	tests := []struct {
		path string
		want bool
	}{
		{"/ws/node_modules/evil.js", false},
		{"/ws/node_modules/loop.js", false},
		{"/ws/node_modules/ok.js", true},
		{"/ws/packages/app/main.js", true},
	}
	for _, tt := range tests {
		if got := tracer.allowPath(tt.path); got != tt.want {
			t.Errorf("allowPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestReadFileThroughWorkspaceSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	ws := t.TempDir()
	pkg := filepath.Join(ws, "packages", "lib")
	if err := os.MkdirAll(filepath.Join(ws, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "index.js"), []byte("export {};"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pkg, filepath.Join(ws, "node_modules", "lib")); err != nil {
		t.Fatal(err)
	}

	tracer := NewTracer(fs.NewOSFileSystem(), ws).
		WithNodeModules(filepath.Join(ws, "node_modules")).
		WithAllowedPaths([]string{pkg})

	if _, err := tracer.readFile(filepath.Join(ws, "node_modules", "lib", "index.js")); err != nil {
		t.Fatalf("read through workspace symlink failed: %v", err)
	}
}

func TestReadFileRejectsEscapeThroughRootedFS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	modules := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.js")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(modules, "escape.js")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(modules, "ok.js")
	if err := os.WriteFile(inside, []byte("export {};"), 0o644); err != nil {
		t.Fatal(err)
	}

	tracer := NewTracer(fs.NewOSFileSystem(), root).WithNodeModules(modules)
	if _, err := tracer.readFile(link); err == nil {
		t.Fatal("rooted read followed a symlink outside its root")
	}
	if _, err := tracer.readFile(inside); err != nil {
		t.Fatalf("rooted read of an in-root file failed: %v", err)
	}
}

func TestAllowPathFallsBackWhenSymlinksUnsupported(t *testing.T) {
	// A FileSystem without SymlinkResolver keeps lexical containment.
	tracer := NewTracer(nil, "/ws/packages/app").WithNodeModules("/ws/node_modules")
	if !tracer.allowPath("/ws/node_modules/lit/index.js") {
		t.Fatal("expected lexical containment to allow a path under node_modules")
	}
}

func TestAllowPathRejectsRealSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	// MapFileSystem cannot model symlinks, so this one test uses a real
	// temporary directory.
	root := t.TempDir()
	outside := t.TempDir()
	modules := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(modules, "escape.js")
	secret := filepath.Join(outside, "secret.js")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	tracer := NewTracer(fs.NewOSFileSystem(), root).WithNodeModules(modules)
	if tracer.allowPath(link) {
		t.Fatal("allowPath followed a symlink outside the allowed roots")
	}
}
