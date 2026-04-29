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

package cdn

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mappacdn "bennypowers.dev/mappa/cdn"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/testutil"
)

// MockFetcher is a test implementation of the Fetcher interface.
type MockFetcher struct {
	responses    map[string][]byte
	errors       map[string]error
	delays       map[string]time.Duration
	fetchCount   atomic.Int64
	maxConcurrent atomic.Int64
	inflight      atomic.Int64
}

func NewMockFetcher() *MockFetcher {
	return &MockFetcher{
		responses: make(map[string][]byte),
		errors:    make(map[string]error),
		delays:    make(map[string]time.Duration),
	}
}

func (m *MockFetcher) AddResponse(url string, data []byte) {
	m.responses[url] = data
}

func (m *MockFetcher) AddError(url string, err error) {
	m.errors[url] = err
}

func (m *MockFetcher) AddDelay(url string, d time.Duration) {
	m.delays[url] = d
}

func (m *MockFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	m.fetchCount.Add(1)
	cur := m.inflight.Add(1)
	defer m.inflight.Add(-1)
	for {
		old := m.maxConcurrent.Load()
		if cur <= old || m.maxConcurrent.CompareAndSwap(old, cur) {
			break
		}
	}

	if d, ok := m.delays[url]; ok {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, &mappacdn.FetchError{URL: url, Message: ctx.Err().Error()}
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, &mappacdn.FetchError{URL: url, Message: err.Error()}
	}
	if err, ok := m.errors[url]; ok {
		return nil, err
	}
	if data, ok := m.responses[url]; ok {
		return data, nil
	}
	return nil, &mappacdn.FetchError{URL: url, StatusCode: 404, Message: "Not Found"}
}

func TestResolverResolvePackageJSON(t *testing.T) {
	mockFetcher := NewMockFetcher()

	// Load fixtures using testutil
	litRegistry := testutil.LoadFixtureFile(t, "lit-registry/response.json")
	litPackage := testutil.LoadFixtureFile(t, "lit-package/package.json")

	mockFetcher.AddResponse("https://registry.npmjs.org/lit", litRegistry)
	mockFetcher.AddResponse("https://esm.sh/lit@3.0.0/package.json", litPackage)

	resolver := New(mockFetcher).WithMaxDepth(1)
	ctx := context.Background()

	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{
			"lit": "^3.0.0",
		},
	}

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	// Check lit entry exists
	if im.Imports["lit"] == "" {
		t.Error("Expected 'lit' in imports")
	}
	if im.Imports["lit"] != "https://esm.sh/lit@3.0.0/index.js" {
		t.Errorf("Unexpected lit URL: %s", im.Imports["lit"])
	}

	// Check subpath export
	if im.Imports["lit/decorators.js"] == "" {
		t.Error("Expected 'lit/decorators.js' in imports")
	}
}

func TestResolverWithProvider(t *testing.T) {
	mockFetcher := NewMockFetcher()

	// Load fixtures for unpkg
	preactRegistry := testutil.LoadFixtureFile(t, "preact-registry/response.json")
	preactPackage := testutil.LoadFixtureFile(t, "preact-package/package.json")

	mockFetcher.AddResponse("https://registry.npmjs.org/preact", preactRegistry)
	mockFetcher.AddResponse("https://unpkg.com/preact@10.0.0/package.json", preactPackage)

	resolver := New(mockFetcher).
		WithProvider(mappacdn.Unpkg).
		WithMaxDepth(1)

	ctx := context.Background()
	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{
			"preact": "^10.0.0",
		},
	}

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	// Check unpkg URL
	expected := "https://unpkg.com/preact@10.0.0/dist/preact.mjs"
	if im.Imports["preact"] != expected {
		t.Errorf("Expected %s, got %s", expected, im.Imports["preact"])
	}
}

func TestResolverWithConditions(t *testing.T) {
	mockFetcher := NewMockFetcher()

	// Load fixtures
	testPkgRegistry := testutil.LoadFixtureFile(t, "test-pkg-registry/response.json")
	testPkgPackage := testutil.LoadFixtureFile(t, "test-pkg-package/package.json")

	mockFetcher.AddResponse("https://registry.npmjs.org/test-pkg", testPkgRegistry)
	mockFetcher.AddResponse("https://esm.sh/test-pkg@1.0.0/package.json", testPkgPackage)

	ctx := context.Background()
	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{
			"test-pkg": "^1.0.0",
		},
	}

	// Test with browser condition
	resolver := New(mockFetcher).
		WithConditions([]string{"browser", "import", "default"}).
		WithMaxDepth(1)

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	expected := "https://esm.sh/test-pkg@1.0.0/browser.js"
	if im.Imports["test-pkg"] != expected {
		t.Errorf("Expected %s, got %s", expected, im.Imports["test-pkg"])
	}
}

