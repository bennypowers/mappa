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
// Package local provides a resolver for local node_modules paths.
package local

import (
	"maps"
	"path/filepath"
	"strings"
	"sync"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
)

// Resolver generates import maps pointing to local node_modules paths.
type Resolver struct {
	fs                 fs.FileSystem
	logger             resolve.Logger
	additionalPackages []string
	template           *resolve.Template
	inputMap           *importmap.ImportMap
	workspacePackages  []resolve.WorkspacePackage
	includeRootExports bool
	cache              packagejson.Cache
	conditions         []string // export condition priority
}

// New creates a new local Resolver.
func New(fs fs.FileSystem, logger resolve.Logger) *Resolver {
	// DefaultLocalTemplate is a known-valid constant; error is impossible
	tmpl, _ := resolve.ParseTemplate(resolve.DefaultLocalTemplate)
	return &Resolver{
		fs:       fs,
		logger:   logger,
		template: tmpl,
	}
}

// WithPackages returns a new Resolver that includes additional packages
// beyond those listed in package.json dependencies.
func (r *Resolver) WithPackages(packages []string) *Resolver {
	return &Resolver{
		fs:                 r.fs,
		logger:             r.logger,
		additionalPackages: packages,
		template:           r.template,
		inputMap:           r.inputMap,
		workspacePackages:  r.workspacePackages,
		includeRootExports: r.includeRootExports,
		cache:              r.cache,
		conditions:         r.conditions,
	}
}

// WithTemplate returns a new Resolver that uses the specified URL template.
func (r *Resolver) WithTemplate(pattern string) (*Resolver, error) {
	tmpl, err := resolve.ParseTemplate(pattern)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		fs:                 r.fs,
		logger:             r.logger,
		additionalPackages: r.additionalPackages,
		template:           tmpl,
		inputMap:           r.inputMap,
		workspacePackages:  r.workspacePackages,
		includeRootExports: r.includeRootExports,
		cache:              r.cache,
		conditions:         r.conditions,
	}, nil
}

// WithInputMap returns a new Resolver that merges the given import map
// with the generated output. Input map entries take precedence.
func (r *Resolver) WithInputMap(im *importmap.ImportMap) *Resolver {
	return &Resolver{
		fs:                 r.fs,
		logger:             r.logger,
		additionalPackages: r.additionalPackages,
		template:           r.template,
		inputMap:           im,
		workspacePackages:  r.workspacePackages,
		includeRootExports: r.includeRootExports,
		cache:              r.cache,
		conditions:         r.conditions,
	}
}

// WithWorkspacePackages returns a new Resolver configured for workspace mode.
// When workspace packages are provided, they are added to global imports and
// their dependencies are collected for node_modules resolution.
func (r *Resolver) WithWorkspacePackages(packages []resolve.WorkspacePackage) *Resolver {
	return &Resolver{
		fs:                 r.fs,
		logger:             r.logger,
		additionalPackages: r.additionalPackages,
		template:           r.template,
		inputMap:           r.inputMap,
		workspacePackages:  packages,
		includeRootExports: r.includeRootExports,
		cache:              r.cache,
		conditions:         r.conditions,
	}
}

// WithIncludeRootExports returns a new Resolver that includes the root package's
// own exports in the import map. This is useful for development servers where
// you want to import the package you're developing by name.
func (r *Resolver) WithIncludeRootExports() *Resolver {
	return &Resolver{
		fs:                 r.fs,
		logger:             r.logger,
		additionalPackages: r.additionalPackages,
		template:           r.template,
		inputMap:           r.inputMap,
		workspacePackages:  r.workspacePackages,
		includeRootExports: true,
		cache:              r.cache,
		conditions:         r.conditions,
	}
}

// WithPackageCache returns a new Resolver that uses the provided cache
// for parsed package.json files. This allows callers to reuse parsed data
// across multiple resolution calls, improving performance for hot-reload
// or monorepo scenarios.
func (r *Resolver) WithPackageCache(cache packagejson.Cache) *Resolver {
	return &Resolver{
		fs:                 r.fs,
		logger:             r.logger,
		additionalPackages: r.additionalPackages,
		template:           r.template,
		inputMap:           r.inputMap,
		workspacePackages:  r.workspacePackages,
		includeRootExports: r.includeRootExports,
		cache:              cache,
		conditions:         r.conditions,
	}
}

