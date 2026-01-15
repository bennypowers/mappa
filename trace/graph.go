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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/packagejson"
)

// ModuleGraph represents the complete module dependency graph.
type ModuleGraph struct {
	// Entrypoints are the starting modules (from HTML scripts or explicit entry)
	Entrypoints []string

	// Modules maps module paths to their parsed information
	Modules map[string]*Module

	// Errors collects non-fatal errors encountered during tracing
	Errors []error

	// bareSpecifiers collects all bare import specifiers (need to be resolved)
	bareSpecifiers map[string]bool
}

// Module represents a parsed module in the graph.
type Module struct {
	Path    string         // Path to the module file
	Imports []ModuleImport // All imports found in the module
	Traced  bool           // Whether this module has been fully traced
}

// Tracer builds module graphs from HTML and JavaScript entrypoints.
type Tracer struct {
	fs              fs.FileSystem
	rootDir         string
	nodeModulesPath string // Path to node_modules for resolving bare specifiers
	followBare      bool   // Whether to follow bare specifier imports into node_modules
	selfPkg         *packagejson.PackageJSON // Current package for self-referencing imports
	selfPkgPath     string                   // Path to current package root

	// pkgCache caches parsed package.json files by path (thread-safe).
	// Pointer is used so caches can be shared across builder method calls.
	pkgCache *sync.Map // map[string]*packagejson.PackageJSON
	// moduleCache caches traced modules by path (thread-safe).
	// Used for cross-file caching in batch mode.
	moduleCache *sync.Map // map[string]*Module
}

// NewTracer creates a new Tracer for the given root directory.
func NewTracer(fs fs.FileSystem, rootDir string) *Tracer {
	return &Tracer{
		fs:          fs,
		rootDir:     rootDir,
		pkgCache:    &sync.Map{},
		moduleCache: &sync.Map{},
	}
}

// WithNodeModules returns a new Tracer that resolves bare specifiers from the given
// node_modules path. When set, the tracer will follow transitive dependencies.
func (t *Tracer) WithNodeModules(nodeModulesPath string) *Tracer {
	return &Tracer{
		fs:              t.fs,
		rootDir:         t.rootDir,
		nodeModulesPath: nodeModulesPath,
		followBare:      true,
		selfPkg:         t.selfPkg,
		selfPkgPath:     t.selfPkgPath,
		pkgCache:        t.pkgCache,
		moduleCache:     t.moduleCache,
	}
}

// WithSelfPackage returns a new Tracer that recognizes imports of the given package
// as self-references and resolves them locally. This is used when tracing a package
// that imports itself (e.g., @rhds/elements importing @rhds/elements/rh-button/rh-button.js).
func (t *Tracer) WithSelfPackage(pkg *packagejson.PackageJSON, pkgPath string) *Tracer {
	return &Tracer{
		fs:              t.fs,
		rootDir:         t.rootDir,
		nodeModulesPath: t.nodeModulesPath,
		followBare:      t.followBare,
		selfPkg:         pkg,
		selfPkgPath:     pkgPath,
		pkgCache:        t.pkgCache,
		moduleCache:     t.moduleCache,
	}
}

// TraceHTML parses an HTML file and traces all module scripts.
func (t *Tracer) TraceHTML(htmlPath string) (*ModuleGraph, error) {
	content, err := t.fs.ReadFile(htmlPath)
	if err != nil {
		return nil, err
	}

	scripts, err := ExtractScripts(content)
	if err != nil {
		return nil, err
	}

	graph := &ModuleGraph{
		Modules:        make(map[string]*Module),
		bareSpecifiers: make(map[string]bool),
	}

	htmlDir := filepath.Dir(htmlPath)

	for _, script := range scripts {
		if script.Type != "module" {
			continue
		}

		if script.Src != "" {
			// External module script - trace it
			modulePath := t.resolvePath(htmlDir, script.Src)
			graph.Entrypoints = append(graph.Entrypoints, modulePath)
			if err := t.traceModule(graph, modulePath); err != nil {
				graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", modulePath, err))
				continue
			}
		} else if script.Inline {
			// Inline module - collect its imports
			for _, imp := range script.Imports {
				if isBareSpecifier(imp) {
					graph.bareSpecifiers[imp] = true

					// Follow bare specifiers into node_modules if configured
					if t.followBare {
						depPath, err := t.resolveBareSpecifier(imp)
						if err != nil {
							graph.Errors = append(graph.Errors, fmt.Errorf("resolving %s: %w", imp, err))
							continue
						}
						if depPath != "" {
							if err := t.traceModule(graph, depPath); err != nil {
								graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", depPath, err))
								continue
							}
						}
					}
				} else {
					// Relative import from inline script
					modulePath := t.resolvePath(htmlDir, imp)
					if err := t.traceModule(graph, modulePath); err != nil {
						graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", modulePath, err))
						continue
					}
				}
			}
		}
	}

	return graph, nil
}