func TestResolverWithIncludeDev(t *testing.T) {
	mockFetcher := NewMockFetcher()

	// Load fixtures
	prodPkgRegistry := testutil.LoadFixtureFile(t, "prod-pkg-registry/response.json")
	prodPkgPackage := testutil.LoadFixtureFile(t, "prod-pkg-package/package.json")
	devPkgRegistry := testutil.LoadFixtureFile(t, "dev-pkg-registry/response.json")
	devPkgPackage := testutil.LoadFixtureFile(t, "dev-pkg-package/package.json")

	mockFetcher.AddResponse("https://registry.npmjs.org/prod-pkg", prodPkgRegistry)
	mockFetcher.AddResponse("https://esm.sh/prod-pkg@1.0.0/package.json", prodPkgPackage)
	mockFetcher.AddResponse("https://registry.npmjs.org/dev-pkg", devPkgRegistry)
	mockFetcher.AddResponse("https://esm.sh/dev-pkg@1.0.0/package.json", devPkgPackage)

	ctx := context.Background()
	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{
			"prod-pkg": "^1.0.0",
		},
		DevDependencies: map[string]string{
			"dev-pkg": "^1.0.0",
		},
	}

	// Without includeDev
	resolver := New(mockFetcher).WithMaxDepth(1)
	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	if im.Imports["prod-pkg"] == "" {
		t.Error("Expected prod-pkg in imports")
	}
	if im.Imports["dev-pkg"] != "" {
		t.Error("Expected dev-pkg NOT in imports without includeDev")
	}

	// With includeDev
	resolver = New(mockFetcher).WithIncludeDev(true).WithMaxDepth(1)
	im, err = resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON with includeDev error: %v", err)
	}

	if im.Imports["dev-pkg"] == "" {
		t.Error("Expected dev-pkg in imports with includeDev")
	}
}

func TestBuildPackageImports(t *testing.T) {
	mockFetcher := NewMockFetcher()
	resolver := New(mockFetcher)

	pkg := &packagejson.PackageJSON{
		Name:    "test",
		Version: "1.0.0",
		Exports: map[string]any{
			".":      "./index.js",
			"./util": "./util.js",
		},
	}

	imports := resolver.buildPackageImports("test", "1.0.0", pkg)

	if imports["test"] != "https://esm.sh/test@1.0.0/index.js" {
		t.Errorf("Unexpected main import: %s", imports["test"])
	}
	if imports["test/util"] != "https://esm.sh/test@1.0.0/util.js" {
		t.Errorf("Unexpected subpath import: %s", imports["test/util"])
	}
}