// WithConditions returns a new Resolver that uses the specified export
// condition priority when resolving package.json exports.
// Example: []string{"production", "browser", "import", "default"}
func (r *Resolver) WithConditions(conditions []string) *Resolver {
	return &Resolver{
		fs:                 r.fs,
		logger:             r.logger,
		additionalPackages: r.additionalPackages,
		template:           r.template,
		inputMap:           r.inputMap,
		workspacePackages:  r.workspacePackages,
		includeRootExports: r.includeRootExports,
		cache:              r.cache,
		conditions:         conditions,
	}
}

// resolveOpts returns ResolveOptions for the configured conditions.
func (r *Resolver) resolveOpts() *packagejson.ResolveOptions {
	if len(r.conditions) == 0 {
		return nil
	}
	return &packagejson.ResolveOptions{Conditions: r.conditions}
}

// parsePackageJSON parses a package.json file, using the cache if available.
// Uses atomic GetOrLoad to ensure only one goroutine parses a given file.
func (r *Resolver) parsePackageJSON(path string) (*packagejson.PackageJSON, error) {
	if r.cache != nil {
		return r.cache.GetOrLoad(path, func() (*packagejson.PackageJSON, error) {
			return packagejson.ParseFile(r.fs, path)
		})
	}
	return packagejson.ParseFile(r.fs, path)
}

// Resolve generates an ImportMap for a project rooted at the given directory.
func (r *Resolver) Resolve(rootDir string) (*importmap.ImportMap, error) {
	im, _, err := r.resolveInternal(rootDir, nil)
	return im, err
}

// ResolveSpecifiers generates import map entries for specific bare specifiers.
// This directly maps each specifier to a resolved URL using the template,
// attempting to resolve subpaths through package.json exports when available.
// Falls back to direct subpath mapping if exports resolution fails.
func (r *Resolver) ResolveSpecifiers(rootDir string, specifiers []string) map[string]string {
	result := make(map[string]string)
	if len(specifiers) == 0 {
		return result
	}

	workspaceRoot := resolve.FindWorkspaceRoot(r.fs, rootDir)
	nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")
	opts := r.resolveOpts()

	for _, spec := range specifiers {
		pkgName := parsePackageName(spec)
		subpath := strings.TrimPrefix(spec, pkgName)
		if subpath == "" {
			subpath = "."
		} else {
			// Convert "/subpath" to "./subpath"
			subpath = "." + subpath
		}

		// Try to resolve through package.json exports
		pkgPath := filepath.Join(nodeModulesPath, pkgName)
		pkgJSONPath := filepath.Join(pkgPath, "package.json")
		pkg, err := r.parsePackageJSON(pkgJSONPath)

		var resolvedPath string
		if err == nil {
			// Try to resolve through exports
			resolved, resolveErr := pkg.ResolveExport(subpath, opts)
			if resolveErr == nil {
				resolvedPath = resolved
			}
		}

		// Fall back to direct subpath if exports resolution failed
		if resolvedPath == "" {
			if subpath == "." {
				// Try main field
				if pkg != nil && pkg.Main != "" {
					resolvedPath = strings.TrimPrefix(pkg.Main, "./")
				} else {
					resolvedPath = "index.js"
				}
			} else {
				// Use subpath directly (strip leading ./)
				resolvedPath = strings.TrimPrefix(subpath, "./")
			}
		}

		// Apply template to generate the URL
		result[spec] = r.template.Expand(pkgName, "", resolvedPath)
	}

	return result
}

