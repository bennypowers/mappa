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
	loading sync.Map // map[string]*cacheEntry for in-flight loads
}

// NewMemoryCache creates a new in-memory cache for package.json files.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		cache: make(map[string]*PackageJSON),
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
	delete(c.cache, path)
	c.mu.Unlock()
	c.loading.Delete(path)
}

// GetOrLoad atomically retrieves from cache or loads using the provided function.
// Only one goroutine will execute the loader for a given path; others wait for the result.
func (c *MemoryCache) GetOrLoad(path string, loader func() (*PackageJSON, error)) (*PackageJSON, error) {
	// Fast path: check if already cached
	c.mu.RLock()
	if pkg, ok := c.cache[path]; ok {
		c.mu.RUnlock()
		return pkg, nil
	}
	c.mu.RUnlock()

	// Get or create an entry for this path - all concurrent goroutines get the same entry
	actual, _ := c.loading.LoadOrStore(path, &cacheEntry{})
	entry := actual.(*cacheEntry)

	// Only one goroutine executes the loader; others block until once.Do completes
	entry.once.Do(func() {
		entry.pkg, entry.err = loader()
		if entry.err == nil {
			c.mu.Lock()
			c.cache[path] = entry.pkg
			c.mu.Unlock()
		}
	})

	// Note: We don't delete from c.loading here because it would race with
	// concurrent LoadOrStore calls. Entries remain until Invalidate is called.
	// This is acceptable: entries are small (sync.Once + pointers) and bounded
	// by unique paths. Invalidate clears both maps for proper cache refresh.

	return entry.pkg, entry.err
}
