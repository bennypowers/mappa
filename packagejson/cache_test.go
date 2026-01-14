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
package packagejson_test

import (
	"testing"

	"bennypowers.dev/mappa/packagejson"
)

func TestMemoryCacheGet(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	// Cache miss should return nil, false
	pkg, ok := cache.Get("/nonexistent/package.json")
	if ok {
		t.Error("Expected cache miss for nonexistent path")
	}
	if pkg != nil {
		t.Error("Expected nil package for cache miss")
	}
}

func TestMemoryCacheSet(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	pkg := &packagejson.PackageJSON{
		Name:    "test-package",
		Version: "1.0.0",
	}

	cache.Set("/path/to/package.json", pkg)

	// Should return the cached package
	got, ok := cache.Get("/path/to/package.json")
	if !ok {
		t.Error("Expected cache hit after Set")
	}
	if got.Name != "test-package" {
		t.Errorf("Expected name 'test-package', got %q", got.Name)
	}
	if got.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got %q", got.Version)
	}
}

func TestMemoryCacheInvalidate(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	pkg := &packagejson.PackageJSON{Name: "test"}
	cache.Set("/path/to/package.json", pkg)

	// Verify it's cached
	if _, ok := cache.Get("/path/to/package.json"); !ok {
		t.Fatal("Expected cache hit before invalidation")
	}

	// Invalidate
	cache.Invalidate("/path/to/package.json")

	// Should now be a cache miss
	if _, ok := cache.Get("/path/to/package.json"); ok {
		t.Error("Expected cache miss after invalidation")
	}
}

func TestMemoryCacheInvalidateNonexistent(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	// Should not panic when invalidating nonexistent key
	cache.Invalidate("/nonexistent/package.json")
}

func TestMemoryCacheConcurrency(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	// Test concurrent access
	done := make(chan bool)
	for range 100 {
		go func() {
			path := "/path/to/package.json"
			pkg := &packagejson.PackageJSON{Name: "test"}
			cache.Set(path, pkg)
			cache.Get(path)
			cache.Invalidate(path)
			done <- true
		}()
	}

	for range 100 {
		<-done
	}
}

func TestCacheInterface(t *testing.T) {
	// Verify MemoryCache implements Cache interface
	var _ packagejson.Cache = (*packagejson.MemoryCache)(nil)
}
