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
package resolve

import "bennypowers.dev/mappa/importmap"

// IncrementalUpdate describes which packages changed for incremental resolution.
type IncrementalUpdate struct {
	// ChangedPackages lists packages whose package.json files changed.
	// Can include both workspace packages and node_modules packages.
	// Use the package name (e.g., "@myorg/core" or "lit").
	ChangedPackages []string

	// PreviousMap is the import map to update incrementally.
	// If nil, a full resolution is performed.
	PreviousMap *importmap.ImportMap

	// PreviousGraph is the dependency graph from the previous resolution.
	// If nil, dependents cannot be computed and a full resolution is used.
	PreviousGraph *DependencyGraph
}

// IncrementalResult contains both the new import map and dependency graph.
type IncrementalResult struct {
	// ImportMap is the resolved import map.
	ImportMap *importmap.ImportMap

	// DependencyGraph tracks package dependencies for future incremental updates.
	DependencyGraph *DependencyGraph
}
