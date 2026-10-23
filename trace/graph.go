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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
)

// ModuleTraceError reports a module that could not be traced because it could
// not be read or parsed, or because it lives outside the allowed roots. These
// errors make a generated import map incomplete, so callers treat them as
// fatal. Resolution failures (for example an export not listed in
// package.json) are recorded as plain errors and remain non-fatal warnings.
type ModuleTraceError struct {
	// Path is the module path that failed.
	Path string
	// Err is the underlying failure.
	Err error
}

// Error keeps the historical "tracing <path>: <cause>" wording.
func (e *ModuleTraceError) Error() string {
	return fmt.Sprintf("tracing %s: %v", e.Path, e.Err)
}

// Unwrap returns the underlying error.
func (e *ModuleTraceError) Unwrap() error { return e.Err }

// relativize rewrites paths that live under root to be relative to it.
// It is a no-op for paths outside root.
func (e *ModuleTraceError) relativize(root string) error {
	rel := relativizePath(root, e.Path)
	inner := e.Err
	if inner != nil && root != "" {
		// Underlying filesystem errors often embed the absolute path too.
		text := strings.ReplaceAll(inner.Error(), root+string(filepath.Separator), "")
		inner = errors.New(text)
	}
	return &ModuleTraceError{Path: rel, Err: inner}
}

// ClassifyGraphErrors splits graph errors into fatal module tracing failures
// and non-fatal resolution warnings.
func ClassifyGraphErrors(errs []error) (fatal, nonFatal []error) {
	for _, err := range errs {
		var moduleErr *ModuleTraceError
		if errors.As(err, &moduleErr) {
			fatal = append(fatal, err)
			continue
		}
		nonFatal = append(nonFatal, err)
	}
	return fatal, nonFatal
}

// RelativizeGraphErrors rewrites absolute paths under root to relative paths
// in error text so CLI output stays portable. Fatal errors keep their type.
func RelativizeGraphErrors(errs []error, root string) []error {
	if root == "" {
		return errs
	}
	out := make([]error, len(errs))
	for i, err := range errs {
		var moduleErr *ModuleTraceError
		if errors.As(err, &moduleErr) {
			out[i] = moduleErr.relativize(root)
			continue
		}
		out[i] = errors.New(strings.ReplaceAll(err.Error(), root+string(filepath.Separator), ""))
	}
	return out
}

