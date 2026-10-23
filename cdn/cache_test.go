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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestPackageCacheGetWaitsForInFlightLoad(t *testing.T) {
	cache := NewPackageCache(10)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	want := &packagejson.PackageJSON{Name: "lit", Version: "3.0.0"}
	loadDone := make(chan struct{})
	go func() {
		defer close(loadDone)
		_, _ = cache.GetOrLoad("lit", "3.0.0", func() (*packagejson.PackageJSON, error) {
			once.Do(func() { close(started) })
			<-release
			return want, nil
		})
	}()

	<-started

	type getResult struct {
		pkg *packagejson.PackageJSON
		ok  bool
	}
	got := make(chan getResult, 1)
	go func() {
		pkg, ok := cache.Get("lit", "3.0.0")
		got <- getResult{pkg, ok}
	}()

	select {
	case r := <-got:
		t.Fatalf("Get returned before the in-flight load completed: %+v", r)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	<-loadDone

	select {
	case r := <-got:
		if !r.ok || r.pkg != want {
			t.Fatalf("Get = (%v, %v), want (%v, true)", r.pkg, r.ok, want)
		}
	case <-time.After(time.Second):
		t.Fatal("Get did not return after the load completed")
	}
}

func TestPackageCacheGetNeverReturnsNilTrue(t *testing.T) {
	cache := NewPackageCache(10)
	started := make(chan struct{})
	release := make(chan struct{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cache.GetOrLoad("lit", "3.0.0", func() (*packagejson.PackageJSON, error) {
			close(started)
			<-release
			return &packagejson.PackageJSON{Name: "lit"}, nil
		})
	}()
	<-started

	type getResult struct {
		pkg *packagejson.PackageJSON
		ok  bool
	}
	result := make(chan getResult, 1)
	go func() {
		pkg, ok := cache.Get("lit", "3.0.0")
		result <- getResult{pkg, ok}
	}()

	// An in-flight entry must not surface as a hit before it has loaded.
	select {
	case got := <-result:
		t.Fatalf("Get returned (nil, true)-style result while loading: %+v", got)
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	<-done
	got := <-result
	if !got.ok || got.pkg == nil {
		t.Fatalf("Get = (%v, %v) after load, want a non-nil hit", got.pkg, got.ok)
	}
}

func TestPackageCacheCleanupDoesNotEvictReplacement(t *testing.T) {
	cache := NewPackageCache(10)
	started := make(chan struct{})
	release := make(chan struct{})

	failed := make(chan struct{})
	go func() {
		defer close(failed)
		_, _ = cache.GetOrLoad("lit", "3.0.0", func() (*packagejson.PackageJSON, error) {
			close(started)
			<-release
			return nil, errors.New("load failed")
		})
	}()

	<-started
	replacement := &packagejson.PackageJSON{Name: "lit", Version: "3.0.0"}
	cache.Set("lit", "3.0.0", replacement)
	close(release)
	<-failed

	got, ok := cache.Get("lit", "3.0.0")
	if !ok {
		t.Fatal("expected replacement entry to remain cached")
	}
	if got != replacement {
		t.Errorf("got %+v, want the replacement entry", got)
	}
}

func TestPackageCacheEvictionThenRefetch(t *testing.T) {
	cache := NewPackageCache(2)
	var loads sync.Map // key -> *atomic.Int32

	load := func(name string) func() (*packagejson.PackageJSON, error) {
		return func() (*packagejson.PackageJSON, error) {
			v, _ := loads.LoadOrStore(name, &atomic.Int32{})
			v.(*atomic.Int32).Add(1)
			return &packagejson.PackageJSON{Name: name}, nil
		}
	}

	for _, name := range []string{"a", "b"} {
		if _, err := cache.GetOrLoad(name, "1.0.0", load(name)); err != nil {
			t.Fatalf("GetOrLoad(%s) error: %v", name, err)
		}
	}
	// Insert c, evicting a (oldest).
	if _, err := cache.GetOrLoad("c", "1.0.0", load("c")); err != nil {
		t.Fatalf("GetOrLoad(c) error: %v", err)
	}
	if _, ok := cache.Get("a", "1.0.0"); ok {
		t.Fatal("expected a to be evicted")
	}
	// Refetching a must call the loader again.
	if _, err := cache.GetOrLoad("a", "1.0.0", load("a")); err != nil {
		t.Fatalf("GetOrLoad(a) after eviction error: %v", err)
	}
	v, _ := loads.Load("a")
	if got := v.(*atomic.Int32).Load(); got != 2 {
		t.Errorf("expected a to load twice, got %d", got)
	}
}

func TestPackageCacheRetriesAfterError(t *testing.T) {
	cache := NewPackageCache(10)
	wantErr := errors.New("transient")
	var calls atomic.Int32
	loader := func() (*packagejson.PackageJSON, error) {
		if calls.Add(1) == 1 {
			return nil, wantErr
		}
		return &packagejson.PackageJSON{Name: "ok"}, nil
	}

	if _, err := cache.GetOrLoad("lit", "3.0.0", loader); !errors.Is(err, wantErr) {
		t.Fatalf("expected first load to fail, got %v", err)
	}
	pkg, err := cache.GetOrLoad("lit", "3.0.0", loader)
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

func TestPackageCacheWaiterRetriesAfterLeaderContextFailure(t *testing.T) {
	cache := NewPackageCache(10)

	started := make(chan struct{})
	release := make(chan struct{})
	leaderDone := make(chan error, 1)

	go func() {
		_, err := cache.GetOrLoad("lit", "3.0.0", func() (*packagejson.PackageJSON, error) {
			close(started)
			<-release
			return nil, context.Canceled
		})
		leaderDone <- err
	}()

	<-started

	var waiterLoads atomic.Int32
	type waiterResult struct {
		pkg *packagejson.PackageJSON
		err error
	}
	waiterDone := make(chan waiterResult, 1)
	go func() {
		pkg, err := cache.GetOrLoad("lit", "3.0.0", func() (*packagejson.PackageJSON, error) {
			waiterLoads.Add(1)
			return &packagejson.PackageJSON{Name: "lit", Version: "3.0.0"}, nil
		})
		waiterDone <- waiterResult{pkg: pkg, err: err}
	}()

	// Give the waiter time to join the in-flight load before the leader fails.
	time.Sleep(20 * time.Millisecond)
	close(release)

	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the leader to fail with context.Canceled, got %v", err)
	}
	got := <-waiterDone
	if got.err != nil {
		t.Fatalf("expected the live waiter to retry, got %v", got.err)
	}
	if got.pkg == nil || got.pkg.Name != "lit" {
		t.Errorf("unexpected waiter package: %+v", got.pkg)
	}
	if waiterLoads.Load() != 1 {
		t.Errorf("expected the waiter's loader to run once, got %d", waiterLoads.Load())
	}
}
