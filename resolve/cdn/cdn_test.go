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
	"testing"

	mappacdn "bennypowers.dev/mappa/cdn"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/testutil"
)

// MockFetcher is a test implementation of the Fetcher interface.
type MockFetcher struct {
	responses map[string][]byte
	errors    map[string]error
}

func NewMockFetcher() *MockFetcher {
	return &MockFetcher{
		responses: make(map[string][]byte),
		errors:    make(map[string]error),
	}
}

func (m *MockFetcher) AddResponse(url string, data []byte) {
	m.responses[url] = data
}

func (m *MockFetcher) AddError(url string, err error) {
	m.errors[url] = err
}

func (m *MockFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
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
