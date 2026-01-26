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
	"errors"
	"sync"
	"testing"

	"bennypowers.dev/mappa/packagejson"
)

func TestPackageCache(t *testing.T) {
	cache := NewPackageCache(10)

	// Test Get on empty cache
	_, ok := cache.Get("lit", "3.0.0")
	if ok {
		t.Error("Expected cache miss for empty cache")
	}

	// Test Set and Get
	pkg := &packagejson.PackageJSON{Name: "lit", Version: "3.0.0"}
	cache.Set("lit", "3.0.0", pkg)

	got, ok := cache.Get("lit", "3.0.0")
	if !ok {
		t.Error("Expected cache hit after Set")
	}
	if got.Name != "lit" || got.Version != "3.0.0" {
		t.Errorf("Expected lit@3.0.0, got %s@%s", got.Name, got.Version)
	}

	// Test Size
	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}

	// Test Invalidate
	cache.Invalidate("lit", "3.0.0")
	_, ok = cache.Get("lit", "3.0.0")
	if ok {
		t.Error("Expected cache miss after Invalidate")
	}

	// Test Clear
	cache.Set("lit", "3.0.0", pkg)
	cache.Set("preact", "10.0.0", &packagejson.PackageJSON{Name: "preact"})
	cache.Clear()
	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after Clear, got %d", cache.Size())
	}
}

func TestPackageCacheEviction(t *testing.T) {
	cache := NewPackageCache(3)

	// Fill cache
	cache.Set("pkg1", "1.0.0", &packagejson.PackageJSON{Name: "pkg1"})
	cache.Set("pkg2", "1.0.0", &packagejson.PackageJSON{Name: "pkg2"})
	cache.Set("pkg3", "1.0.0", &packagejson.PackageJSON{Name: "pkg3"})

	// Verify all present
	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Add one more - should evict oldest (pkg1)
	cache.Set("pkg4", "1.0.0", &packagejson.PackageJSON{Name: "pkg4"})

	_, ok := cache.Get("pkg1", "1.0.0")
	if ok {
		t.Error("Expected pkg1 to be evicted")
	}

	_, ok = cache.Get("pkg4", "1.0.0")
	if !ok {
		t.Error("Expected pkg4 to be present")
	}
}

func TestPackageCacheGetOrLoad(t *testing.T) {
	cache := NewPackageCache(10)

	loadCount := 0
	loader := func() (*packagejson.PackageJSON, error) {
		loadCount++
		return &packagejson.PackageJSON{Name: "lit", Version: "3.0.0"}, nil
	}

	// First call should load
	pkg, err := cache.GetOrLoad("lit", "3.0.0", loader)
	if err != nil {
		t.Fatalf("GetOrLoad error: %v", err)
	}
	if pkg.Name != "lit" {
		t.Errorf("Expected lit, got %s", pkg.Name)
	}
	if loadCount != 1 {
		t.Errorf("Expected 1 load, got %d", loadCount)
	}

	// Second call should use cache
	_, err = cache.GetOrLoad("lit", "3.0.0", loader)
	if err != nil {
		t.Fatalf("GetOrLoad error: %v", err)
	}
	if loadCount != 1 {
		t.Errorf("Expected 1 load (cached), got %d", loadCount)
	}
}

func TestPackageCacheGetOrLoadError(t *testing.T) {
	cache := NewPackageCache(10)

	expectedErr := errors.New("load failed")
	loader := func() (*packagejson.PackageJSON, error) {
		return nil, expectedErr
	}

	_, err := cache.GetOrLoad("lit", "3.0.0", loader)
	if err == nil {
		t.Error("Expected error from GetOrLoad")
	}
}

func TestPackageCacheConcurrency(t *testing.T) {
	cache := NewPackageCache(100)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			pkg := &packagejson.PackageJSON{Name: "pkg", Version: "1.0.0"}
			cache.Set("pkg", "1.0.0", pkg)
			cache.Get("pkg", "1.0.0")
			_, _ = cache.GetOrLoad("pkg", "1.0.0", func() (*packagejson.PackageJSON, error) {
				return pkg, nil
			})
		})
	}
	wg.Wait()

	// Verify cache integrity after concurrent access
	got, ok := cache.Get("pkg", "1.0.0")
	if !ok {
		t.Error("Expected cache entry to exist after concurrent operations")
	}
	if got == nil || got.Name != "pkg" || got.Version != "1.0.0" {
		t.Errorf("Cache entry corrupted: got %+v", got)
	}
	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", cache.Size())
	}
}