// relativizePath returns p relative to root when p is inside root.
func relativizePath(root, p string) string {
	if root == "" {
		return p
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return rel
}

// ModuleGraph represents the complete module dependency graph.
type ModuleGraph struct {
	// Entrypoints are the starting modules (from HTML scripts or explicit entry)
	Entrypoints []string

	// Modules maps module paths to their parsed information
	Modules map[string]*Module

	// Errors collects errors encountered during tracing. ModuleTraceError
	// values are fatal (the map is incomplete); other errors are non-fatal
	// resolution warnings.
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
	logger          resolve.Logger
	nodeModulesPath string                   // Path to node_modules for resolving bare specifiers
	followBare      bool                     // Whether to follow bare specifier imports into node_modules
	selfPkg         *packagejson.PackageJSON // Current package for self-referencing imports
	selfPkgPath     string                   // Path to current package root
	conditions      []string                 // Export condition priority
	// allowedPaths are additional roots that may be traced, such as
	// discovered workspace package directories.
	allowedPaths []string

	// pkgCache caches parsed package.json files by path (thread-safe).
	// It can be shared with a resolver so each manifest is parsed once.
	pkgCache packagejson.Cache
	// moduleCache caches traced modules by path (thread-safe).
	// Used for cross-file caching in batch mode.
	moduleCache *sync.Map // map[string]*Module
}

// NewTracer creates a new Tracer for the given root directory.
func NewTracer(fs fs.FileSystem, rootDir string) *Tracer {
	return &Tracer{
		fs:          fs,
		rootDir:     rootDir,
		pkgCache:    packagejson.NewMemoryCache(),
		moduleCache: &sync.Map{},
	}
}

// clone returns a shallow copy sharing the underlying caches.
func (t *Tracer) clone() *Tracer {
	c := *t
	c.conditions = slices.Clone(t.conditions)
	c.allowedPaths = slices.Clone(t.allowedPaths)
	return &c
}

// WithLogger returns a new Tracer that logs warnings to the given logger.
func (t *Tracer) WithLogger(logger resolve.Logger) *Tracer {
	c := t.clone()
	c.logger = logger
	return c
}

// WithConditions returns a new Tracer that uses the given export conditions.
func (t *Tracer) WithConditions(conditions []string) *Tracer {
	c := t.clone()
	c.conditions = slices.Clone(conditions)
	return c
}

// WithPackageCache returns a new Tracer that uses the given package.json cache.
func (t *Tracer) WithPackageCache(cache packagejson.Cache) *Tracer {
	c := t.clone()
	c.pkgCache = cache
	return c
}

// WithNodeModules returns a new Tracer that resolves bare specifiers from the given
// node_modules path. When set, the tracer will follow transitive dependencies.
func (t *Tracer) WithNodeModules(nodeModulesPath string) *Tracer {
	c := t.clone()
	c.nodeModulesPath = nodeModulesPath
	c.followBare = true
	return c
}

// WithAllowedPaths returns a new Tracer that may also trace modules under the
// given roots. NewSetup uses it for discovered workspace package directories.
func (t *Tracer) WithAllowedPaths(paths []string) *Tracer {
	c := t.clone()
	c.allowedPaths = slices.Clone(paths)
	return c
}

// WithSelfPackage returns a new Tracer that recognizes imports of the given package
// as self-references and resolves them locally. This is used when tracing a package
// that imports itself (e.g., @rhds/elements importing @rhds/elements/rh-button/rh-button.js).
func (t *Tracer) WithSelfPackage(pkg *packagejson.PackageJSON, pkgPath string) *Tracer {
	c := t.clone()
	c.selfPkg = pkg
	c.selfPkgPath = pkgPath
	return c
}

// TraceHTML parses an HTML file and traces all module scripts.
func (t *Tracer) TraceHTML(htmlPath string) (*ModuleGraph, error) {
	if info, err := t.fs.Stat(htmlPath); err == nil && info.Size() > maxHTMLSize {
		return nil, fmt.Errorf("HTML file %s exceeds maximum size (%d bytes)", htmlPath, maxHTMLSize)
	}
	content, err := t.fs.ReadFile(htmlPath)
	if err != nil {
		return nil, err
	}
	if len(content) > maxHTMLSize {
		return nil, fmt.Errorf("HTML file %s exceeds maximum size (%d bytes)", htmlPath, maxHTMLSize)
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
			if isURLSpecifier(script.Src) || isSubpathImport(script.Src) {
				continue
			}
			modulePath := t.resolvePath(htmlDir, script.Src)
			graph.Entrypoints = append(graph.Entrypoints, modulePath)
			if err := t.traceModule(graph, modulePath); err != nil {
				graph.Errors = append(graph.Errors, &ModuleTraceError{Path: modulePath, Err: err})
				continue
			}
		} else if script.Inline {
			// Inline module - collect its imports
			for _, imp := range script.Imports {
				if isSubpathImport(imp) || isURLSpecifier(imp) {
					continue
				}
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
								graph.Errors = append(graph.Errors, &ModuleTraceError{Path: depPath, Err: err})
								continue
							}
						}
					}
				} else {
					// Relative import from inline script
					modulePath := t.resolvePath(htmlDir, imp)
					if err := t.traceModule(graph, modulePath); err != nil {
						graph.Errors = append(graph.Errors, &ModuleTraceError{Path: modulePath, Err: err})
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

// maxModuleSize caps the size of a module read during tracing.
const maxModuleSize = 10 << 20 // 10 MiB

// maxHTMLSize caps the size of an HTML document read during tracing.
const maxHTMLSize = 10 << 20 // 10 MiB

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
		if !t.allowPath(modulePath) {
			return fmt.Errorf("refusing to read module outside allowed roots: %s", modulePath)
		}
		if info, err := t.fs.Stat(modulePath); err == nil && info.Size() > maxModuleSize {
			return fmt.Errorf("module %s exceeds maximum size (%d bytes)", modulePath, maxModuleSize)
		}
		content, err := t.fs.ReadFile(modulePath)
		if err != nil {
			return err
		}
		if len(content) > maxModuleSize {
			return fmt.Errorf("module %s exceeds maximum size (%d bytes)", modulePath, maxModuleSize)
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
		if isSubpathImport(imp.Specifier) || isURLSpecifier(imp.Specifier) {
			continue
		}
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
						graph.Errors = append(graph.Errors, &ModuleTraceError{Path: depPath, Err: err})
						continue
					}
				}
			}
		} else {
			// Relative or absolute path - resolve and trace
			depPath := t.resolvePath(moduleDir, imp.Specifier)
			if err := t.traceModule(graph, depPath); err != nil {
				graph.Errors = append(graph.Errors, &ModuleTraceError{Path: depPath, Err: err})
				continue
			}
		}
	}

	return nil
}

// getPackageJSON returns a cached package.json, parsing and caching it if needed.
// Returns nil if the package.json doesn't exist or can't be parsed.
// Parse errors (as opposed to missing files) are logged for debugging.
func (t *Tracer) getPackageJSON(pkgJSONPath string) *packagejson.PackageJSON {
	// Check cache first (a cached nil is a negative result).
	if pkg, ok := t.pkgCache.Get(pkgJSONPath); ok {
		return pkg
	}

	// Parse and cache
	pkg, err := packagejson.ParseFile(t.fs, pkgJSONPath)
	if err != nil {
		// Log parse errors (not missing files) to help with debugging
		if !os.IsNotExist(err) && t.logger != nil {
			t.logger.Warning("failed to parse %s: %v", pkgJSONPath, err)
		}
		t.pkgCache.Set(pkgJSONPath, nil) // Cache negative result
		return nil
	}
	t.pkgCache.Set(pkgJSONPath, pkg)
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

	return resolvePackageSubpath(pkg, pkgPath, subpath, t.resolveOpts())
}

