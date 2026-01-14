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
}

// MemoryCache is a thread-safe in-memory implementation of Cache.
type MemoryCache struct {
	mu    sync.RWMutex
	cache map[string]*PackageJSON
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

// Invalidate removes a cached entry.
func (c *MemoryCache) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, path)
}
