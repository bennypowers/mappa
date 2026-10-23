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

import "sync"

// lru is a small size-bounded least-recently-used map. It centralizes the
// order bookkeeping shared by PackageCache and VersionCache so the LRU loop
// exists in exactly one place.
type lru[V any] struct {
	mu      sync.Mutex
	entries map[string]V
	order   []string // least recently used first
	maxSize int
}

func newLRU[V any](maxSize int) *lru[V] {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &lru[V]{
		entries: make(map[string]V),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

// get returns the value for key without refreshing its recency.
func (l *lru[V]) get(key string) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.entries[key]
	return v, ok
}

// set stores a value and marks the key as most recently used.
func (l *lru[V]) set(key string, value V) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.setLocked(key, value)
}

// getOrInsert returns the existing value for key, or inserts fresh and reports
// that it was absent.
func (l *lru[V]) getOrInsert(key string, fresh V) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if v, ok := l.entries[key]; ok {
		return v, true
	}
	l.evictLocked()
	l.entries[key] = fresh
	l.order = append(l.order, key)
	return fresh, false
}

// remove deletes key if present.
func (l *lru[V]) remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.deleteLocked(key)
}

// deleteIf deletes key only when match accepts the current value. This guards
// cleanup paths from removing an entry that another goroutine replaced.
func (l *lru[V]) deleteIf(key string, match func(V) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if v, ok := l.entries[key]; ok && match(v) {
		l.deleteLocked(key)
	}
}

// clear removes every entry.
func (l *lru[V]) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = make(map[string]V)
	l.order = make([]string, 0, l.maxSize)
}

// size returns the number of entries.
func (l *lru[V]) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *lru[V]) setLocked(key string, value V) {
	if _, exists := l.entries[key]; exists {
		l.entries[key] = value
		l.touchLocked(key)
		return
	}
	l.evictLocked()
	l.entries[key] = value
	l.order = append(l.order, key)
}

// touchLocked moves key to the most-recently-used end of the order slice.
func (l *lru[V]) touchLocked(key string) {
	for i, k := range l.order {
		if k == key {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	l.order = append(l.order, key)
}

func (l *lru[V]) deleteLocked(key string) {
	if _, ok := l.entries[key]; !ok {
		return
	}
	delete(l.entries, key)
	for i, k := range l.order {
		if k == key {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
}

func (l *lru[V]) evictLocked() {
	if len(l.entries) < l.maxSize || len(l.order) == 0 {
		return
	}
	oldest := l.order[0]
	l.order = l.order[1:]
	delete(l.entries, oldest)
}
