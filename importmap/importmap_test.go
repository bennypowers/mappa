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
	"strings"
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
		{"keeps bare specifier alongside trailing slash", "simplify-keeps-bare-specifier"},
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

func TestValidate(t *testing.T) {
	t.Run("valid import map", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "importmap/validate-valid", "/test")
		input, err := mfs.ReadFile("/test/input.json")
		if err != nil {
			t.Fatalf("Failed to read input.json: %v", err)
		}
		im, err := importmap.Parse(input)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		errs := im.Validate()
		if len(errs) != 0 {
			t.Errorf("Expected no validation errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("trailing-slash key without trailing-slash value", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "importmap/validate-trailing-slash", "/test")
		input, err := mfs.ReadFile("/test/input.json")
		if err != nil {
			t.Fatalf("Failed to read input.json: %v", err)
		}
		im, err := importmap.Parse(input)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		errs := im.Validate()
		if len(errs) == 0 {
			t.Fatal("Expected validation errors for trailing-slash mismatch")
		}
		foundTrailing := false
		foundInvalidValue := false
		for _, e := range errs {
			if e.Key == "lit/" {
				foundTrailing = true
			}
			if e.Key == "broken/" {
				foundInvalidValue = true
			}
		}
		if !foundTrailing {
			t.Error("Expected error for lit/ key with non-trailing-slash value")
		}
		if !foundInvalidValue {
			t.Error("Expected error for broken/ key with bare value")
		}
	})

	t.Run("invalid URL values", func(t *testing.T) {
		mfs := testutil.NewFixtureFS(t, "importmap/validate-invalid-values", "/test")
		input, err := mfs.ReadFile("/test/input.json")
		if err != nil {
			t.Fatalf("Failed to read input.json: %v", err)
		}
		im, err := importmap.Parse(input)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		errs := im.Validate()
		if len(errs) != 2 {
			t.Fatalf("Expected 2 validation errors, got %d: %v", len(errs), errs)
		}
	})

	t.Run("nil import map", func(t *testing.T) {
		var im *importmap.ImportMap
		errs := im.Validate()
		if len(errs) != 0 {
			t.Errorf("Expected no errors for nil import map, got %v", errs)
		}
	})

	t.Run("scope validation", func(t *testing.T) {
		im := &importmap.ImportMap{
			Scopes: map[string]map[string]string{
				"/node_modules/lit/": {
					"@lit/reactive-element/": "/node_modules/@lit/reactive-element",
				},
			},
		}
		errs := im.Validate()
		if len(errs) == 0 {
			t.Fatal("Expected validation error for scope trailing-slash mismatch")
		}
		if errs[0].Scope != "/node_modules/lit/" {
			t.Errorf("Expected scope in error, got %q", errs[0].Scope)
		}
	})
}

func TestValidationErrorString(t *testing.T) {
	t.Run("top-level error", func(t *testing.T) {
		e := &importmap.ValidationError{Key: "lit/", Value: "/bad", Message: "test msg"}
		s := e.Error()
		if s != `key "lit/": test msg` {
			t.Errorf("Unexpected error string: %s", s)
		}
	})

	t.Run("scope error", func(t *testing.T) {
		e := &importmap.ValidationError{Key: "lit/", Value: "/bad", Scope: "/scope/", Message: "test msg"}
		s := e.Error()
		if s != `scope "/scope/" key "lit/": test msg` {
			t.Errorf("Unexpected error string: %s", s)
		}
	})
}

func TestSimplifyNil(t *testing.T) {
	var im *importmap.ImportMap
	result := im.Simplify()
	if result != nil {
		t.Errorf("Simplify on nil should return nil, got %v", result)
	}
}

func TestCloneNil(t *testing.T) {
	var im *importmap.ImportMap
	result := im.Clone()
	if result != nil {
		t.Errorf("Clone on nil should return nil, got %v", result)
	}
}

func TestMergeNils(t *testing.T) {
	t.Run("both nil", func(t *testing.T) {
		var a, b *importmap.ImportMap
		result := a.Merge(b)
		if result == nil {
			t.Fatal("Merge(nil, nil) should return empty map, not nil")
		}
	})

	t.Run("base nil", func(t *testing.T) {
		var a *importmap.ImportMap
		b := &importmap.ImportMap{Imports: map[string]string{"lit": "/lit.js"}}
		result := a.Merge(b)
		if result.Imports["lit"] != "/lit.js" {
			t.Errorf("Expected lit import, got %v", result.Imports)
		}
	})

	t.Run("other nil", func(t *testing.T) {
		a := &importmap.ImportMap{Imports: map[string]string{"lit": "/lit.js"}}
		result := a.Merge(nil)
		if result.Imports["lit"] != "/lit.js" {
			t.Errorf("Expected lit import, got %v", result.Imports)
		}
	})
}

func TestToHTML(t *testing.T) {
	im := &importmap.ImportMap{
		Imports: map[string]string{"lit": "/node_modules/lit/index.js"},
	}
	html := im.ToHTML()
	prefix := `<script type="importmap">`
	if !strings.HasPrefix(html, prefix) {
		t.Errorf("ToHTML should start with script tag, got: %s", html)
	}
}

func TestFormat(t *testing.T) {
	im := &importmap.ImportMap{}
	if im.Format("json") != "{}" {
		t.Errorf("Format json for empty map should return {}, got %s", im.Format("json"))
	}
	if im.Format("html") == "{}" {
		t.Error("Format html should not return bare JSON")
	}
}

func TestSimplifyEntries(t *testing.T) {
	entries := map[string]string{
		"lit":               "/node_modules/lit/index.js",
		"lit/":              "/node_modules/lit/",
		"lit/decorators.js": "/node_modules/lit/decorators.js",
		"lit/html.js":       "/node_modules/lit/html.js",
		"other":             "/node_modules/other/index.js",
	}
	result := importmap.SimplifyEntries(entries)
	if _, ok := result["lit/decorators.js"]; ok {
		t.Error("lit/decorators.js should be simplified away")
	}
	if _, ok := result["lit/html.js"]; ok {
		t.Error("lit/html.js should be simplified away")
	}
	if result["lit"] != "/node_modules/lit/index.js" {
		t.Error("bare specifier 'lit' should be kept")
	}
	if result["lit/"] != "/node_modules/lit/" {
		t.Error("trailing-slash key 'lit/' should be kept")
	}
	if result["other"] != "/node_modules/other/index.js" {
		t.Error("unrelated key 'other' should be kept")
	}
}
