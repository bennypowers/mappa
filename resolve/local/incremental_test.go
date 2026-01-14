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
package local_test

import (
	"reflect"
	"slices"
	"testing"

	"bennypowers.dev/mappa/resolve"
	"bennypowers.dev/mappa/resolve/local"
	"bennypowers.dev/mappa/testutil"
)

func TestResolveWithGraph(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/with-scopes", "/test")

	resolver := local.New(mfs, nil)
	result, err := resolver.ResolveWithGraph("/test")
	if err != nil {
		t.Fatalf("ResolveWithGraph failed: %v", err)
	}

	// Verify import map is correct
	if result.ImportMap.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import, got %v", result.ImportMap.Imports)
	}

	// Verify graph was built
	if result.DependencyGraph == nil {
		t.Fatal("Expected DependencyGraph to be non-nil")
	}

	// Verify scope key was tracked
	scopeKey := result.DependencyGraph.ScopeKey("lit")
	if scopeKey != "/node_modules/lit/" {
		t.Errorf("Expected scope key '/node_modules/lit/', got %q", scopeKey)
	}

	// Verify dependencies were tracked
	deps := result.DependencyGraph.Dependents("@lit/reactive-element")
	if !slices.Contains(deps, "lit") {
		t.Errorf("Expected lit to depend on @lit/reactive-element, got dependents: %v", deps)
	}
}

func TestResolveIncrementalFallback(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/with-scopes", "/test")

	resolver := local.New(mfs, nil)

	// With nil PreviousMap, should fall back to full resolution
	result, err := resolver.ResolveIncremental("/test", resolve.IncrementalUpdate{
		ChangedPackages: []string{"lit"},
		PreviousMap:     nil,
		PreviousGraph:   nil,
	})
	if err != nil {
		t.Fatalf("ResolveIncremental failed: %v", err)
	}

	if result.ImportMap.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import after fallback, got %v", result.ImportMap.Imports)
	}
}

func TestResolveIncrementalChangedPackage(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/with-scopes", "/test")

	resolver := local.New(mfs, nil)

	// First, do a full resolution
	initial, err := resolver.ResolveWithGraph("/test")
	if err != nil {
		t.Fatalf("Initial ResolveWithGraph failed: %v", err)
	}

	// Simulate a change to lit (in real usage, the file would have changed)
	// For this test, we just verify the incremental resolution runs correctly
	result, err := resolver.ResolveIncremental("/test", resolve.IncrementalUpdate{
		ChangedPackages: []string{"lit"},
		PreviousMap:     initial.ImportMap,
		PreviousGraph:   initial.DependencyGraph,
	})
	if err != nil {
		t.Fatalf("ResolveIncremental failed: %v", err)
	}

	// Result should still be correct
	if result.ImportMap.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import after incremental, got %v", result.ImportMap.Imports)
	}

	// Graph should still have the dependency
	deps := result.DependencyGraph.Dependents("@lit/reactive-element")
	if !slices.Contains(deps, "lit") {
		t.Errorf("Expected lit to depend on @lit/reactive-element after incremental, got dependents: %v", deps)
	}
}

func TestResolveIncrementalEmptyChanges(t *testing.T) {
	mfs := testutil.NewFixtureFS(t, "resolve/with-scopes", "/test")

	resolver := local.New(mfs, nil)

	// With empty ChangedPackages, should fall back to full resolution
	result, err := resolver.ResolveIncremental("/test", resolve.IncrementalUpdate{
		ChangedPackages: []string{},
		PreviousMap:     nil,
		PreviousGraph:   nil,
	})
	if err != nil {
		t.Fatalf("ResolveIncremental failed: %v", err)
	}

	if result.ImportMap.Imports["lit"] != "/node_modules/lit/index.js" {
		t.Errorf("Expected lit import after fallback, got %v", result.ImportMap.Imports)
	}
}

func TestDependencyGraphDependents(t *testing.T) {
	graph := resolve.NewDependencyGraph()

	// Set up: A depends on B, B depends on C
	graph.AddDependency("A", "B")
	graph.AddDependency("B", "C")

	// Direct dependents of C should be B
	deps := graph.Dependents("C")
	if !reflect.DeepEqual(deps, []string{"B"}) {
		t.Errorf("Expected Dependents(C) = [B], got %v", deps)
	}

	// Direct dependents of B should be A
	deps = graph.Dependents("B")
	if !reflect.DeepEqual(deps, []string{"A"}) {
		t.Errorf("Expected Dependents(B) = [A], got %v", deps)
	}
}

func TestDependencyGraphTransitiveDependents(t *testing.T) {
	graph := resolve.NewDependencyGraph()

	// Set up: A depends on B, B depends on C
	graph.AddDependency("A", "B")
	graph.AddDependency("B", "C")

	// Transitive dependents of C should be both B and A
	deps := graph.TransitiveDependents("C")
	if len(deps) != 2 {
		t.Errorf("Expected 2 transitive dependents of C, got %v", deps)
	}
	if !slices.Contains(deps, "A") || !slices.Contains(deps, "B") {
		t.Errorf("Expected transitive dependents to include A and B, got %v", deps)
	}
}

func TestDependencyGraphClone(t *testing.T) {
	graph := resolve.NewDependencyGraph()
	graph.AddDependency("A", "B")
	graph.SetScopeKey("A", "/node_modules/A/")
	graph.SetPackagePath("A", "/path/to/A")
	graph.AddWorkspacePackage("A")

	clone := graph.Clone()

	// Verify clone has same data
	if !reflect.DeepEqual(graph.Dependents("B"), clone.Dependents("B")) {
		t.Error("Clone should have same dependents")
	}
	if graph.ScopeKey("A") != clone.ScopeKey("A") {
		t.Error("Clone should have same scope key")
	}
	if graph.PackagePath("A") != clone.PackagePath("A") {
		t.Error("Clone should have same package path")
	}
	if graph.IsWorkspacePackage("A") != clone.IsWorkspacePackage("A") {
		t.Error("Clone should have same workspace package status")
	}

	// Modify original, verify clone is independent
	graph.AddDependency("C", "D")
	if len(clone.Dependents("D")) != 0 {
		t.Error("Clone should be independent of original modifications")
	}
}

func TestDependencyGraphRemovePackage(t *testing.T) {
	graph := resolve.NewDependencyGraph()

	// Set up: A depends on B, B depends on C
	graph.AddDependency("A", "B")
	graph.AddDependency("B", "C")
	graph.SetScopeKey("B", "/node_modules/B/")
	graph.SetPackagePath("B", "/path/to/B")

	// Remove B
	dependents := graph.RemovePackage("B")

	// Should return A as dependent
	if !slices.Contains(dependents, "A") {
		t.Errorf("Expected RemovePackage to return A as dependent, got %v", dependents)
	}

	// B should no longer be tracked
	if graph.ScopeKey("B") != "" {
		t.Error("Expected scope key to be removed")
	}
	if graph.PackagePath("B") != "" {
		t.Error("Expected package path to be removed")
	}

	// C should no longer have B as dependent
	deps := graph.Dependents("C")
	if slices.Contains(deps, "B") {
		t.Errorf("Expected C to no longer have B as dependent, got %v", deps)
	}
}
