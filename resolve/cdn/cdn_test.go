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
	"encoding/json"
	"fmt"
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
	responses     map[string][]byte
	errors        map[string]error
	delays        map[string]time.Duration
	fetchCount    atomic.Int64
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
	if err == nil {
		t.Fatal("Expected timeout to be reported as an error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected deadline error, got: %v", err)
	}
	if im.Imports["slow-pkg"] != "" {
		t.Error("Expected slow-pkg to be missing from imports due to timeout")
	}
}

func TestDefaultRequestTimeoutAllowsFastFetch(t *testing.T) {
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
	if err == nil {
		t.Fatal("Expected context cancellation to be reported as an error")
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

func TestTransitiveDepsOnlyInScopes(t *testing.T) {
	mockFetcher := NewMockFetcher()
	libARegistry := testutil.LoadFixtureFile(t, "lib-a-registry/response.json")
	libAPackage := testutil.LoadFixtureFile(t, "lib-a-package/package.json")
	libBRegistry := testutil.LoadFixtureFile(t, "lib-b-registry/response.json")
	libBPackage := testutil.LoadFixtureFile(t, "lib-b-package/package.json")
	mockFetcher.AddResponse("https://registry.npmjs.org/lib-a", libARegistry)
	mockFetcher.AddResponse("https://esm.sh/lib-a@1.0.0/package.json", libAPackage)
	mockFetcher.AddResponse("https://registry.npmjs.org/lib-b", libBRegistry)
	mockFetcher.AddResponse("https://esm.sh/lib-b@1.0.0/package.json", libBPackage)

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{"lib-a": "^1.0.0"}}
	im, err := New(mockFetcher).ResolvePackageJSON(context.Background(), pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}
	for key := range im.Imports {
		if key == "lib-b" || strings.HasPrefix(key, "lib-b/") {
			t.Errorf("transitive package must not appear in top-level imports: %q", key)
		}
	}
	found := false
	for _, scope := range im.Scopes {
		if _, ok := scope["lib-b"]; ok {
			found = true
		}
	}
	if !found {
		t.Errorf("expected lib-b in a scope, got %v", im.Scopes)
	}
}

func TestResolvePackageJSONReturnsErrors(t *testing.T) {
	// Registry fetch for the declared dependency 404s.
	mockFetcher := NewMockFetcher()
	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{"missing": "^1.0.0"}}

	_, err := New(mockFetcher).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an error for an unresolved declared dependency")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected error to name the dependency, got: %v", err)
	}
}

func TestResolvePackageJSONErrorsAreDeterministic(t *testing.T) {
	// Registry fetches for all three declared dependencies 404.
	mockFetcher := NewMockFetcher()
	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{
		"alpha": "^1.0.0",
		"beta":  "^2.0.0",
		"gamma": "^3.0.0",
	}}

	const calls = 20
	var first string
	for i := range calls {
		_, err := New(mockFetcher).ResolvePackageJSON(context.Background(), pkg)
		if err == nil {
			t.Fatal("expected an error for unresolved declared dependencies")
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if got := err.Error(); got != first {
			t.Fatalf("joined error is not deterministic:\nfirst: %q\ngot:   %q", first, got)
		}
	}

	lines := strings.Split(first, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 joined errors, got %d: %q", len(lines), first)
	}
	for i, want := range []string{"alpha@^1.0.0:", "beta@^2.0.0:", "gamma@^3.0.0:"} {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line %d = %q, want prefix %q", i, lines[i], want)
		}
	}
}

func TestDefaultRequestTimeout(t *testing.T) {
	resolver := New(NewMockFetcher())
	if resolver.requestTimeout != DefaultRequestTimeout {
		t.Errorf("requestTimeout = %v, want %v", resolver.requestTimeout, DefaultRequestTimeout)
	}
}

func TestWithRequestTimeoutZeroDisables(t *testing.T) {
	resolver := New(NewMockFetcher()).WithRequestTimeout(0)
	if resolver.requestTimeout != 0 {
		t.Errorf("requestTimeout = %v, want 0 (disabled)", resolver.requestTimeout)
	}
}