func TestResolverWithExclude(t *testing.T) {
	mockFetcher := NewMockFetcher()

	libARegistry := testutil.LoadFixtureFile(t, "lib-a-registry/response.json")
	libAPackage := testutil.LoadFixtureFile(t, "lib-a-package/package.json")
	libBRegistry := testutil.LoadFixtureFile(t, "lib-b-registry/response.json")
	libBPackage := testutil.LoadFixtureFile(t, "lib-b-package/package.json")

	mockFetcher.AddResponse("https://registry.npmjs.org/lib-a", libARegistry)
	mockFetcher.AddResponse("https://esm.sh/lib-a@1.0.0/package.json", libAPackage)
	mockFetcher.AddResponse("https://registry.npmjs.org/lib-b", libBRegistry)
	mockFetcher.AddResponse("https://esm.sh/lib-b@1.0.0/package.json", libBPackage)

	ctx := context.Background()
	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{
			"lib-a": "^1.0.0",
			"lib-b": "^1.0.0",
		},
	}

	t.Run("without exclude both present", func(t *testing.T) {
		resolver := New(mockFetcher)
		im, err := resolver.ResolvePackageJSON(ctx, pkg)
		if err != nil {
			t.Fatalf("ResolvePackageJSON error: %v", err)
		}
		if im.Imports["lib-a"] == "" {
			t.Error("Expected lib-a in imports")
		}
		if im.Imports["lib-b"] == "" {
			t.Error("Expected lib-b in imports")
		}
	})

	t.Run("transitive deps produce scopes", func(t *testing.T) {
		pkgOnlyA := &packagejson.PackageJSON{
			Dependencies: map[string]string{
				"lib-a": "^1.0.0",
			},
		}
		resolver := New(mockFetcher)
		im, err := resolver.ResolvePackageJSON(ctx, pkgOnlyA)
		if err != nil {
			t.Fatalf("ResolvePackageJSON error: %v", err)
		}
		found := false
		for _, scope := range im.Scopes {
			if _, ok := scope["lib-b"]; ok {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected lib-b in a scope as transitive dep of lib-a")
		}
	})

	t.Run("exclude direct dependency", func(t *testing.T) {
		resolver := New(mockFetcher).WithExclude([]string{"lib-b"})
		im, err := resolver.ResolvePackageJSON(ctx, pkg)
		if err != nil {
			t.Fatalf("ResolvePackageJSON error: %v", err)
		}
		if im.Imports["lib-a"] == "" {
			t.Error("Expected lib-a in imports")
		}
		for k := range im.Imports {
			if k == "lib-b" || strings.HasPrefix(k, "lib-b/") {
				t.Errorf("Expected lib-b excluded from imports, found key %q", k)
			}
		}
	})

	t.Run("exclude transitive dependency", func(t *testing.T) {
		pkgOnlyA := &packagejson.PackageJSON{
			Dependencies: map[string]string{
				"lib-a": "^1.0.0",
			},
		}
		resolver := New(mockFetcher).WithExclude([]string{"lib-b"})
		im, err := resolver.ResolvePackageJSON(ctx, pkgOnlyA)
		if err != nil {
			t.Fatalf("ResolvePackageJSON error: %v", err)
		}
		if im.Imports["lib-a"] == "" {
			t.Error("Expected lib-a in imports")
		}
		// lib-b should not appear in imports
		for k := range im.Imports {
			if k == "lib-b" || strings.HasPrefix(k, "lib-b/") {
				t.Errorf("Expected lib-b excluded from imports, found key %q", k)
			}
		}
		// lib-b should not appear in any scope
		for scopeKey, scope := range im.Scopes {
			for k := range scope {
				if k == "lib-b" || strings.HasPrefix(k, "lib-b/") {
					t.Errorf("Expected lib-b excluded from scope %q, found key %q", scopeKey, k)
				}
			}
		}
	})
}

func TestBuildPackageImportsMainFallback(t *testing.T) {
	mockFetcher := NewMockFetcher()
	resolver := New(mockFetcher)

	pkg := &packagejson.PackageJSON{
		Name:    "legacy",
		Version: "1.0.0",
		Main:    "lib/main.js",
	}

	imports := resolver.buildPackageImports("legacy", "1.0.0", pkg)

	if imports["legacy"] != "https://esm.sh/legacy@1.0.0/lib/main.js" {
		t.Errorf("Unexpected main fallback: %s", imports["legacy"])
	}
}

func TestSharedSemaphoreBoundsConcurrency(t *testing.T) {
	mockFetcher := NewMockFetcher()

	rootRegistry := testutil.LoadFixtureFile(t, "root-registry/response.json")
	rootPkg := testutil.LoadFixtureFile(t, "root-package/package.json")
	mockFetcher.AddResponse("https://registry.npmjs.org/root", rootRegistry)
	mockFetcher.AddResponse("https://esm.sh/root@1.0.0/package.json", rootPkg)

	for _, name := range []string{"dep-a", "dep-b", "dep-c"} {
		reg := testutil.LoadFixtureFile(t, name+"-registry/response.json")
		pkg := testutil.LoadFixtureFile(t, name+"-package/package.json")
		mockFetcher.AddResponse("https://registry.npmjs.org/"+name, reg)
		mockFetcher.AddResponse("https://esm.sh/"+name+"@1.0.0/package.json", pkg)
		mockFetcher.AddDelay("https://registry.npmjs.org/"+name, 10*time.Millisecond)
		mockFetcher.AddDelay("https://esm.sh/"+name+"@1.0.0/package.json", 10*time.Millisecond)
	}

	resolver := New(mockFetcher).WithConcurrency(2)
	ctx := context.Background()

	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{"root": "^1.0.0"},
	}

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	if im.Imports["root"] == "" {
		t.Error("Expected 'root' in imports")
	}

	if max := mockFetcher.maxConcurrent.Load(); max > 2 {
		t.Errorf("Expected max concurrent <= 2, got %d", max)
	}
}