// ResolveWithGraph generates an ImportMap and builds a DependencyGraph.
// Use this for the initial resolution when you plan to do incremental updates.
func (r *Resolver) ResolveWithGraph(rootDir string) (*resolve.IncrementalResult, error) {
	graph := resolve.NewDependencyGraph()
	im, graph, err := r.resolveInternal(rootDir, graph)
	if err != nil {
		return nil, err
	}
	return &resolve.IncrementalResult{
		ImportMap:       im,
		DependencyGraph: graph,
	}, nil
}

// resolveInternal is the core resolution logic, optionally tracking dependencies in graph.
func (r *Resolver) resolveInternal(rootDir string, graph *resolve.DependencyGraph) (*importmap.ImportMap, *resolve.DependencyGraph, error) {
	// Normalize rootDir
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		absRoot = rootDir
	}
	rootDir = absRoot

	// Use workspace mode if workspace packages are explicitly configured
	if len(r.workspacePackages) > 0 {
		return r.resolveWorkspaceInternal(rootDir, graph)
	}

	// Auto-discover workspace packages from package.json workspaces field
	discoveredPackages, err := resolve.DiscoverWorkspacePackages(r.fs, rootDir)
	if err != nil && r.logger != nil {
		r.logger.Warning("Failed to discover workspace packages: %v", err)
	}
	if len(discoveredPackages) > 0 {
		return r.WithWorkspacePackages(discoveredPackages).resolveWorkspaceInternal(rootDir, graph)
	}

	result := &importmap.ImportMap{
		Imports: make(map[string]string),
		Scopes:  make(map[string]map[string]string),
	}

	// Find workspace root (may be different from rootDir in monorepos)
	workspaceRoot := resolve.FindWorkspaceRoot(r.fs, rootDir)

	// Parse root package.json
	rootPkgPath := filepath.Join(rootDir, "package.json")
	rootPkg, err := r.parsePackageJSON(rootPkgPath)
	if err != nil {
		// No package.json - still apply input map if provided
		if r.inputMap != nil {
			return result.Merge(r.inputMap), graph, nil
		}
		return result, graph, nil
	}

	// Add root package's own exports if requested
	// This is useful for dev servers where you want to import the package by name
	if r.includeRootExports && rootPkg.Name != "" {
		if err := r.addRootPackageExports(result, rootPkg); err != nil {
			if r.logger != nil {
				r.logger.Warning("Failed to add root package exports: %v", err)
			}
		}
	}

	// Collect all packages to process: dependencies + additional packages
	packagesToProcess := make(map[string]bool)
	for depName := range rootPkg.Dependencies {
		packagesToProcess[depName] = true
	}
	for _, pkg := range r.additionalPackages {
		// Parse package spec (may include subpath like "lit/decorators.js")
		pkgName := parsePackageName(pkg)
		packagesToProcess[pkgName] = true
	}

	// Add direct dependencies to imports (parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 10) // limit concurrency
	nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")

	for depName := range packagesToProcess {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			depPath := filepath.Join(nodeModulesPath, name)
			if !r.fs.Exists(depPath) {
				if r.logger != nil {
					r.logger.Warning("Dependency %s not found in node_modules", name)
				}
				return
			}

			if err := r.addPackageToImportMapWithGraph(result, &mu, name, depPath, graph); err != nil {
				if r.logger != nil {
					r.logger.Warning("Failed to add package %s: %v", name, err)
				}
			}
		}(depName)
	}
	wg.Wait()

	// Add scopes for transitive dependencies
	if err := r.addTransitiveDependenciesWithGraph(result, workspaceRoot, rootPkg, graph); err != nil {
		if r.logger != nil {
			r.logger.Warning("Failed to add transitive dependencies: %v", err)
		}
	}

	// Clean up empty scopes
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}

	// Merge with input map if provided (input map takes precedence)
	if r.inputMap != nil {
		result = result.Merge(r.inputMap)
	}

	return result, graph, nil
}

// resolveWorkspace generates an import map for a monorepo workspace.
// Workspace packages are added to global imports, and their dependencies
// from node_modules are resolved using the template.
func (r *Resolver) resolveWorkspace(rootDir string) (*importmap.ImportMap, error) {
	im, _, err := r.resolveWorkspaceInternal(rootDir, nil)
	return im, err
}

