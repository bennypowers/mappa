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
	"errors"
	"sync"
	"sync/atomic"
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

func TestMemoryCacheGetOrLoad(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	var loadCount atomic.Int32
	loader := func() (*packagejson.PackageJSON, error) {
		loadCount.Add(1)
		return &packagejson.PackageJSON{Name: "loaded"}, nil
	}

	// First call should invoke loader
	pkg, err := cache.GetOrLoad("/path/to/package.json", loader)
	if err != nil {
		t.Fatalf("GetOrLoad failed: %v", err)
	}
	if pkg.Name != "loaded" {
		t.Errorf("Expected name 'loaded', got %q", pkg.Name)
	}
	if loadCount.Load() != 1 {
		t.Errorf("Expected loader to be called once, called %d times", loadCount.Load())
	}

	// Second call should use cached value, not invoke loader
	pkg, err = cache.GetOrLoad("/path/to/package.json", loader)
	if err != nil {
		t.Fatalf("GetOrLoad failed: %v", err)
	}
	if pkg.Name != "loaded" {
		t.Errorf("Expected name 'loaded', got %q", pkg.Name)
	}
	if loadCount.Load() != 1 {
		t.Errorf("Expected loader to still be called once, called %d times", loadCount.Load())
	}
}

func TestMemoryCacheGetOrLoadConcurrent(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	var loadCount atomic.Int32
	loader := func() (*packagejson.PackageJSON, error) {
		loadCount.Add(1)
		return &packagejson.PackageJSON{Name: "loaded"}, nil
	}

	// Launch many goroutines trying to load the same path
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cache.GetOrLoad("/same/path/package.json", loader)
			if err != nil {
				t.Errorf("GetOrLoad failed: %v", err)
			}
		}()
	}
	wg.Wait()

	// Loader should only be called once due to atomic GetOrLoad
	if loadCount.Load() != 1 {
		t.Errorf("Expected loader to be called exactly once, called %d times", loadCount.Load())
	}
}

func TestMemoryCacheInvalidateAllowsReload(t *testing.T) {
	cache := packagejson.NewMemoryCache()

	var loadCount atomic.Int32
	loader := func() (*packagejson.PackageJSON, error) {
		n := loadCount.Add(1)
		return &packagejson.PackageJSON{Name: "loaded", Version: string(rune('0' + n))}, nil
	}

	// First load
	pkg, err := cache.GetOrLoad("/path/package.json", loader)
	if err != nil {
		t.Fatalf("GetOrLoad failed: %v", err)
	}
	if pkg.Version != "1" {
		t.Errorf("Expected version '1', got %q", pkg.Version)
	}
	if loadCount.Load() != 1 {
		t.Errorf("Expected 1 load, got %d", loadCount.Load())
	}

	// Invalidate
	cache.Invalidate("/path/package.json")

	// Second load should reload (not use stale entry)
	pkg, err = cache.GetOrLoad("/path/package.json", loader)
	if err != nil {
		t.Fatalf("GetOrLoad failed: %v", err)
	}
	if pkg.Version != "2" {
		t.Errorf("Expected version '2' after invalidate, got %q", pkg.Version)
	}
	if loadCount.Load() != 2 {
		t.Errorf("Expected 2 loads after invalidate, got %d", loadCount.Load())
	}
}

func TestMemoryCacheInvalidateRacingInFlightLoad(t *testing.T) {
	cache := packagejson.NewMemoryCache()
	const path = "/path/package.json"

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	loader := func() (*packagejson.PackageJSON, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return &packagejson.PackageJSON{Name: "stale"}, nil
		}
		return &packagejson.PackageJSON{Name: "fresh"}, nil
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = cache.GetOrLoad(path, loader)
	}()

	<-started
	cache.Invalidate(path)
	close(release)
	<-firstDone

	// The invalidated load's result must not be cached; the next call must
	// invoke the loader again and observe fresh data.
	pkg, err := cache.GetOrLoad(path, loader)
	if err != nil {
		t.Fatalf("GetOrLoad after invalidate failed: %v", err)
	}
	if pkg == nil || pkg.Name != "fresh" {
		t.Errorf("expected fresh package after invalidate, got %+v", pkg)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 loader calls, got %d", calls.Load())
	}
}

func TestMemoryCacheRetriesAfterError(t *testing.T) {
	cache := packagejson.NewMemoryCache()
	wantErr := errors.New("transient")
	var calls atomic.Int32
	loader := func() (*packagejson.PackageJSON, error) {
		if calls.Add(1) == 1 {
			return nil, wantErr
		}
		return &packagejson.PackageJSON{Name: "ok"}, nil
	}

	if _, err := cache.GetOrLoad("/path/package.json", loader); !errors.Is(err, wantErr) {
		t.Fatalf("expected first load to fail, got %v", err)
	}
	pkg, err := cache.GetOrLoad("/path/package.json", loader)
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if pkg.Name != "ok" {
		t.Errorf("unexpected package %+v", pkg)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 loader calls, got %d", calls.Load())
	}
}

func TestMemoryCacheRetriesAfterNilSuccess(t *testing.T) {
	cache := packagejson.NewMemoryCache()
	var calls atomic.Int32
	loader := func() (*packagejson.PackageJSON, error) {
		if calls.Add(1) == 1 {
			return nil, nil
		}
		return &packagejson.PackageJSON{Name: "ok"}, nil
	}

	pkg, err := cache.GetOrLoad("/path/package.json", loader)
	if err != nil || pkg != nil {
		t.Fatalf("expected a nil result from the first load, got %+v, %v", pkg, err)
	}
	pkg, err = cache.GetOrLoad("/path/package.json", loader)
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if pkg == nil || pkg.Name != "ok" {
		t.Errorf("unexpected package %+v", pkg)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 loader calls, got %d", calls.Load())
	}
}