func TestSharedSemaphoreNoDeadlockMultiDepth(t *testing.T) {
	// Regression test: with per-level semaphores, concurrency=2 with 2 direct
	// deps each having transitive deps would deadlock. Both parents hold slots
	// while children wait for slots.
	// Tree: deep-a -> subdep-a1, deep-b -> subdep-b1
	mockFetcher := NewMockFetcher()

	for _, name := range []string{"deep-a", "deep-b", "subdep-a1", "subdep-b1"} {
		reg := testutil.LoadFixtureFile(t, name+"-registry/response.json")
		pkg := testutil.LoadFixtureFile(t, name+"-package/package.json")
		mockFetcher.AddResponse("https://registry.npmjs.org/"+name, reg)
		mockFetcher.AddResponse("https://esm.sh/"+name+"@1.0.0/package.json", pkg)
		mockFetcher.AddDelay("https://registry.npmjs.org/"+name, 5*time.Millisecond)
		mockFetcher.AddDelay("https://esm.sh/"+name+"@1.0.0/package.json", 5*time.Millisecond)
	}

	resolver := New(mockFetcher).WithConcurrency(2)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{
			"deep-a": "^1.0.0",
			"deep-b": "^1.0.0",
		},
	}

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error (possible deadlock): %v", err)
	}

	if im.Imports["deep-a"] == "" {
		t.Error("Expected 'deep-a' in imports")
	}
	if im.Imports["deep-b"] == "" {
		t.Error("Expected 'deep-b' in imports")
	}
}

func TestRequestTimeoutCancelsSlowFetch(t *testing.T) {
	mockFetcher := NewMockFetcher()

	slowRegistry := testutil.LoadFixtureFile(t, "slow-pkg-registry/response.json")
	slowPkg := testutil.LoadFixtureFile(t, "slow-pkg-package/package.json")
	mockFetcher.AddResponse("https://registry.npmjs.org/slow-pkg", slowRegistry)
	mockFetcher.AddResponse("https://esm.sh/slow-pkg@1.0.0/package.json", slowPkg)
	mockFetcher.AddDelay("https://esm.sh/slow-pkg@1.0.0/package.json", 500*time.Millisecond)

	resolver := New(mockFetcher).
		WithRequestTimeout(50 * time.Millisecond).
		WithMaxDepth(1)

	ctx := context.Background()
	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{"slow-pkg": "^1.0.0"},
	}

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	if im.Imports["slow-pkg"] != "" {
		t.Error("Expected slow-pkg to be missing from imports due to timeout")
	}
}

func TestRequestTimeoutDefaultIsNoTimeout(t *testing.T) {
	mockFetcher := NewMockFetcher()

	litRegistry := testutil.LoadFixtureFile(t, "lit-registry/response.json")
	litPackage := testutil.LoadFixtureFile(t, "lit-package/package.json")

	mockFetcher.AddResponse("https://registry.npmjs.org/lit", litRegistry)
	mockFetcher.AddResponse("https://esm.sh/lit@3.0.0/package.json", litPackage)

	resolver := New(mockFetcher).WithMaxDepth(1)
	ctx := context.Background()

	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{"lit": "^3.0.0"},
	}

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	if im.Imports["lit"] == "" {
		t.Error("Expected 'lit' in imports")
	}
}

func TestContextCancellationStopsSemAcquisition(t *testing.T) {
	mockFetcher := NewMockFetcher()

	for _, name := range []string{"dep-a", "dep-b", "dep-c"} {
		reg := testutil.LoadFixtureFile(t, name+"-registry/response.json")
		pkg := testutil.LoadFixtureFile(t, name+"-package/package.json")
		mockFetcher.AddResponse("https://registry.npmjs.org/"+name, reg)
		mockFetcher.AddResponse("https://esm.sh/"+name+"@1.0.0/package.json", pkg)
		mockFetcher.AddDelay("https://registry.npmjs.org/"+name, 100*time.Millisecond)
	}

	resolver := New(mockFetcher).WithConcurrency(1).WithMaxDepth(1)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	pkg := &packagejson.PackageJSON{
		Dependencies: map[string]string{
			"dep-a": "^1.0.0",
			"dep-b": "^1.0.0",
			"dep-c": "^1.0.0",
		},
	}

	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	// With concurrency=1 and 100ms delay per dep, 50ms timeout should prevent
	// resolving all 3 deps
	resolved := 0
	for _, name := range []string{"dep-a", "dep-b", "dep-c"} {
		if im.Imports[name] != "" {
			resolved++
		}
	}
	if resolved == 3 {
		t.Error("Expected context cancellation to prevent resolving all deps")
	}
}

func TestWithConcurrencyDefault(t *testing.T) {
	mockFetcher := NewMockFetcher()
	resolver := New(mockFetcher)
	if resolver.concurrency != 10 {
		t.Errorf("Expected default concurrency 10, got %d", resolver.concurrency)
	}
}

func TestWithConcurrencyIgnoresZero(t *testing.T) {
	mockFetcher := NewMockFetcher()
	resolver := New(mockFetcher).WithConcurrency(0)
	if resolver.concurrency != 10 {
		t.Errorf("Expected concurrency to remain 10 for zero input, got %d", resolver.concurrency)
	}
}