// resolveWorkspaceInternal is the core workspace resolution logic, optionally tracking dependencies.
func (r *Resolver) resolveWorkspaceInternal(rootDir string, graph *resolve.DependencyGraph) (*importmap.ImportMap, *resolve.DependencyGraph, error) {
	result := &importmap.ImportMap{
		Imports: make(map[string]string),
		Scopes:  make(map[string]map[string]string),
	}

	// Build set of workspace package names (to exclude from node_modules resolution)
	workspaceNames := make(map[string]bool)
	for _, pkg := range r.workspacePackages {
		workspaceNames[pkg.Name] = true
		if graph != nil {
			graph.AddWorkspacePackage(pkg.Name)
			graph.SetPackagePath(pkg.Name, pkg.Path)
		}
	}

	// 1. Add workspace packages to global imports
	for _, pkg := range r.workspacePackages {
		if err := r.addWorkspacePackageToImportMapWithGraph(result, pkg, rootDir, graph); err != nil {
			if r.logger != nil {
				r.logger.Warning("Failed to add workspace package %s: %v", pkg.Name, err)
			}
		}
	}

	// 2. Collect dependencies from all workspace packages (excluding other workspace packages)
	allDeps := make(map[string]bool)
	for _, pkg := range r.workspacePackages {
		pkgJSON, err := r.parsePackageJSON(filepath.Join(pkg.Path, "package.json"))
		if err != nil {
			continue
		}
		for depName := range pkgJSON.Dependencies {
			if !workspaceNames[depName] {
				allDeps[depName] = true
				if graph != nil {
					graph.AddDependency(pkg.Name, depName)
				}
			}
		}
	}

	// Include additional packages
	for _, pkg := range r.additionalPackages {
		pkgName := parsePackageName(pkg)
		if !workspaceNames[pkgName] {
			allDeps[pkgName] = true
		}
	}

	// 3. Add node_modules dependencies (parallel)
	nodeModulesPath := filepath.Join(rootDir, "node_modules")
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 10)

	for depName := range allDeps {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			depPath := filepath.Join(nodeModulesPath, name)
			if !r.fs.Exists(depPath) {
				if r.logger != nil {
					r.logger.Warning("Dependency %s not found in node_modules", name)
				}
				return
			}
			if err := r.addPackageToImportMapWithGraph(result, &mu, name, depPath, graph); err != nil {
				if r.logger != nil {
					r.logger.Warning("Failed to add package %s: %v", name, err)
				}
			}
		}(depName)
	}
	wg.Wait()

	// 4. Add transitive dependency scopes (parallel)
	visited := sync.Map{}
	for depName := range allDeps {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r.processPackageDependenciesParallelWithGraph(result, &mu, &visited, nodeModulesPath, name, rootDir, graph)
		}(depName)
	}
	wg.Wait()

	// Clean up empty scopes
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}

	// 5. Merge with input map if provided (input map takes precedence)
	if r.inputMap != nil {
		result = result.Merge(r.inputMap)
	}

	return result, graph, nil
}

// addWorkspacePackageToImportMap adds a workspace package's exports to the import map.
// Unlike node_modules packages, workspace packages use web paths relative to rootDir.
func (r *Resolver) addWorkspacePackageToImportMap(im *importmap.ImportMap, pkg resolve.WorkspacePackage, rootDir string) error {
	return r.addWorkspacePackageToImportMapWithGraph(im, pkg, rootDir, nil)
}