// resolveOpts returns export resolution options for the tracer's conditions.
func (t *Tracer) resolveOpts() *packagejson.ResolveOptions {
	if len(t.conditions) == 0 {
		return nil
	}
	return &packagejson.ResolveOptions{Conditions: t.conditions}
}

// resolveSelfImport resolves an import of the current package (self-reference).
// The subpath should already be normalized to start with "./" or be ".".
func (t *Tracer) resolveSelfImport(subpath string) (string, error) {
	if t.selfPkg == nil {
		return "", nil
	}
	return resolvePackageSubpath(t.selfPkg, t.selfPkgPath, subpath, t.resolveOpts())
}

// resolvePackageSubpath resolves a subpath within a package directory,
// using exports resolution first with fallback to direct path only if no exports are defined.
func resolvePackageSubpath(pkg *packagejson.PackageJSON, pkgPath, subpath string, opts *packagejson.ResolveOptions) (string, error) {
	// ResolveExport handles the no-exports fallback (module, main, index.js).
	resolved, err := pkg.ResolveExport(subpath, opts)
	if err == nil {
		return filepath.Join(pkgPath, resolved), nil
	}

	// If package has exports defined, enforce export restrictions - don't fall back
	if pkg.Exports != nil {
		return "", err
	}

	// No exports defined - fall back to direct subpath resolution.
	// Subpath "." is handled by ResolveExport above.
	return filepath.Join(pkgPath, strings.TrimPrefix(subpath, "./")), nil
}

// splitURLSuffix separates a query string or fragment from a URL/path specifier.
func splitURLSuffix(specifier string) string {
	if i := strings.IndexAny(specifier, "?#"); i >= 0 {
		return specifier[:i]
	}
	return specifier
}

// resolvePath resolves a specifier relative to a base directory.
// For web-style paths:
// - "./foo" and "../foo" are resolved relative to baseDir
// - "/foo" is resolved relative to rootDir (the document/site root)
// Query strings and fragments are stripped before filesystem resolution.
func (t *Tracer) resolvePath(baseDir, specifier string) string {
	specifier = splitURLSuffix(specifier)
	if strings.HasPrefix(specifier, "/") {
		// Web-style absolute path - relative to the document/site root
		return filepath.Join(t.rootDir, specifier)
	}
	// Relative path (./ or ../ or no prefix)
	return filepath.Join(baseDir, specifier)
}

// containsPath reports whether p is inside root after canonicalization.
func containsPath(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// allowPath applies containment checks for module tracing. Paths must live
// under the package root, the workspace node_modules tree, or a discovered
// workspace package directory.
//
// Symlinks are not resolved: fs.FileSystem exposes no Lstat/Readlink
// operation, so a symlink planted inside an allowed root could point outside
// it and this check would not notice. Callers that need that guarantee must
// supply a filesystem whose Stat already resolves links to the real path or
// reject escaping links before tracing.
// allowPath reports whether p is inside the tracer's allowed roots. When the
// FileSystem implements fs.SymlinkResolver, both p and the roots are resolved
// first so a link cannot escape. Resolution happens before the read; a path
// swapped for a symlink after this check is still subject to that race.
func (t *Tracer) allowPath(p string) bool {
	resolved := resolveSymlinks(t.fs, p)
	roots := make([]string, 0, len(t.allowedPaths)+2)
	roots = append(roots, t.rootDir)
	if t.nodeModulesPath != "" {
		roots = append(roots, t.nodeModulesPath)
	}
	roots = append(roots, t.allowedPaths...)
	for _, root := range roots {
		if containsPath(resolveSymlinks(t.fs, root), resolved) {
			return true
		}
	}
	return false
}

// resolveSymlinks resolves p when the FileSystem supports it. File systems
// without SymlinkResolver, and paths that cannot be resolved (for example
// because they do not exist yet), fall back to the lexical path.
func resolveSymlinks(fsys fs.FileSystem, p string) string {
	resolver, ok := fsys.(fs.SymlinkResolver)
	if !ok {
		return p
	}
	resolved, err := resolver.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return resolved
}

// isURLSpecifier reports whether the specifier has a URL scheme (e.g. node:, data:, https:).
func isURLSpecifier(specifier string) bool {
	if strings.HasPrefix(specifier, "//") {
		return true
	}
	i := strings.IndexByte(specifier, ':')
	if i <= 0 {
		return false
	}
	for j, r := range specifier[:i] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case j > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

// isSubpathImport reports whether the specifier is a package.json "#imports" alias.
func isSubpathImport(specifier string) bool {
	return strings.HasPrefix(specifier, "#")
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
	if isSubpathImport(specifier) || isURLSpecifier(specifier) {
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
	return resolve.PackageName(specifier)
}