// TraceModule traces a single module and all its dependencies.
func (t *Tracer) TraceModule(modulePath string) (*ModuleGraph, error) {
	graph := &ModuleGraph{
		Entrypoints:    []string{modulePath},
		Modules:        make(map[string]*Module),
		bareSpecifiers: make(map[string]bool),
	}

	if err := t.traceModule(graph, modulePath); err != nil {
		return nil, err
	}

	return graph, nil
}

// traceModule recursively traces a module and its dependencies.
func (t *Tracer) traceModule(graph *ModuleGraph, modulePath string) error {
	// Already traced in this graph?
	if mod, exists := graph.Modules[modulePath]; exists && mod.Traced {
		return nil
	}

	// Try to get cached module (avoid re-parsing)
	var mod *Module
	if cached, ok := t.moduleCache.Load(modulePath); ok {
		// Use cached module info (imports are already parsed)
		cachedMod := cached.(*Module)
		mod = &Module{
			Path:    cachedMod.Path,
			Imports: cachedMod.Imports,
			Traced:  true,
		}
	} else {
		// Read and parse the module
		content, err := t.fs.ReadFile(modulePath)
		if err != nil {
			return err
		}

		imports, err := ExtractImports(content)
		if err != nil {
			return err
		}

		mod = &Module{
			Path:    modulePath,
			Imports: imports,
			Traced:  true,
		}

		// Cache the parsed module for reuse across graphs
		t.moduleCache.Store(modulePath, mod)
	}

	// Record this module in this graph
	graph.Modules[modulePath] = mod

	// Process imports
	moduleDir := filepath.Dir(modulePath)
	for _, imp := range mod.Imports {
		if isBareSpecifier(imp.Specifier) {
			graph.bareSpecifiers[imp.Specifier] = true

			// Follow bare specifiers into node_modules if configured
			if t.followBare {
				depPath, err := t.resolveBareSpecifier(imp.Specifier)
				if err != nil {
					graph.Errors = append(graph.Errors, fmt.Errorf("resolving %s: %w", imp.Specifier, err))
					continue
				}
				if depPath != "" {
					if err := t.traceModule(graph, depPath); err != nil {
						graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", depPath, err))
						continue
					}
				}
			}
		} else {
			// Relative or absolute path - resolve and trace
			depPath := t.resolvePath(moduleDir, imp.Specifier)
			if err := t.traceModule(graph, depPath); err != nil {
				graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", depPath, err))
				continue
			}
		}
	}

	return nil
}

// getPackageJSON returns a cached package.json, parsing and caching it if needed.
// Returns nil if the package.json doesn't exist or can't be parsed.
// Parse errors (as opposed to missing files) are logged to stderr for debugging.
func (t *Tracer) getPackageJSON(pkgJSONPath string) *packagejson.PackageJSON {
	// Check cache first
	if cached, ok := t.pkgCache.Load(pkgJSONPath); ok {
		if cached == nil {
			return nil // Cached negative result
		}
		return cached.(*packagejson.PackageJSON)
	}

	// Parse and cache
	pkg, err := packagejson.ParseFile(t.fs, pkgJSONPath)
	if err != nil {
		// Log parse errors (not missing files) to help with debugging
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", pkgJSONPath, err)
		}
		t.pkgCache.Store(pkgJSONPath, nil) // Cache negative result
		return nil
	}
	t.pkgCache.Store(pkgJSONPath, pkg)
	return pkg
}