func TestTransitiveFailureIsAggregated(t *testing.T) {
	mockFetcher := NewMockFetcher()
	libARegistry := testutil.LoadFixtureFile(t, "lib-a-registry/response.json")
	libAPackage := testutil.LoadFixtureFile(t, "lib-a-package/package.json")
	mockFetcher.AddResponse("https://registry.npmjs.org/lib-a", libARegistry)
	mockFetcher.AddResponse("https://esm.sh/lib-a@1.0.0/package.json", libAPackage)
	// lib-b's registry response is deliberately absent: resolving it 404s.

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{"lib-a": "^1.0.0"}}
	im, err := New(mockFetcher).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an aggregated error for a failed transitive dependency")
	}
	if !strings.Contains(err.Error(), "lib-b") {
		t.Errorf("expected error to name lib-b, got: %v", err)
	}
	if !strings.Contains(err.Error(), "https://esm.sh/lib-a@1.0.0") {
		t.Errorf("expected error to be prefixed with the parent scope, got: %v", err)
	}
	// The partial map still contains the root package that did resolve.
	if im.Imports["lib-a"] == "" {
		t.Error("expected lib-a in partial import map")
	}
}

func TestTransitiveFailureRetriesOnAnotherBranch(t *testing.T) {
	mockFetcher := NewMockFetcher()
	for _, name := range []string{"retry-parent-a", "retry-parent-b"} {
		reg := testutil.LoadFixtureFile(t, name+"-registry/response.json")
		pkg := testutil.LoadFixtureFile(t, name+"-package/package.json")
		mockFetcher.AddResponse("https://registry.npmjs.org/"+name, reg)
		mockFetcher.AddResponse("https://esm.sh/"+name+"@1.0.0/package.json", pkg)
	}
	sharedRegistry := testutil.LoadFixtureFile(t, "retry-shared-registry/response.json")
	sharedPackage := testutil.LoadFixtureFile(t, "retry-shared-package/package.json")
	mockFetcher.AddResponse("https://registry.npmjs.org/retry-shared", sharedRegistry)
	mockFetcher.AddResponse("https://esm.sh/retry-shared@1.0.0/package.json", sharedPackage)

	fetcher := &flakyURLFetcher{
		MockFetcher: mockFetcher,
		url:         "https://registry.npmjs.org/retry-shared",
	}

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{
		"retry-parent-a": "^1.0.0",
		"retry-parent-b": "^1.0.0",
	}}
	// A single worker makes the branch order deterministic: parent-b's task
	// runs after parent-a's failed attempt, so it retries the same package.
	im, err := New(fetcher).WithConcurrency(1).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected the transient failure to be aggregated")
	}
	if got := fetcher.attempts.Load(); got != 2 {
		t.Fatalf("expected the failed dependency to be retried once (2 attempts), got %d", got)
	}
	if im.Imports["retry-parent-a"] == "" || im.Imports["retry-parent-b"] == "" {
		t.Error("expected both declared dependencies in the partial map")
	}

	// The retry succeeded, so the successful branch has a complete scope.
	scopeKey := "https://esm.sh/retry-parent-b@1.0.0/"
	scope, ok := im.Scopes[scopeKey]
	if !ok {
		t.Fatalf("expected scope %q, got %v", scopeKey, im.Scopes)
	}
	if scope["retry-shared"] != "https://esm.sh/retry-shared@1.0.0/index.js" {
		t.Errorf("expected retry-shared in scope %q, got %v", scopeKey, scope)
	}
}

func TestDualRoleDependencyKeepsImportsAndScope(t *testing.T) {
	mockFetcher := NewMockFetcher()
	for _, name := range []string{"lib-a", "lib-b"} {
		reg := testutil.LoadFixtureFile(t, name+"-registry/response.json")
		pkg := testutil.LoadFixtureFile(t, name+"-package/package.json")
		mockFetcher.AddResponse("https://registry.npmjs.org/"+name, reg)
		mockFetcher.AddResponse("https://esm.sh/"+name+"@1.0.0/package.json", pkg)
	}

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{
		"lib-a": "^1.0.0",
		"lib-b": "^1.0.0",
	}}

	im, err := New(mockFetcher).ResolvePackageJSON(context.Background(), pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}

	// lib-b is declared at the root, so it must have top-level imports.
	if im.Imports["lib-b"] != "https://esm.sh/lib-b@1.0.0/index.js" {
		t.Errorf("expected top-level lib-b import, got %q", im.Imports["lib-b"])
	}
	if im.Imports["lib-b/helpers"] != "https://esm.sh/lib-b@1.0.0/helpers.js" {
		t.Errorf("expected top-level lib-b/helpers import, got %q", im.Imports["lib-b/helpers"])
	}
	// lib-b is also a transitive dep of lib-a, so it must appear in that scope.
	scope := im.Scopes["https://esm.sh/lib-a@1.0.0/"]
	if scope["lib-b"] != "https://esm.sh/lib-b@1.0.0/index.js" {
		t.Errorf("expected lib-b in lib-a scope, got %v", im.Scopes)
	}
}

