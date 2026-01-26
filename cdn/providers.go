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

// Provider represents a CDN provider with URL templates for package resolution.
type Provider struct {
	Name string
	// PackageJSONTemplate is the URL template for fetching package.json files.
	// Variables: {package}, {version}
	PackageJSONTemplate string
	// ModuleTemplate is the URL template for module URLs in the import map.
	// Variables: {package}, {version}, {path}
	ModuleTemplate string
}

// Predefined CDN providers
var (
	// EsmSh is the esm.sh CDN provider.
	EsmSh = Provider{
		Name:                "esm.sh",
		PackageJSONTemplate: "https://esm.sh/{package}@{version}/package.json",
		ModuleTemplate:      "https://esm.sh/{package}@{version}/{path}",
	}

	// Unpkg is the unpkg CDN provider.
	Unpkg = Provider{
		Name:                "unpkg",
		PackageJSONTemplate: "https://unpkg.com/{package}@{version}/package.json",
		ModuleTemplate:      "https://unpkg.com/{package}@{version}/{path}",
	}

	// Jsdelivr is the jsDelivr CDN provider.
	Jsdelivr = Provider{
		Name:                "jsdelivr",
		PackageJSONTemplate: "https://cdn.jsdelivr.net/npm/{package}@{version}/package.json",
		ModuleTemplate:      "https://cdn.jsdelivr.net/npm/{package}@{version}/{path}",
	}
)

// DefaultProvider is the default CDN provider (esm.sh).
var DefaultProvider = EsmSh

// ProviderByName returns a CDN provider by name.
// Returns nil if the provider name is not recognized.
func ProviderByName(name string) *Provider {
	switch name {
	case "esm.sh", "esmsh", "esm":
		return &EsmSh
	case "unpkg":
		return &Unpkg
	case "jsdelivr", "jsdelivr.net", "cdn.jsdelivr.net":
		return &Jsdelivr
	default:
		return nil
	}
}

// ProviderNames returns a list of supported CDN provider names.
func ProviderNames() []string {
	return []string{"esm.sh", "unpkg", "jsdelivr"}
}

// IsValidProvider returns true if the provider name is recognized.
// It honors the same aliases as ProviderByName.
func IsValidProvider(name string) bool {
	return ProviderByName(name) != nil
}
