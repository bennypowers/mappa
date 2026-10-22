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
package local

import "testing"

func TestValidPackageName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"..", false},
		{`..\evil`, false},
		{`pkg\evil`, false},
		{"pkg:evil", false},
		{"pkg\x00evil", false},
		{"@scope/a/b", false},
		{"@/x", false},
		{"@scope/", false},
		{"@", false},
		{"/abs", false},
		{"a/b", false},
		{"lit", true},
		{"fuse.js", true},
		{"@scope/pkg", true},
	}
	for _, tt := range tests {
		if got := validPackageName(tt.name); got != tt.valid {
			t.Errorf("validPackageName(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}
