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
	"testing"
)

func TestProviderByName(t *testing.T) {
	tests := []struct {
		name     string
		wantName string
		wantNil  bool
	}{
		{"esm.sh", "esm.sh", false},
		{"esmsh", "esm.sh", false},
		{"esm", "esm.sh", false},
		{"unpkg", "unpkg", false},
		{"jsdelivr", "jsdelivr", false},
		{"jsdelivr.net", "jsdelivr", false},
		{"cdn.jsdelivr.net", "jsdelivr", false},
		{"unknown", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProviderByName(tt.name)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ProviderByName(%q) = %v, want nil", tt.name, got)
				}
				return
			}
			if got == nil {
				t.Errorf("ProviderByName(%q) = nil, want %q", tt.name, tt.wantName)
				return
			}
			if got.Name != tt.wantName {
				t.Errorf("ProviderByName(%q).Name = %q, want %q", tt.name, got.Name, tt.wantName)
			}
		})
	}
}

func TestProviderNames(t *testing.T) {
	names := ProviderNames()
	if len(names) != 3 {
		t.Errorf("Expected 3 provider names, got %d", len(names))
	}
}

func TestIsValidProvider(t *testing.T) {
	if !IsValidProvider("esm.sh") {
		t.Error("Expected esm.sh to be valid")
	}
	if IsValidProvider("unknown") {
		t.Error("Expected unknown to be invalid")
	}
}

func TestProviderTemplates(t *testing.T) {
	tests := []struct {
		provider Provider
		pkg      string
		version  string
		path     string
		wantPkg  string
		wantMod  string
	}{
		{
			EsmSh,
			"lit",
			"3.0.0",
			"index.js",
			"https://esm.sh/lit@3.0.0/package.json",
			"https://esm.sh/lit@3.0.0/index.js",
		},
		{
			Unpkg,
			"@preact/signals",
			"1.0.0",
			"dist/signals.mjs",
			"https://unpkg.com/@preact/signals@1.0.0/package.json",
			"https://unpkg.com/@preact/signals@1.0.0/dist/signals.mjs",
		},
		{
			Jsdelivr,
			"lodash-es",
			"4.17.21",
			"lodash.js",
			"https://cdn.jsdelivr.net/npm/lodash-es@4.17.21/package.json",
			"https://cdn.jsdelivr.net/npm/lodash-es@4.17.21/lodash.js",
		},
	}

	for _, tt := range tests {
		t.Run(tt.provider.Name+"/"+tt.pkg, func(t *testing.T) {
			// Test package.json template
			pkgURL := tt.provider.PackageJSONTemplate
			pkgURL = expandTemplate(pkgURL, tt.pkg, tt.version, "")
			if pkgURL != tt.wantPkg {
				t.Errorf("PackageJSONTemplate expansion = %q, want %q", pkgURL, tt.wantPkg)
			}

			// Test module template
			modURL := tt.provider.ModuleTemplate
			modURL = expandTemplate(modURL, tt.pkg, tt.version, tt.path)
			if modURL != tt.wantMod {
				t.Errorf("ModuleTemplate expansion = %q, want %q", modURL, tt.wantMod)
			}
		})
	}
}

// expandTemplate is a helper for testing template expansion.
func expandTemplate(tmpl, pkg, version, path string) string {
	result := tmpl
	result = replaceAll(result, "{package}", pkg)
	result = replaceAll(result, "{version}", version)
	result = replaceAll(result, "{path}", path)
	return result
}

func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}

func indexOf(s, substr string) int {
	for i := range len(s) - len(substr) + 1 {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