// addWorkspacePackageToImportMapWithGraph adds a workspace package's exports to the import map,
// optionally tracking in the dependency graph.
func (r *Resolver) addWorkspacePackageToImportMapWithGraph(im *importmap.ImportMap, pkg resolve.WorkspacePackage, rootDir string, graph *resolve.DependencyGraph) error {
	pkgJSON, err := r.parsePackageJSON(filepath.Join(pkg.Path, "package.json"))
	if err != nil {
		return err
	}

	// Calculate web path relative to rootDir
	webPath := resolve.ToWebPath(rootDir, pkg.Path)

	// Get all export entries
	opts := r.resolveOpts()
	entries := pkgJSON.ExportEntries(opts)
	for _, entry := range entries {
		var importKey string
		if entry.Subpath == "." {
			importKey = pkg.Name
		} else {
			subpath := strings.TrimPrefix(entry.Subpath, "./")
			importKey = pkg.Name + "/" + subpath
		}
		target := strings.TrimPrefix(entry.Target, "./")
		im.Imports[importKey] = webPath + "/" + target
	}

	// Handle wildcard exports (trailing slash imports)
	wildcards := pkgJSON.WildcardExports(opts)
	for _, w := range wildcards {
		patternPrefix := strings.TrimSuffix(strings.TrimPrefix(w.Pattern, "./"), "*")
		importKey := pkg.Name + "/" + patternPrefix
		target := strings.TrimSuffix(w.Target, "*")
		im.Imports[importKey] = webPath + "/" + target
	}

	// Fallback to main if no exports
	if len(entries) == 0 && pkgJSON.Main != "" {
		im.Imports[pkg.Name] = webPath + "/" + strings.TrimPrefix(pkgJSON.Main, "./")
	}

	// Add trailing slash for packages that support it
	if pkgJSON.HasTrailingSlashExport(opts) && len(wildcards) == 0 {
		im.Imports[pkg.Name+"/"] = webPath + "/"
	}

	return nil
}

// addRootPackageExports adds the root package's own exports to the import map.
// This allows importing the package by name in development (e.g., import { x } from 'my-lib').
func (r *Resolver) addRootPackageExports(im *importmap.ImportMap, pkg *packagejson.PackageJSON) error {
	// Get all export entries
	opts := r.resolveOpts()
	entries := pkg.ExportEntries(opts)
	for _, entry := range entries {
		var importKey string
		if entry.Subpath == "." {
			importKey = pkg.Name
		} else {
			subpath := strings.TrimPrefix(entry.Subpath, "./")
			importKey = pkg.Name + "/" + subpath
		}
		// Root package exports use relative paths from root (e.g., ./lib/index.js -> /lib/index.js)
		target := "/" + strings.TrimPrefix(entry.Target, "./")
		im.Imports[importKey] = target
	}

	// Handle wildcard exports
	wildcards := pkg.WildcardExports(opts)
	for _, w := range wildcards {
		patternPrefix := strings.TrimSuffix(strings.TrimPrefix(w.Pattern, "./"), "*")
		importKey := pkg.Name + "/" + patternPrefix
		target := "/" + strings.TrimSuffix(strings.TrimPrefix(w.Target, "./"), "*")
		im.Imports[importKey] = target
	}

	// Fallback to main if no exports
	if len(entries) == 0 && pkg.Main != "" {
		im.Imports[pkg.Name] = "/" + strings.TrimPrefix(pkg.Main, "./")
	}

	// Add trailing slash for packages that support it
	if pkg.HasTrailingSlashExport(opts) && len(wildcards) == 0 {
		im.Imports[pkg.Name+"/"] = "/"
	}

	return nil
}

// addPackageToImportMap adds a package's exports to the import map.
// It builds entries locally first, then acquires the lock to merge them.
func (r *Resolver) addPackageToImportMap(im *importmap.ImportMap, mu *sync.Mutex, pkgName, pkgPath string) error {
	return r.addPackageToImportMapWithGraph(im, mu, pkgName, pkgPath, nil)
}

