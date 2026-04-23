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

package importmap

import "sort"

// MapDiff describes changes between two specifier maps.
type MapDiff struct {
	// Added contains entries present in B but not in A.
	Added map[string]string `json:"added"`

	// Removed contains entries present in A but not in B.
	Removed map[string]string `json:"removed"`

	// Modified contains entries present in both but with different values.
	// Each value is [old, new].
	Modified map[string][2]string `json:"modified"`
}

// Empty reports whether the diff contains no changes.
func (d MapDiff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Modified) == 0
}

// Diff describes the differences between two import maps.
type Diff struct {
	// Imports holds the diff of top-level imports.
	Imports MapDiff `json:"imports"`

	// Scopes holds per-scope diffs for scopes present in both maps.
	Scopes map[string]MapDiff `json:"scopes"`

	// AddedScopes lists scope keys present in B but not A.
	AddedScopes []string `json:"added_scopes"`

	// RemovedScopes lists scope keys present in A but not B.
	RemovedScopes []string `json:"removed_scopes"`
}

// Empty reports whether the diff contains no changes.
func (d *Diff) Empty() bool {
	if !d.Imports.Empty() {
		return false
	}
	if len(d.AddedScopes) > 0 || len(d.RemovedScopes) > 0 {
		return false
	}
	for _, sd := range d.Scopes {
		if !sd.Empty() {
			return false
		}
	}
	return true
}

// Compare computes the diff between two import maps.
// A nil import map is treated as empty.
func Compare(a, b *ImportMap) *Diff {
	aImports := emptyIfNil(getImports(a))
	bImports := emptyIfNil(getImports(b))
	aScopes := emptyIfNilScopes(getScopes(a))
	bScopes := emptyIfNilScopes(getScopes(b))

	d := &Diff{
		Imports: diffMaps(aImports, bImports),
		Scopes:  make(map[string]MapDiff),
	}

	// Find scopes present in both, added, or removed
	for scope, bEntries := range bScopes {
		if aEntries, ok := aScopes[scope]; ok {
			sd := diffMaps(aEntries, bEntries)
			if !sd.Empty() {
				d.Scopes[scope] = sd
			}
		} else {
			d.AddedScopes = append(d.AddedScopes, scope)
		}
	}
	for scope := range aScopes {
		if _, ok := bScopes[scope]; !ok {
			d.RemovedScopes = append(d.RemovedScopes, scope)
		}
	}

	sort.Strings(d.AddedScopes)
	sort.Strings(d.RemovedScopes)

	if d.AddedScopes == nil {
		d.AddedScopes = []string{}
	}
	if d.RemovedScopes == nil {
		d.RemovedScopes = []string{}
	}

	return d
}

func getImports(im *ImportMap) map[string]string {
	if im == nil {
		return nil
	}
	return im.Imports
}

func getScopes(im *ImportMap) map[string]map[string]string {
	if im == nil {
		return nil
	}
	return im.Scopes
}

func emptyIfNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func emptyIfNilScopes(m map[string]map[string]string) map[string]map[string]string {
	if m == nil {
		return map[string]map[string]string{}
	}
	return m
}

func diffMaps(a, b map[string]string) MapDiff {
	d := MapDiff{
		Added:    make(map[string]string),
		Removed:  make(map[string]string),
		Modified: make(map[string][2]string),
	}

	for key, aVal := range a {
		if bVal, ok := b[key]; ok {
			if aVal != bVal {
				d.Modified[key] = [2]string{aVal, bVal}
			}
		} else {
			d.Removed[key] = aVal
		}
	}

	for key, bVal := range b {
		if _, ok := a[key]; !ok {
			d.Added[key] = bVal
		}
	}

	return d
}