func TestExactPinSurfacesCDN404(t *testing.T) {
	mockFetcher := NewMockFetcher()
	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{
		"pinned": "9.9.9",
	}}

	_, err := New(mockFetcher).WithMaxDepth(1).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an error for a nonexistent exact version")
	}
	if !strings.Contains(err.Error(), "pinned@9.9.9") {
		t.Errorf("expected error to name package and exact version, got: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("expected the CDN 404 to surface, got: %v", err)
	}
	if !mappacdn.IsNotFound(err) {
		t.Errorf("expected IsNotFound to unwrap the aggregated error, got %v", err)
	}
}

func TestOversizedManifestRejected(t *testing.T) {
	mockFetcher := NewMockFetcher()
	mockFetcher.AddResponse("https://registry.npmjs.org/huge-manifest",
		testutil.LoadFixtureFile(t, "huge-manifest-registry/response.json"))
	oversized := []byte(`{"name":"huge-manifest","version":"1.0.0","description":"` +
		strings.Repeat("x", packagejson.MaxPackageJSONSize) + `"}`)
	mockFetcher.AddResponse("https://esm.sh/huge-manifest@1.0.0/package.json", oversized)

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{"huge-manifest": "^1.0.0"}}
	_, err := New(mockFetcher).WithMaxDepth(1).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an oversized manifest to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAllowPartialReturnsWarnings(t *testing.T) {
	mockFetcher := NewMockFetcher()
	litRegistry := testutil.LoadFixtureFile(t, "lit-registry/response.json")
	litPackage := testutil.LoadFixtureFile(t, "lit-package/package.json")
	mockFetcher.AddResponse("https://registry.npmjs.org/lit", litRegistry)
	mockFetcher.AddResponse("https://esm.sh/lit@3.0.0/package.json", litPackage)

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{
		"lit":     "^3.0.0",
		"missing": "^1.0.0",
	}}

	// Default: the error is returned and warnings are not set.
	im, err := New(mockFetcher).WithMaxDepth(1).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an error by default")
	}
	if len(im.Warnings) != 0 {
		t.Errorf("expected no warnings by default, got %v", im.Warnings)
	}

	// Opt in: the partial map is returned with warnings instead of an error.
	im, err = New(mockFetcher).
		WithMaxDepth(1).
		WithAllowPartial(true).
		ResolvePackageJSON(context.Background(), pkg)
	if err != nil {
		t.Fatalf("expected allowPartial to suppress the error, got %v", err)
	}
	if im.Imports["lit"] == "" {
		t.Error("expected lit in the partial import map")
	}
	if len(im.Warnings) == 0 {
		t.Fatal("expected warnings for the unresolved dependency")
	}
	if !strings.Contains(strings.Join(im.Warnings, "\n"), "missing@^1.0.0") {
		t.Errorf("expected warning to name the missing dependency, got %v", im.Warnings)
	}
}

func TestMaxDependencyEntriesRoot(t *testing.T) {
	deps := make(map[string]string, maxDependencyEntries+1)
	for i := range maxDependencyEntries + 1 {
		deps[fmt.Sprintf("dep-%d", i)] = "1.0.0"
	}
	pkg := &packagejson.PackageJSON{Dependencies: deps}

	_, err := New(NewMockFetcher()).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an error for too many root dependencies")
	}
	if !strings.Contains(err.Error(), "maximum") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMaxDependencyEntriesTransitive(t *testing.T) {
	mockFetcher := NewMockFetcher()
	mockFetcher.AddResponse("https://registry.npmjs.org/huge-deps",
		testutil.LoadFixtureFile(t, "huge-deps-registry/response.json"))

	deps := make(map[string]string, maxDependencyEntries+1)
	for i := range maxDependencyEntries + 1 {
		deps[fmt.Sprintf("dep-%d", i)] = "1.0.0"
	}
	manifest, err := json.Marshal(map[string]any{
		"name":         "huge-deps",
		"version":      "1.0.0",
		"exports":      map[string]string{".": "./index.js"},
		"dependencies": deps,
	})
	if err != nil {
		t.Fatal(err)
	}
	mockFetcher.AddResponse("https://esm.sh/huge-deps@1.0.0/package.json", manifest)

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{"huge-deps": "^1.0.0"}}
	_, err = New(mockFetcher).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an error for too many transitive dependencies")
	}
	if !strings.Contains(err.Error(), "maximum") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "huge-deps") {
		t.Errorf("expected error to name the parent package, got: %v", err)
	}
}