// addPackageToImportMapWithGraph adds a package's exports to the import map,
// optionally tracking in the dependency graph.
func (r *Resolver) addPackageToImportMapWithGraph(im *importmap.ImportMap, mu *sync.Mutex, pkgName, pkgPath string, graph *resolve.DependencyGraph) error {
	pkgJSONPath := filepath.Join(pkgPath, "package.json")
	pkg, err := r.parsePackageJSON(pkgJSONPath)
	if err != nil {
		return err
	}

	// Track package path in graph
	if graph != nil {
		graph.SetPackagePath(pkgName, pkgPath)
	}

	// Build entries locally
	imports := make(map[string]string)
	opts := r.resolveOpts()

	entries := pkg.ExportEntries(opts)
	for _, entry := range entries {
		var importKey string
		if entry.Subpath == "." {
			importKey = pkgName
		} else {
			subpath := strings.TrimPrefix(entry.Subpath, "./")
			importKey = pkgName + "/" + subpath
		}
		imports[importKey] = r.template.Expand(pkgName, "", entry.Target)
	}

	wildcards := pkg.WildcardExports(opts)
	for _, w := range wildcards {
		patternPrefix := strings.TrimSuffix(strings.TrimPrefix(w.Pattern, "./"), "*")
		importKey := pkgName + "/" + patternPrefix
		imports[importKey] = r.template.Expand(pkgName, "", w.Target)
	}

	// Fallback to main if no exports
	if len(entries) == 0 && pkg.Main != "" {
		imports[pkgName] = r.template.Expand(pkgName, "", strings.TrimPrefix(pkg.Main, "./"))
	}

	// Warn if bare specifier won't work (no root export and no main fallback)
	if _, ok := imports[pkgName]; !ok {
		if r.logger != nil {
			r.logger.Warning("Package '%s' has no root export or main field; only subpath imports will work", pkgName)
		}
	}

	// Add trailing slash for packages that support it
	if pkg.HasTrailingSlashExport(opts) && len(wildcards) == 0 {
		imports[pkgName+"/"] = r.template.Expand(pkgName, "", "")
	}

	// Merge into import map under lock
	mu.Lock()
	maps.Copy(im.Imports, imports)
	mu.Unlock()

	return nil
}

// addTransitiveDependencies adds scopes for packages that have their own dependencies.
// Uses parallel processing for improved performance on large dependency trees.
func (r *Resolver) addTransitiveDependencies(im *importmap.ImportMap, rootDir string, rootPkg *packagejson.PackageJSON) error {
	return r.addTransitiveDependenciesWithGraph(im, rootDir, rootPkg, nil)
}

// addTransitiveDependenciesWithGraph adds scopes for packages that have their own dependencies,
// optionally tracking in the dependency graph.
func (r *Resolver) addTransitiveDependenciesWithGraph(im *importmap.ImportMap, rootDir string, rootPkg *packagejson.PackageJSON, graph *resolve.DependencyGraph) error {
	nodeModulesPath := filepath.Join(rootDir, "node_modules")

	var (
		mu      sync.Mutex
		visited sync.Map
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 10) // limit to 10 concurrent goroutines
	)

	for depName := range rootPkg.Dependencies {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire semaphore
			defer func() { <-sem }() // release semaphore

			r.processPackageDependenciesParallelWithGraph(im, &mu, &visited, nodeModulesPath, name, rootDir, graph)
		}(depName)
	}

	wg.Wait()
	return nil
}

// processPackageDependenciesParallel recursively processes a package's dependencies and adds scopes.
// Uses thread-safe data structures for concurrent access.
func (r *Resolver) processPackageDependenciesParallel(
	im *importmap.ImportMap,
	mu *sync.Mutex,
	visited *sync.Map,
	nodeModulesPath, pkgName, rootDir string,
) {
	r.processPackageDependenciesParallelWithGraph(im, mu, visited, nodeModulesPath, pkgName, rootDir, nil)
}

