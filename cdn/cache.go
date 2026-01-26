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
	"sync"

	"bennypowers.dev/mappa/packagejson"
)

// PackageCache provides a thread-safe cache for fetched package.json data.
// This cache stores parsed PackageJSON objects keyed by package@version.
type PackageCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	order   []string // LRU order tracking
	maxSize int
}

type cacheEntry struct {
	pkg  *packagejson.PackageJSON
	once sync.Once
	err  error
}

// NewPackageCache creates a new package cache with the specified maximum size.
// When the cache exceeds this size, the oldest entries are evicted.
func NewPackageCache(maxSize int) *PackageCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &PackageCache{
		entries: make(map[string]*cacheEntry),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

// cacheKey generates a cache key for a package at a specific version.
func cacheKey(pkgName, version string) string {
	return pkgName + "@" + version
}

// Get retrieves a cached package.json by package name and version.
// Returns nil, false if not in cache.
func (c *PackageCache) Get(pkgName, version string) (*packagejson.PackageJSON, bool) {
	key := cacheKey(pkgName, version)
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || entry.err != nil {
		return nil, false
	}
	return entry.pkg, true
}

// Set stores a package.json in the cache.
func (c *PackageCache) Set(pkgName, version string, pkg *packagejson.PackageJSON) {
	key := cacheKey(pkgName, version)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if _, exists := c.entries[key]; exists {
		c.entries[key] = &cacheEntry{pkg: pkg}
		return
	}

	// Evict oldest if at capacity
	if len(c.entries) >= c.maxSize {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}

	c.entries[key] = &cacheEntry{pkg: pkg}
	c.order = append(c.order, key)
}

// GetOrLoad retrieves a cached package.json or loads it using the provided loader.
// The loader is called at most once per cache key, even with concurrent access.
func (c *PackageCache) GetOrLoad(pkgName, version string, loader func() (*packagejson.PackageJSON, error)) (*packagejson.PackageJSON, error) {
	key := cacheKey(pkgName, version)

	// Fast path: check if already cached
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if ok {
		entry.once.Do(func() {}) // Ensure once completed
		if entry.err != nil {
			return nil, entry.err
		}
		return entry.pkg, nil
	}

	// Slow path: create entry and load
	c.mu.Lock()
	// Double-check after acquiring write lock
	entry, ok = c.entries[key]
	if ok {
		c.mu.Unlock()
		entry.once.Do(func() {})
		if entry.err != nil {
			return nil, entry.err
		}
		return entry.pkg, nil
	}

	// Create new entry
	entry = &cacheEntry{}
	c.entries[key] = entry

	// Evict oldest if at capacity
	if len(c.entries) > c.maxSize {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.order = append(c.order, key)
	c.mu.Unlock()

	// Load outside the lock
	entry.once.Do(func() {
		entry.pkg, entry.err = loader()
	})

	if entry.err != nil {
		return nil, entry.err
	}
	return entry.pkg, nil
}

// Invalidate removes a specific package from the cache.
func (c *PackageCache) Invalidate(pkgName, version string) {
	key := cacheKey(pkgName, version)
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
	// Remove from order slice
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// Clear removes all entries from the cache.
func (c *PackageCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
	c.order = make([]string, 0, c.maxSize)
}

// Size returns the current number of entries in the cache.
func (c *PackageCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
