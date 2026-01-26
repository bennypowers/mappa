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
	"context"
	"fmt"
	"testing"
)

// MockFetcher is a test implementation of the Fetcher interface.
type MockFetcher struct {
	responses map[string][]byte
	errors    map[string]error
}

func NewMockFetcher() *MockFetcher {
	return &MockFetcher{
		responses: make(map[string][]byte),
		errors:    make(map[string]error),
	}
}

func (m *MockFetcher) AddResponse(url string, data []byte) {
	m.responses[url] = data
}

func (m *MockFetcher) AddError(url string, err error) {
	m.errors[url] = err
}

func (m *MockFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if err, ok := m.errors[url]; ok {
		return nil, err
	}
	if data, ok := m.responses[url]; ok {
		return data, nil
	}
	return nil, &FetchError{URL: url, StatusCode: 404, Message: "Not Found"}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input   string
		want    *SemVer
		wantErr bool
	}{
		{"1.0.0", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"2.3.4", &SemVer{Major: 2, Minor: 3, Patch: 4}, false},
		{"1.0.0-alpha", &SemVer{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha"}, false},
		{"1.0.0-beta.1", &SemVer{Major: 1, Minor: 0, Patch: 0, Prerelease: "beta.1"}, false},
		{"v1.0.0", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"1.0", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"1", &SemVer{Major: 1, Minor: 0, Patch: 0}, false},
		{"invalid", nil, true},
		{"", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSemver(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSemver(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.want != nil {
				if got.Major != tt.want.Major || got.Minor != tt.want.Minor ||
					got.Patch != tt.want.Patch || got.Prerelease != tt.want.Prerelease {
					t.Errorf("parseSemver(%q) = %+v, want %+v", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s vs %s", tt.a, tt.b), func(t *testing.T) {
			got := compareSemver(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMatchVersion(t *testing.T) {
	versions := []string{"1.0.0", "1.0.1", "1.1.0", "1.2.0", "2.0.0", "2.1.0", "3.0.0-alpha"}

	tests := []struct {
		name         string
		versionRange string
		want         string
	}{
		{"exact version", "1.0.0", "1.0.0"},
		{"exact version missing", "4.0.0", ""},
		{"caret major", "^1.0.0", "1.2.0"},
		{"caret minor", "^1.1.0", "1.2.0"},
		{"tilde", "~1.0.0", "1.0.1"},
		{"gte", ">=2.0.0", "2.1.0"},
		{"gt", ">1.2.0", "2.1.0"},
		{"lte", "<=1.1.0", "1.1.0"},
		{"lt", "<2.0.0", "1.2.0"},
		{"latest", "latest", "2.1.0"},
		{"star", "*", "2.1.0"},
		{"empty", "", "2.1.0"},
		{"x-range major", "1.x", "1.2.0"},
		{"x-range minor", "1.0.x", "1.0.1"},
		{"hyphen range", "1.0.0 - 1.1.0", "1.1.0"},
		{"or range", "^1.0.0 || ^2.0.0", "2.1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchVersion(versions, tt.versionRange)
			if got != tt.want {
				t.Errorf("matchVersion(%q) = %q, want %q", tt.versionRange, got, tt.want)
			}
		})
	}
}

func TestMatchCaretRange(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		base     string
		want     string
	}{
		{
			"major version constraint",
			[]string{"1.0.0", "1.1.0", "1.2.0", "2.0.0"},
			"1.0.0",
			"1.2.0",
		},
		{
			"zero major",
			[]string{"0.1.0", "0.1.1", "0.2.0"},
			"0.1.0",
			"0.1.1",
		},
		{
			"zero major zero minor",
			[]string{"0.0.1", "0.0.2", "0.1.0"},
			"0.0.1",
			"0.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchCaretRange(tt.versions, tt.base)
			if got != tt.want {
				t.Errorf("matchCaretRange(%v, %q) = %q, want %q", tt.versions, tt.base, got, tt.want)
			}
		})
	}
}

func TestMatchTildeRange(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		base     string
		want     string
	}{
		{
			"patch updates only",
			[]string{"1.0.0", "1.0.1", "1.0.2", "1.1.0"},
			"1.0.0",
			"1.0.2",
		},
		{
			"no match",
			[]string{"1.1.0", "1.2.0"},
			"1.0.0",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchTildeRange(tt.versions, tt.base)
			if got != tt.want {
				t.Errorf("matchTildeRange(%v, %q) = %q, want %q", tt.versions, tt.base, got, tt.want)
			}
		})
	}
}

func TestRegistryResolveVersion(t *testing.T) {
	mockFetcher := NewMockFetcher()
	mockFetcher.AddResponse("https://registry.npmjs.org/lit", []byte(`{
		"name": "lit",
		"dist-tags": {
			"latest": "3.0.0"
		},
		"versions": {
			"2.0.0": {},
			"2.1.0": {},
			"3.0.0": {}
		}
	}`))

	registry := NewRegistry(mockFetcher)
	ctx := context.Background()

	tests := []struct {
		name         string
		pkgName      string
		versionRange string
		want         string
		wantErr      bool
	}{
		{"dist-tag latest", "lit", "latest", "3.0.0", false},
		{"caret range", "lit", "^2.0.0", "2.1.0", false},
		{"exact version", "lit", "3.0.0", "3.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := registry.ResolveVersion(ctx, tt.pkgName, tt.versionRange)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCache(t *testing.T) {
	cache := NewVersionCache()

	// Initially not in cache
	_, ok := cache.Get("lit", "^3.0.0")
	if ok {
		t.Error("Expected cache miss for new entry")
	}

	// Set and get
	cache.Set("lit", "^3.0.0", "3.1.0")
	version, ok := cache.Get("lit", "^3.0.0")
	if !ok {
		t.Error("Expected cache hit after set")
	}
	if version != "3.1.0" {
		t.Errorf("Expected version 3.1.0, got %s", version)
	}
}