// resolveBareSpecifier resolves a bare specifier to a file path.
// First checks if the specifier is a self-reference (current package importing itself),
// then falls back to node_modules resolution.
// Returns empty string if the specifier cannot be resolved.
func (t *Tracer) resolveBareSpecifier(specifier string) (string, error) {
	pkgName := getPackageName(specifier)
	subpath := strings.TrimPrefix(specifier, pkgName)
	if subpath == "" {
		subpath = "."
	} else {
		// Convert "/subpath" to "./subpath"
		subpath = "." + subpath
	}

	// Check for self-referencing import (package imports itself)
	if t.selfPkg != nil && pkgName == t.selfPkg.Name {
		return t.resolveSelfImport(subpath)
	}

	// Fall back to node_modules resolution
	if t.nodeModulesPath == "" {
		return "", nil
	}

	// Load package.json from node_modules (cached)
	pkgPath := filepath.Join(t.nodeModulesPath, pkgName)
	pkgJSONPath := filepath.Join(pkgPath, "package.json")
	pkg := t.getPackageJSON(pkgJSONPath)
	if pkg == nil {
		// Package not found - can't follow
		return "", nil
	}

	return resolvePackageSubpath(pkg, pkgPath, subpath)
}

// resolveSelfImport resolves an import of the current package (self-reference).
// The subpath should already be normalized to start with "./" or be ".".
func (t *Tracer) resolveSelfImport(subpath string) (string, error) {
	if t.selfPkg == nil {
		return "", nil
	}
	return resolvePackageSubpath(t.selfPkg, t.selfPkgPath, subpath)
}

// resolvePackageSubpath resolves a subpath within a package directory,
// using exports resolution first with fallback to direct path only if no exports are defined.
func resolvePackageSubpath(pkg *packagejson.PackageJSON, pkgPath, subpath string) (string, error) {
	// Try to resolve through exports
	resolved, err := pkg.ResolveExport(subpath, nil)
	if err == nil {
		return filepath.Join(pkgPath, resolved), nil
	}

	// If package has exports defined, enforce export restrictions - don't fall back
	if pkg.Exports != nil {
		return "", err
	}

	// No exports defined - fall back to direct subpath resolution
	if subpath == "." {
		if pkg.Main != "" {
			return filepath.Join(pkgPath, strings.TrimPrefix(pkg.Main, "./")), nil
		}
		return filepath.Join(pkgPath, "index.js"), nil
	}

	// Use subpath directly
	return filepath.Join(pkgPath, strings.TrimPrefix(subpath, "./")), nil
}

// resolvePath resolves a specifier relative to a base directory.
// For web-style paths:
// - "./foo" and "../foo" are resolved relative to baseDir
// - "/foo" is resolved relative to rootDir (web-style absolute)
func (t *Tracer) resolvePath(baseDir, specifier string) string {
	if strings.HasPrefix(specifier, "/") {
		// Web-style absolute path - relative to root
		return filepath.Join(t.rootDir, specifier)
	}
	// Relative path (./ or ../ or no prefix)
	return filepath.Join(baseDir, specifier)
}

// isBareSpecifier returns true if the specifier is a bare module specifier
// (needs to be resolved via import map or node_modules).
func isBareSpecifier(specifier string) bool {
	// Bare specifiers don't start with ./, ../, or /
	if specifier == "" {
		return false
	}
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
		return false
	}
	if strings.HasPrefix(specifier, "/") {
		return false
	}
	// Check for URL schemes
	if strings.Contains(specifier, "://") {
		return false
	}
	return true
}

// BareSpecifiers returns a sorted slice of all bare specifiers found.
func (g *ModuleGraph) BareSpecifiers() []string {
	specifiers := make([]string, 0, len(g.bareSpecifiers))
	for spec := range g.bareSpecifiers {
		specifiers = append(specifiers, spec)
	}
	sort.Strings(specifiers)
	return specifiers
}

// PackageNames extracts sorted package names from bare specifiers.
// e.g., "lit/decorators.js" -> "lit"
func (g *ModuleGraph) PackageNames() []string {
	packages := make(map[string]bool)
	for spec := range g.bareSpecifiers {
		pkgName := getPackageName(spec)
		packages[pkgName] = true
	}

	result := make([]string, 0, len(packages))
	for pkg := range packages {
		result = append(result, pkg)
	}
	sort.Strings(result)
	return result
}

// getPackageName extracts the package name from a bare specifier.
func getPackageName(specifier string) string {
	// Handle scoped packages: @scope/package/path -> @scope/package
	if strings.HasPrefix(specifier, "@") {
		parts := strings.SplitN(specifier, "/", 3)
		if len(parts) >= 2 {
			return path.Join(parts[0], parts[1])
		}
		return specifier
	}
	// Regular package: package/path -> package
	parts := strings.SplitN(specifier, "/", 2)
	return parts[0]
}
