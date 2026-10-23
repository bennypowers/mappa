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
	iofs "io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bennypowers.dev/mappa/internal/mapfs"
	"bennypowers.dev/mappa/testutil"
)

func TestAllowPathBoundary(t *testing.T) {
	tracer := NewTracer(nil, "/ws/packages/app").
		WithNodeModules("/ws/node_modules").
		WithAllowedPaths([]string{"/ws/packages/lib"})

	allowed := []string{
		"/ws/packages/app/index.js",
		"/ws/node_modules/lit/index.js",
		"/ws/packages/lib/index.js",
	}
	for _, p := range allowed {
		if !tracer.allowPath(p) {
			t.Errorf("expected %q to be allowed", p)
		}
	}

	rejected := []string{
		"/ws/other/secret.js",
		"/ws/packages/app/../other/secret.js",
		"/ws/packages/lib2/index.js",
	}
	for _, p := range rejected {
		if tracer.allowPath(p) {
			t.Errorf("expected %q to be rejected", p)
		}
	}
}

type fakeFileInfo struct {
	name string
	size int64
}

func (f fakeFileInfo) Name() string        { return f.name }
func (f fakeFileInfo) Size() int64         { return f.size }
func (f fakeFileInfo) Mode() iofs.FileMode { return 0o644 }
func (f fakeFileInfo) ModTime() time.Time  { return time.Time{} }
func (f fakeFileInfo) IsDir() bool         { return false }
func (f fakeFileInfo) Sys() any            { return nil }

// oversizedFS reports a synthetic size for one path.
type oversizedFS struct {
	*mapfs.MapFileSystem
	target string
	size   int64
}

func (f *oversizedFS) Stat(name string) (iofs.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(f.target) {
		return fakeFileInfo{name: filepath.Base(name), size: f.size}, nil
	}
	return f.MapFileSystem.Stat(name)
}

func TestTraceModuleRejectsOversizedModule(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/graph", "/test")
	target := "/test/components/nav.js"
	osfs := &oversizedFS{MapFileSystem: mfs, target: target, size: maxModuleSize + 1}

	_, err := NewTracer(osfs, "/test").TraceModule(target)
	if err == nil {
		t.Fatal("expected an oversized-module error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTraceHTMLRejectsOversizedHTML(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "trace/graph", "/test")
	target := "/test/index.html"
	osfs := &oversizedFS{MapFileSystem: mfs, target: target, size: maxHTMLSize + 1}

	_, err := NewTracer(osfs, "/test").TraceHTML(target)
	if err == nil {
		t.Fatal("expected an oversized-HTML error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("unexpected error: %v", err)
	}
}