// processPackageDependenciesParallelWithGraph recursively processes a package's dependencies and adds scopes,
// optionally tracking in the dependency graph.
func (r *Resolver) processPackageDependenciesParallelWithGraph(
	im *importmap.ImportMap,
	mu *sync.Mutex,
	visited *sync.Map,
	nodeModulesPath, pkgName, rootDir string,
	graph *resolve.DependencyGraph,
) {
	// Check if already visited (atomic)
	if _, loaded := visited.LoadOrStore(pkgName, true); loaded {
		return
	}

	pkgPath := filepath.Join(nodeModulesPath, pkgName)
	pkgJSONPath := filepath.Join(pkgPath, "package.json")

	pkg, err := r.parsePackageJSON(pkgJSONPath)
	if err != nil {
		return
	}

	if len(pkg.Dependencies) == 0 {
		return
	}

	// Scope key uses the template with empty path to get the base URL
	scopeKey := r.template.Expand(pkgName, "", "")
	if !strings.HasSuffix(scopeKey, "/") {
		scopeKey += "/"
	}

	// Track scope key in graph
	if graph != nil {
		graph.SetScopeKey(pkgName, scopeKey)
	}

	// Build scope entries locally first to minimize lock time
	scopeEntries := make(map[string]string)
	opts := r.resolveOpts()

	for depName := range pkg.Dependencies {
		// Track dependency relationship in graph
		if graph != nil {
			graph.AddDependency(pkgName, depName)
		}

		depPath := filepath.Join(nodeModulesPath, depName)
		if !r.fs.Exists(depPath) {
			continue
		}

		depPkgPath := filepath.Join(depPath, "package.json")
		depPkg, err := r.parsePackageJSON(depPkgPath)
		if err != nil {
			continue
		}

		// Add all export entries for this dependency
		entries := depPkg.ExportEntries(opts)
		for _, entry := range entries {
			var importKey string
			if entry.Subpath == "." {
				importKey = depName
			} else {
				subpath := strings.TrimPrefix(entry.Subpath, "./")
				importKey = depName + "/" + subpath
			}
			scopeEntries[importKey] = r.template.Expand(depName, "", entry.Target)
		}

		// Handle wildcard exports (trailing slash imports)
		wildcards := depPkg.WildcardExports(opts)
		for _, w := range wildcards {
			patternPrefix := strings.TrimSuffix(strings.TrimPrefix(w.Pattern, "./"), "*")
			importKey := depName + "/" + patternPrefix
			scopeEntries[importKey] = r.template.Expand(depName, "", w.Target)
		}

		// Fallback to main if no exports
		if len(entries) == 0 && depPkg.Main != "" {
			scopeEntries[depName] = r.template.Expand(depName, "", strings.TrimPrefix(depPkg.Main, "./"))
		}

		// Recursively process (will be deduped by visited map)
		r.processPackageDependenciesParallelWithGraph(im, mu, visited, nodeModulesPath, depName, rootDir, graph)
	}

	// Merge scope entries into import map (protected by mutex)
	if len(scopeEntries) > 0 {
		mu.Lock()
		if im.Scopes[scopeKey] == nil {
			im.Scopes[scopeKey] = make(map[string]string)
		}
		maps.Copy(im.Scopes[scopeKey], scopeEntries)
		mu.Unlock()
	}
}

// parsePackageName extracts the package name from a package spec.
// Handles scoped packages (@scope/name) and subpaths (lit/decorators.js).
func parsePackageName(spec string) string {
	if strings.HasPrefix(spec, "@") {
		// Scoped package: @scope/name or @scope/name/subpath
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return spec
	}
	// Regular package: name or name/subpath
	if idx := strings.Index(spec, "/"); idx > 0 {
		return spec[:idx]
	}
	return spec
}

