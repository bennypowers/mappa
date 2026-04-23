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
package importmap_test

import (
	"encoding/json"
	"testing"

	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/testutil"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"identical maps", "diff-identical"},
		{"added entries", "diff-added"},
		{"removed entries", "diff-removed"},
		{"modified entries", "diff-modified"},
		{"scope changes", "diff-scopes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := testutil.NewFixtureFS(t, "importmap/"+tt.dir, "/test")

			aData, err := mfs.ReadFile("/test/a.json")
			if err != nil {
				t.Fatalf("Failed to read a.json: %v", err)
			}

			bData, err := mfs.ReadFile("/test/b.json")
			if err != nil {
				t.Fatalf("Failed to read b.json: %v", err)
			}

			expectedData, err := mfs.ReadFile("/test/expected.json")
			if err != nil {
				t.Fatalf("Failed to read expected.json: %v", err)
			}

			a, err := importmap.Parse(aData)
			if err != nil {
				t.Fatalf("Failed to parse a.json: %v", err)
			}

			b, err := importmap.Parse(bData)
			if err != nil {
				t.Fatalf("Failed to parse b.json: %v", err)
			}

			result := importmap.Compare(a, b)

			// Marshal result to JSON for comparison
			resultJSON, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("Failed to marshal result: %v", err)
			}

			// Parse both as generic maps to compare
			var resultMap, expectedMap map[string]any
			if err := json.Unmarshal(resultJSON, &resultMap); err != nil {
				t.Fatalf("Failed to unmarshal result: %v", err)
			}
			if err := json.Unmarshal(expectedData, &expectedMap); err != nil {
				t.Fatalf("Failed to unmarshal expected: %v", err)
			}

			resultNorm, _ := json.MarshalIndent(resultMap, "", "  ")
			expectedNorm, _ := json.MarshalIndent(expectedMap, "", "  ")

			if string(resultNorm) != string(expectedNorm) {
				t.Errorf("Diff mismatch:\n  got:\n%s\n  expected:\n%s", resultNorm, expectedNorm)
			}
		})
	}
}

func TestCompareNils(t *testing.T) {
	t.Run("both nil", func(t *testing.T) {
		result := importmap.Compare(nil, nil)
		if !result.Empty() {
			t.Error("Expected empty diff for two nil import maps")
		}
	})

	t.Run("a nil", func(t *testing.T) {
		b := &importmap.ImportMap{
			Imports: map[string]string{"lit": "/lit.js"},
		}
		result := importmap.Compare(nil, b)
		if result.Empty() {
			t.Error("Expected non-empty diff when a is nil and b has imports")
		}
		if result.Imports.Added["lit"] != "/lit.js" {
			t.Errorf("Expected lit in added, got %v", result.Imports.Added)
		}
	})

	t.Run("b nil", func(t *testing.T) {
		a := &importmap.ImportMap{
			Imports: map[string]string{"lit": "/lit.js"},
		}
		result := importmap.Compare(a, nil)
		if result.Empty() {
			t.Error("Expected non-empty diff when b is nil and a has imports")
		}
		if result.Imports.Removed["lit"] != "/lit.js" {
			t.Errorf("Expected lit in removed, got %v", result.Imports.Removed)
		}
	})
}

func TestMapDiffEmpty(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		d := importmap.MapDiff{
			Added:    map[string]string{},
			Removed:  map[string]string{},
			Modified: map[string][2]string{},
		}
		if !d.Empty() {
			t.Error("Expected empty MapDiff to report Empty()")
		}
	})

	t.Run("has added", func(t *testing.T) {
		d := importmap.MapDiff{
			Added:    map[string]string{"foo": "bar"},
			Removed:  map[string]string{},
			Modified: map[string][2]string{},
		}
		if d.Empty() {
			t.Error("Expected non-empty MapDiff with added entries")
		}
	})

	t.Run("has removed", func(t *testing.T) {
		d := importmap.MapDiff{
			Added:    map[string]string{},
			Removed:  map[string]string{"foo": "bar"},
			Modified: map[string][2]string{},
		}
		if d.Empty() {
			t.Error("Expected non-empty MapDiff with removed entries")
		}
	})

	t.Run("has modified", func(t *testing.T) {
		d := importmap.MapDiff{
			Added:    map[string]string{},
			Removed:  map[string]string{},
			Modified: map[string][2]string{"foo": {"old", "new"}},
		}
		if d.Empty() {
			t.Error("Expected non-empty MapDiff with modified entries")
		}
	})
}

func TestDiffEmpty(t *testing.T) {
	t.Run("empty diff", func(t *testing.T) {
		d := &importmap.Diff{
			Imports: importmap.MapDiff{
				Added:    map[string]string{},
				Removed:  map[string]string{},
				Modified: map[string][2]string{},
			},
			Scopes:        map[string]importmap.MapDiff{},
			AddedScopes:   []string{},
			RemovedScopes: []string{},
		}
		if !d.Empty() {
			t.Error("Expected empty Diff to report Empty()")
		}
	})

	t.Run("has added scopes", func(t *testing.T) {
		d := &importmap.Diff{
			Imports: importmap.MapDiff{
				Added:    map[string]string{},
				Removed:  map[string]string{},
				Modified: map[string][2]string{},
			},
			Scopes:        map[string]importmap.MapDiff{},
			AddedScopes:   []string{"/new/"},
			RemovedScopes: []string{},
		}
		if d.Empty() {
			t.Error("Expected non-empty Diff with added scopes")
		}
	})

	t.Run("has scope diff", func(t *testing.T) {
		d := &importmap.Diff{
			Imports: importmap.MapDiff{
				Added:    map[string]string{},
				Removed:  map[string]string{},
				Modified: map[string][2]string{},
			},
			Scopes: map[string]importmap.MapDiff{
				"/scope/": {
					Added:    map[string]string{"foo": "bar"},
					Removed:  map[string]string{},
					Modified: map[string][2]string{},
				},
			},
			AddedScopes:   []string{},
			RemovedScopes: []string{},
		}
		if d.Empty() {
			t.Error("Expected non-empty Diff with scope changes")
		}
	})
}