func TestMultiFailureAggregatedSortedWithPartialMap(t *testing.T) {
	mockFetcher := NewMockFetcher()
	goodRegistry := testutil.LoadFixtureFile(t, "lit-registry/response.json")
	goodPackage := testutil.LoadFixtureFile(t, "lit-package/package.json")
	mockFetcher.AddResponse("https://registry.npmjs.org/lit", goodRegistry)
	mockFetcher.AddResponse("https://esm.sh/lit@3.0.0/package.json", goodPackage)

	pkg := &packagejson.PackageJSON{Dependencies: map[string]string{
		"zzz-missing": "^1.0.0",
		"lit":         "^3.0.0",
		"aaa-missing": "^1.0.0",
	}}
	im, err := New(mockFetcher).WithConcurrency(2).ResolvePackageJSON(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected an aggregated error")
	}
	if im.Imports["lit"] == "" {
		t.Error("expected the successful dependency in the partial map")
	}

	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 joined errors, got %d: %q", len(lines), err)
	}
	if !strings.HasPrefix(lines[0], "aaa-missing@^1.0.0:") {
		t.Errorf("line 0 = %q, want aaa-missing first", lines[0])
	}
	if !strings.HasPrefix(lines[1], "zzz-missing@^1.0.0:") {
		t.Errorf("line 1 = %q, want zzz-missing second", lines[1])
	}
}

func TestHighFanOutBoundedConcurrency(t *testing.T) {
	mockFetcher := NewMockFetcher()
	const fanOut = 200
	deps := make(map[string]string, fanOut)
	for i := range fanOut {
		name := fmt.Sprintf("fan-%03d", i)
		deps[name] = "^1.0.0"
		registry, err := json.Marshal(map[string]any{
			"name":      name,
			"dist-tags": map[string]string{"latest": "1.0.0"},
			"versions":  map[string]any{"1.0.0": map[string]string{"version": "1.0.0"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := json.Marshal(map[string]any{
			"name":    name,
			"version": "1.0.0",
			"exports": map[string]string{".": "./index.js"},
		})
		if err != nil {
			t.Fatal(err)
		}
		mockFetcher.AddResponse("https://registry.npmjs.org/"+name, registry)
		mockFetcher.AddResponse("https://esm.sh/"+name+"@1.0.0/package.json", manifest)
		mockFetcher.AddDelay("https://registry.npmjs.org/"+name, time.Millisecond)
		mockFetcher.AddDelay("https://esm.sh/"+name+"@1.0.0/package.json", time.Millisecond)
	}

	pkg := &packagejson.PackageJSON{Dependencies: deps}
	im, err := New(mockFetcher).WithConcurrency(4).ResolvePackageJSON(context.Background(), pkg)
	if err != nil {
		t.Fatalf("ResolvePackageJSON error: %v", err)
	}
	if len(im.Imports) != fanOut {
		t.Errorf("expected %d imports, got %d", fanOut, len(im.Imports))
	}
	if max := mockFetcher.maxConcurrent.Load(); max > 4 {
		t.Errorf("expected max concurrent HTTP requests <= 4, got %d", max)
	}
}

// flakyURLFetcher fails the first request to url and counts attempts.
type flakyURLFetcher struct {
	*MockFetcher
	url      string
	attempts atomic.Int32
}

func (f *flakyURLFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if url == f.url && f.attempts.Add(1) == 1 {
		return nil, &mappacdn.FetchError{URL: url, StatusCode: 500, Message: "transient"}
	}
	return f.MockFetcher.Fetch(ctx, url)
}
