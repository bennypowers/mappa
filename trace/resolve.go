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

package trace

import (
	"strings"

	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve/local"
)

// ResolveForSpecifiers builds a minimal import map containing only the given
// bare specifiers, their exact resolutions, and the scopes reachable from
// them. It is shared by trace and inject so both produce identical maps.
//
// base should already be configured with the desired template, conditions,
// logger, and package cache. rootPkg is used to point self-references at the
// package source rather than node_modules.
func ResolveForSpecifiers(base *local.Resolver, rootDir string, bareSpecs []string, rootPkg *packagejson.PackageJSON) (*importmap.ImportMap, error) {
	if len(bareSpecs) == 0 {
		return &importmap.ImportMap{Imports: make(map[string]string)}, nil
	}

	resolver := base.WithPackages(bareSpecs)
	if referencesPackage(bareSpecs, rootPkg) {
		resolver = resolver.WithIncludeRootExports()
	}

	// Generate the full map for scopes and trailing-slash keys, then keep only
	// what the traced specifiers can reach.
	generatedMap, err := resolver.Resolve(rootDir)
	if err != nil {
		return nil, err
	}

	tracedImports := resolver.ResolveSpecifiers(rootDir, bareSpecs)
	overlayGeneratedImports(tracedImports, generatedMap, bareSpecs)

	for key, value := range generatedMap.Imports {
		if strings.HasSuffix(key, "/") {
			tracedImports[key] = value
		}
	}

	return (&importmap.ImportMap{
		Imports: tracedImports,
		Scopes:  filterReachableScopes(generatedMap.Scopes, tracedImports),
	}).Simplify(), nil
}

// referencesPackage reports whether any specifier imports pkg by name.
func referencesPackage(specs []string, pkg *packagejson.PackageJSON) bool {
	if pkg == nil || pkg.Name == "" {
		return false
	}
	for _, spec := range specs {
		if spec == pkg.Name || strings.HasPrefix(spec, pkg.Name+"/") {
			return true
		}
	}
	return false
}

// filterReachableScopes keeps only scopes that can apply to a module reachable
// from the given imports. Reachability is computed over the URLs in the map:
// a scope applies when its key is a prefix of a reachable URL, and every value
// inside a reachable scope then becomes reachable in turn.
func filterReachableScopes(scopes map[string]map[string]string, imports map[string]string) map[string]map[string]string {
	if len(scopes) == 0 {
		return nil
	}

	reachableURLs := make(map[string]bool, len(imports))
	queue := make([]string, 0, len(imports))
	for _, url := range imports {
		if !reachableURLs[url] {
			reachableURLs[url] = true
			queue = append(queue, url)
		}
	}

	reachable := make(map[string]map[string]string)
	for len(queue) > 0 {
		url := queue[0]
		queue = queue[1:]

		for scopeKey, entries := range scopes {
			if _, ok := reachable[scopeKey]; ok {
				continue
			}
			if !strings.HasPrefix(url, scopeKey) {
				continue
			}
			reachable[scopeKey] = entries
			for _, value := range entries {
				if !reachableURLs[value] {
					reachableURLs[value] = true
					queue = append(queue, value)
				}
			}
		}
	}

	if len(reachable) == 0 {
		return nil
	}
	return reachable
}
