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
	lru *lru[*cacheEntry]
}

type cacheEntry struct {
	pkg    *packagejson.PackageJSON
	once   sync.Once
	err    error
	loader func() (*packagejson.PackageJSON, error)
}

func (e *cacheEntry) load() {
	e.once.Do(func() {
		if e.loader != nil {
			e.pkg, e.err = e.loader()
			e.loader = nil
		}
	})
}

// NewPackageCache creates a new package cache with the specified maximum size.
// When the cache exceeds this size, the oldest entries are evicted.
func NewPackageCache(maxSize int) *PackageCache {
	return &PackageCache{lru: newLRU[*cacheEntry](maxSize)}
}

// cacheKey generates a cache key for a package at a specific version.
func cacheKey(pkgName, version string) string {
	return pkgName + "@" + version
}

// Get retrieves a cached package.json by package name and version.
// Returns nil, false if not in cache. If another goroutine is still loading
// the entry, Get waits for that load to finish.
func (c *PackageCache) Get(pkgName, version string) (*packagejson.PackageJSON, bool) {
	entry, ok := c.lru.get(cacheKey(pkgName, version))
	if !ok {
		return nil, false
	}
	entry.load()
	if entry.err != nil || entry.pkg == nil {
		return nil, false
	}
	return entry.pkg, true
}

// Set stores a package.json in the cache.
func (c *PackageCache) Set(pkgName, version string, pkg *packagejson.PackageJSON) {
	c.lru.set(cacheKey(pkgName, version), &cacheEntry{pkg: pkg})
}

// GetOrLoad retrieves a cached package.json or loads it using the provided loader.
// The loader runs at most once per successful load, even with concurrent access.
// Failed loads are evicted from the cache, so a subsequent call retries.
//
// When a waiter observes a load that failed because the leader's context was
// canceled or expired, it retries under its own loader and context. Without
// this, one caller's cancellation would poison every request coalesced behind
// it, even though those callers are still live. Retries are bounded.
func (c *PackageCache) GetOrLoad(pkgName, version string, loader func() (*packagejson.PackageJSON, error)) (*packagejson.PackageJSON, error) {
	return c.getOrLoad(pkgName, version, loader, 2)
}

func (c *PackageCache) getOrLoad(pkgName, version string, loader func() (*packagejson.PackageJSON, error), retries int) (*packagejson.PackageJSON, error) {
	key := cacheKey(pkgName, version)

	if entry, ok := c.lru.get(key); ok {
		entry.load()
		if entry.err != nil && retries > 0 && isContextError(entry.err) {
			c.lru.deleteIf(key, func(current *cacheEntry) bool { return current == entry })
			return c.getOrLoad(pkgName, version, loader, retries-1)
		}
		return entry.pkg, entry.err
	}

	entry := &cacheEntry{loader: loader}
	existing, exists := c.lru.getOrInsert(key, entry)
	if exists {
		existing.load()
		if existing.err != nil && retries > 0 && isContextError(existing.err) {
			c.lru.deleteIf(key, func(current *cacheEntry) bool { return current == existing })
			return c.getOrLoad(pkgName, version, loader, retries-1)
		}
		return existing.pkg, existing.err
	}

	entry.load()
	if entry.err != nil {
		c.lru.deleteIf(key, func(current *cacheEntry) bool { return current == entry })
	}
	return entry.pkg, entry.err
}

// Invalidate removes a specific package from the cache.
func (c *PackageCache) Invalidate(pkgName, version string) {
	c.lru.remove(cacheKey(pkgName, version))
}

// Clear removes all entries from the cache.
func (c *PackageCache) Clear() {
	c.lru.clear()
}

// Size returns the current number of entries in the cache.
func (c *PackageCache) Size() int {
	return c.lru.size()
}