// ResolveIncremental updates an existing import map based on changed packages.
// If update.PreviousMap or update.PreviousGraph is nil, falls back to full resolution.
// Only the changed packages and their dependents are re-resolved.
func (r *Resolver) ResolveIncremental(rootDir string, update resolve.IncrementalUpdate) (*resolve.IncrementalResult, error) {
	// Fallback to full resolution if no previous state
	if update.PreviousMap == nil || update.PreviousGraph == nil || len(update.ChangedPackages) == 0 {
		return r.ResolveWithGraph(rootDir)
	}

	// Normalize rootDir
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		absRoot = rootDir
	}
	rootDir = absRoot

	// Invalidate cache for changed packages
	if r.cache != nil {
		for _, pkgName := range update.ChangedPackages {
			pkgPath := update.PreviousGraph.PackagePath(pkgName)
			if pkgPath != "" {
				r.cache.Invalidate(filepath.Join(pkgPath, "package.json"))
			}
		}
	}

	// Compute all affected packages: changed + transitive dependents
	affected := r.computeAffectedPackages(update.ChangedPackages, update.PreviousGraph)

	// Clone the previous map and graph
	result := update.PreviousMap.Clone()
	newGraph := update.PreviousGraph.Clone()

	// Remove affected packages from the map
	for _, pkgName := range affected {
		r.removePackageFromMap(result, pkgName, update.PreviousGraph)
		newGraph.RemovePackage(pkgName)
	}

	// Re-resolve affected packages
	nodeModulesPath := filepath.Join(rootDir, "node_modules")
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 10)

	for _, pkgName := range affected {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check if this is a workspace package
			if update.PreviousGraph.IsWorkspacePackage(name) {
				pkgPath := update.PreviousGraph.PackagePath(name)
				if pkgPath != "" {
					pkg := resolve.WorkspacePackage{Name: name, Path: pkgPath}
					newGraph.AddWorkspacePackage(name)
					newGraph.SetPackagePath(name, pkgPath)
					if err := r.addWorkspacePackageToImportMapWithGraph(result, pkg, rootDir, newGraph); err != nil {
						if r.logger != nil {
							r.logger.Warning("Failed to re-add workspace package %s: %v", name, err)
						}
					}
				}
				return
			}

			// Regular node_modules package
			depPath := filepath.Join(nodeModulesPath, name)
			if !r.fs.Exists(depPath) {
				if r.logger != nil {
					r.logger.Warning("Dependency %s not found in node_modules", name)
				}
				return
			}

			mu.Lock()
			if result.Imports == nil {
				result.Imports = make(map[string]string)
			}
			if result.Scopes == nil {
				result.Scopes = make(map[string]map[string]string)
			}
			mu.Unlock()

			if err := r.addPackageToImportMapWithGraph(result, &mu, name, depPath, newGraph); err != nil {
				if r.logger != nil {
					r.logger.Warning("Failed to re-add package %s: %v", name, err)
				}
			}
		}(pkgName)
	}
	wg.Wait()

	// Re-process transitive dependencies for affected packages
	visited := sync.Map{}
	for _, pkgName := range affected {
		if !update.PreviousGraph.IsWorkspacePackage(pkgName) {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				r.processPackageDependenciesParallelWithGraph(result, &mu, &visited, nodeModulesPath, name, rootDir, newGraph)
			}(pkgName)
		}
	}
	wg.Wait()

	// Clean up empty scopes
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}

	// Merge with input map if provided (input map takes precedence)
	if r.inputMap != nil {
		result = result.Merge(r.inputMap)
	}

	return &resolve.IncrementalResult{
		ImportMap:       result,
		DependencyGraph: newGraph,
	}, nil
}

// computeAffectedPackages returns all packages that need to be re-resolved:
// the changed packages plus all their transitive dependents.
func (r *Resolver) computeAffectedPackages(changed []string, graph *resolve.DependencyGraph) []string {
	affected := make(map[string]bool)

	for _, pkg := range changed {
		affected[pkg] = true
		for _, dep := range graph.TransitiveDependents(pkg) {
			affected[dep] = true
		}
	}

	result := make([]string, 0, len(affected))
	for pkg := range affected {
		result = append(result, pkg)
	}
	return result
}

// removePackageFromMap removes a package's entries from the import map.
func (r *Resolver) removePackageFromMap(im *importmap.ImportMap, pkgName string, graph *resolve.DependencyGraph) {
	if im.Imports != nil {
		// Remove exact package name entry
		delete(im.Imports, pkgName)
		// Remove trailing slash entry
		delete(im.Imports, pkgName+"/")
		// Remove subpath entries (e.g., "lit/decorators.js")
		prefix := pkgName + "/"
		for key := range im.Imports {
			if strings.HasPrefix(key, prefix) {
				delete(im.Imports, key)
			}
		}
	}

	// Remove package's scope
	scopeKey := graph.ScopeKey(pkgName)
	if scopeKey != "" && im.Scopes != nil {
		delete(im.Scopes, scopeKey)
	}
}
