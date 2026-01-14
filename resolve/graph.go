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

import (
	"maps"
	"slices"
	"sync"
)

// DependencyGraph tracks package dependencies for incremental updates.
// Built during resolution and cached for efficient dependent discovery.
type DependencyGraph struct {
	mu sync.RWMutex

	// dependsOn maps package name -> set of package names it imports
	// e.g., "lit" -> {"@lit/reactive-element": true, "lit-html": true}
	dependsOn map[string]map[string]bool

	// dependents maps package name -> set of packages that depend on it
	// e.g., "@lit/reactive-element" -> {"lit": true}
	dependents map[string]map[string]bool

	// scopeKeys maps package name -> scope key in import map
	// e.g., "lit" -> "/node_modules/lit/"
	scopeKeys map[string]string

	// packagePaths maps package name -> filesystem path
	// Used to locate package.json files for cache invalidation
	packagePaths map[string]string

	// workspacePackages tracks which packages are workspace packages
	workspacePackages map[string]bool
}

// NewDependencyGraph creates a new empty dependency graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		dependsOn:         make(map[string]map[string]bool),
		dependents:        make(map[string]map[string]bool),
		scopeKeys:         make(map[string]string),
		packagePaths:      make(map[string]string),
		workspacePackages: make(map[string]bool),
	}
}

// AddDependency records that pkg depends on dep.
// Updates both dependsOn and dependents maps.
func (g *DependencyGraph) AddDependency(pkg, dep string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.dependsOn[pkg] == nil {
		g.dependsOn[pkg] = make(map[string]bool)
	}
	g.dependsOn[pkg][dep] = true

	if g.dependents[dep] == nil {
		g.dependents[dep] = make(map[string]bool)
	}
	g.dependents[dep][pkg] = true
}

// SetScopeKey records the import map scope key for a package.
func (g *DependencyGraph) SetScopeKey(pkg, scopeKey string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.scopeKeys[pkg] = scopeKey
}

// ScopeKey returns the scope key for a package, or empty string if not found.
func (g *DependencyGraph) ScopeKey(pkg string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.scopeKeys[pkg]
}

// SetPackagePath records the filesystem path for a package.
func (g *DependencyGraph) SetPackagePath(pkg, path string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.packagePaths[pkg] = path
}

// PackagePath returns the filesystem path for a package, or empty string if not found.
func (g *DependencyGraph) PackagePath(pkg string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.packagePaths[pkg]
}

// AddWorkspacePackage marks a package as a workspace package.
func (g *DependencyGraph) AddWorkspacePackage(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.workspacePackages[name] = true
}

// IsWorkspacePackage returns true if the package is a workspace package.
func (g *DependencyGraph) IsWorkspacePackage(name string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.workspacePackages[name]
}

// Dependents returns all packages that directly depend on pkg.
func (g *DependencyGraph) Dependents(pkg string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	deps := g.dependents[pkg]
	if deps == nil {
		return nil
	}
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	slices.Sort(result)
	return result
}

// TransitiveDependents returns all packages that directly or indirectly depend on pkg.
// Uses breadth-first traversal to find all dependents.
func (g *DependencyGraph) TransitiveDependents(pkg string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool)
	queue := []string{pkg}
	var result []string

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for dep := range g.dependents[current] {
			if !visited[dep] {
				visited[dep] = true
				result = append(result, dep)
				queue = append(queue, dep)
			}
		}
	}

	slices.Sort(result)
	return result
}

// Clone creates a deep copy of the dependency graph.
func (g *DependencyGraph) Clone() *DependencyGraph {
	g.mu.RLock()
	defer g.mu.RUnlock()

	clone := NewDependencyGraph()

	for pkg, deps := range g.dependsOn {
		clone.dependsOn[pkg] = make(map[string]bool, len(deps))
		maps.Copy(clone.dependsOn[pkg], deps)
	}

	for pkg, deps := range g.dependents {
		clone.dependents[pkg] = make(map[string]bool, len(deps))
		maps.Copy(clone.dependents[pkg], deps)
	}

	maps.Copy(clone.scopeKeys, g.scopeKeys)
	maps.Copy(clone.packagePaths, g.packagePaths)
	maps.Copy(clone.workspacePackages, g.workspacePackages)

	return clone
}

// RemovePackage removes a package and all its edges from the graph.
// Returns the packages that were dependents of the removed package.
func (g *DependencyGraph) RemovePackage(pkg string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Collect dependents before removal
	deps := g.dependents[pkg]
	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}

	// Remove from dependents: for each dep that pkg depends on, remove pkg from their dependents
	for dep := range g.dependsOn[pkg] {
		delete(g.dependents[dep], pkg)
	}

	// Remove from dependsOn: for each dependent of pkg, remove pkg from their dependsOn
	for dependent := range g.dependents[pkg] {
		delete(g.dependsOn[dependent], pkg)
	}

	// Remove the package's own entries
	delete(g.dependsOn, pkg)
	delete(g.dependents, pkg)
	delete(g.scopeKeys, pkg)
	delete(g.packagePaths, pkg)
	delete(g.workspacePackages, pkg)

	slices.Sort(result)
	return result
}
