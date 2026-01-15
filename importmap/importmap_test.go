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
	"reflect"
	"testing"

	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/testutil"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"basic imports", "parse-basic"},
		{"with scopes", "parse-with-scopes"},
		{"with integrity", "parse-with-integrity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := testutil.NewFixtureFS(t, "importmap/"+tt.dir, "/test")

			input, err := mfs.ReadFile("/test/input.json")
			if err != nil {
				t.Fatalf("Failed to read input.json: %v", err)
			}

			im, err := importmap.Parse(input)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			// Re-marshal to JSON to compare
			output, err := json.Marshal(im)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			// Parse both as generic maps to compare
			var inputMap, outputMap map[string]any
			if err := json.Unmarshal(input, &inputMap); err != nil {
				t.Fatalf("Failed to unmarshal input: %v", err)
			}
			if err := json.Unmarshal(output, &outputMap); err != nil {
				t.Fatalf("Failed to unmarshal output: %v", err)
			}

			if !reflect.DeepEqual(inputMap, outputMap) {
				t.Errorf("Round-trip failed:\n  input:  %s\n  output: %s", string(input), string(output))
			}
		})
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"simple merge", "merge-simple"},
		{"merge with scopes", "merge-with-scopes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := testutil.NewFixtureFS(t, "importmap/"+tt.dir, "/test")

			baseData, err := mfs.ReadFile("/test/base.json")
			if err != nil {
				t.Fatalf("Failed to read base.json: %v", err)
			}

			overrideData, err := mfs.ReadFile("/test/override.json")
			if err != nil {
				t.Fatalf("Failed to read override.json: %v", err)
			}

			expectedData, err := mfs.ReadFile("/test/expected.json")
			if err != nil {
				t.Fatalf("Failed to read expected.json: %v", err)
			}

			base, err := importmap.Parse(baseData)
			if err != nil {
				t.Fatalf("Failed to parse base: %v", err)
			}

			override, err := importmap.Parse(overrideData)
			if err != nil {
				t.Fatalf("Failed to parse override: %v", err)
			}

			var expected importmap.ImportMap
			if err := json.Unmarshal(expectedData, &expected); err != nil {
				t.Fatalf("Failed to parse expected: %v", err)
			}

			result := base.Merge(override)

			if !reflect.DeepEqual(result.Imports, expected.Imports) {
				t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
			}

			if !reflect.DeepEqual(result.Scopes, expected.Scopes) {
				t.Errorf("Scopes mismatch:\n  got:      %v\n  expected: %v", result.Scopes, expected.Scopes)
			}
		})
	}
}

func TestToJSON(t *testing.T) {
	im := &importmap.ImportMap{
		Imports: map[string]string{
			"lit": "/node_modules/lit/index.js",
		},
	}

	jsonStr := im.ToJSON()
	if jsonStr == "" {
		t.Error("ToJSON returned empty string for non-empty import map")
	}

	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Errorf("ToJSON produced invalid JSON: %v", err)
	}
}

func TestToJSONEmpty(t *testing.T) {
	im := &importmap.ImportMap{}
	jsonStr := im.ToJSON()
	if jsonStr != "" {
		t.Errorf("ToJSON should return empty string for empty import map, got: %s", jsonStr)
	}
}

func TestToJSONNil(t *testing.T) {
	var im *importmap.ImportMap
	jsonStr := im.ToJSON()
	if jsonStr != "" {
		t.Errorf("ToJSON should return empty string for nil import map, got: %s", jsonStr)
	}
}

func TestSimplify(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"basic trailing-slash removal", "simplify-basic"},
		{"with scopes", "simplify-with-scopes"},
		{"no trailing-slash keys", "simplify-no-trailing-slash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := testutil.NewFixtureFS(t, "importmap/"+tt.dir, "/test")

			inputData, err := mfs.ReadFile("/test/input.json")
			if err != nil {
				t.Fatalf("Failed to read input.json: %v", err)
			}

			expectedData, err := mfs.ReadFile("/test/expected.json")
			if err != nil {
				t.Fatalf("Failed to read expected.json: %v", err)
			}

			input, err := importmap.Parse(inputData)
			if err != nil {
				t.Fatalf("Failed to parse input: %v", err)
			}

			var expected importmap.ImportMap
			if err := json.Unmarshal(expectedData, &expected); err != nil {
				t.Fatalf("Failed to parse expected: %v", err)
			}

			result := input.Simplify()

			if !reflect.DeepEqual(result.Imports, expected.Imports) {
				t.Errorf("Imports mismatch:\n  got:      %v\n  expected: %v", result.Imports, expected.Imports)
			}

			if !reflect.DeepEqual(result.Scopes, expected.Scopes) {
				t.Errorf("Scopes mismatch:\n  got:      %v\n  expected: %v", result.Scopes, expected.Scopes)
			}
		})
	}
}
