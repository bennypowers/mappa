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
package version

import "testing"

func TestVersionStringUsesLDFLAGSValue(t *testing.T) {
	old := Version
	Version = "v9.9.9"
	t.Cleanup(func() { Version = old })

	if got := VersionString(); got != "v9.9.9" {
		t.Errorf("VersionString() = %q, want %q", got, "v9.9.9")
	}
}

func TestBuildInfo(t *testing.T) {
	info := BuildInfo()
	for _, key := range []string{"version", "gitCommit", "gitTag", "buildTime", "gitDirty"} {
		if _, ok := info[key]; !ok {
			t.Errorf("BuildInfo() missing %q: %v", key, info)
		}
	}
	if info["version"] != VersionString() {
		t.Errorf("BuildInfo version = %q, want %q", info["version"], VersionString())
	}
}
