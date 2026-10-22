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
package packagejson

import "sync"

// Cache provides a caching interface for parsed package.json files.
// This allows callers to reuse parsed data across multiple resolution calls,
// improving performance for scenarios like hot-reload or monorepo traversal.
type Cache interface {
	// Get retrieves a cached package.json by its file path.
	// Returns the cached package and true if found, nil and false otherwise.
	Get(path string) (*PackageJSON, bool)

	// Set stores a parsed package.json in the cache, keyed by file path.
	Set(path string, pkg *PackageJSON)

	// Invalidate removes a cached entry, typically called when a file changes.
	Invalidate(path string)

	// GetOrLoad atomically retrieves from cache or loads using the provided function.
	// Only one goroutine should execute the loader for a given path; others wait.
	GetOrLoad(path string, loader func() (*PackageJSON, error)) (*PackageJSON, error)
}

// cacheEntry holds a cached value and coordinates concurrent loading.
type cacheEntry struct {
	pkg  *PackageJSON
	err  error
	once sync.Once
}

// MemoryCache is a thread-safe in-memory implementation of Cache.
type MemoryCache struct {
	mu      sync.RWMutex
	cache   map[string]*PackageJSON
	loading map[string]*cacheEntry // in-flight loads
}

// NewMemoryCache creates a new in-memory cache for package.json files.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		cache:   make(map[string]*PackageJSON),
		loading: make(map[string]*cacheEntry),
	}
}

// Get retrieves a cached package.json by its file path.
func (c *MemoryCache) Get(path string) (*PackageJSON, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	pkg, ok := c.cache[path]
	return pkg, ok
}

// Set stores a parsed package.json in the cache.
func (c *MemoryCache) Set(path string, pkg *PackageJSON) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[path] = pkg
}

// Invalidate removes a cached entry and any in-flight loading state.
func (c *MemoryCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, path)
	delete(c.loading, path)
}

// GetOrLoad atomically retrieves from cache or loads using the provided function.
// Only one goroutine will execute the loader for a given path; others wait for the result.
func (c *MemoryCache) GetOrLoad(path string, loader func() (*PackageJSON, error)) (*PackageJSON, error) {
	// Fast path: check if already cached. A nil value is a negative-cache
	// marker from a caller that only needs lookups; GetOrLoad must still run
	// the loader so callers receive a proper error or fresh data.
	c.mu.Lock()
	if pkg, ok := c.cache[path]; ok && pkg != nil {
		c.mu.Unlock()
		return pkg, nil
	}
	entry, ok := c.loading[path]
	if !ok {
		entry = &cacheEntry{}
		c.loading[path] = entry
	}
	c.mu.Unlock()

	// Only one goroutine executes the loader; others block until once.Do completes.
	entry.once.Do(func() {
		entry.pkg, entry.err = loader()
		if entry.err == nil && entry.pkg != nil {
			c.mu.Lock()
			// Skip storing if Invalidate (or a newer load) replaced this entry.
			if c.loading[path] == entry {
				c.cache[path] = entry.pkg
			}
			c.mu.Unlock()
		}
	})

	// Do not cache failures or successful nil loads: both must be retryable.
	// Delete only if this entry is still the current one, so an Invalidate
	// followed by a fresh load is not clobbered.
	c.mu.Lock()
	if c.loading[path] == entry && (entry.err != nil || entry.pkg == nil) {
		delete(c.loading, path)
	}
	c.mu.Unlock()

	return entry.pkg, entry.err
}
