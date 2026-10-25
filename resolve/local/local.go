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
	"fmt"
	"maps"
	"path/filepath"
	"slices"
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
	excludePackages    []string
	template           *resolve.Template
	inputMap           *importmap.ImportMap
	workspacePackages  []resolve.WorkspacePackage
	includeRootExports bool
	cache              packagejson.Cache
	conditions         []string // export condition priority
	pathBase           string
	packageDeps        string
	// excluded is the exclusion set derived from excludePackages once per
	// resolution pass. It is read-only during concurrent traversal.
	excluded map[string]bool
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

// clone returns a shallow copy of the resolver with cloned slices.
func (r *Resolver) clone() *Resolver {
	c := *r
	c.additionalPackages = slices.Clone(r.additionalPackages)
	c.excludePackages = slices.Clone(r.excludePackages)
	c.workspacePackages = slices.Clone(r.workspacePackages)
	c.conditions = slices.Clone(r.conditions)
	return &c
}

// WithPackages returns a new Resolver that includes additional packages
// beyond those listed in package.json dependencies.
func (r *Resolver) WithPackages(packages []string) *Resolver {
	c := r.clone()
	c.additionalPackages = slices.Clone(packages)
	return c
}

// WithExclude returns a new Resolver that excludes the specified packages
// from the generated import map.
func (r *Resolver) WithExclude(packages []string) *Resolver {
	c := r.clone()
	c.excludePackages = slices.Clone(packages)
	return c
}

// WithTemplate returns a new Resolver that uses the specified URL template.
func (r *Resolver) WithTemplate(pattern string) (*Resolver, error) {
	tmpl, err := resolve.ParseTemplate(pattern)
	if err != nil {
		return nil, err
	}
	c := r.clone()
	c.template = tmpl
	return c, nil
}

// WithInputMap returns a new Resolver that merges the given import map
// with the generated output. Input map entries take precedence.
func (r *Resolver) WithInputMap(im *importmap.ImportMap) *Resolver {
	c := r.clone()
	c.inputMap = im
	return c
}

// WithWorkspacePackages returns a new Resolver configured for workspace mode.
// When workspace packages are provided, they are added to global imports and
// their dependencies are collected for node_modules resolution.
func (r *Resolver) WithWorkspacePackages(packages []resolve.WorkspacePackage) *Resolver {
	c := r.clone()
	c.workspacePackages = slices.Clone(packages)
	return c
}

// WithIncludeRootExports returns a new Resolver that includes the root package's
// own exports in the import map. This is useful for development servers where
// you want to import the package you're developing by name.
func (r *Resolver) WithIncludeRootExports() *Resolver {
	c := r.clone()
	c.includeRootExports = true
	return c
}

// WithPackageCache returns a new Resolver that uses the provided cache
// for parsed package.json files. This allows callers to reuse parsed data
// across multiple resolution calls, improving performance for hot-reload
// or monorepo scenarios.
func (r *Resolver) WithPackageCache(cache packagejson.Cache) *Resolver {
	c := r.clone()
	c.cache = cache
	return c
}

// WithConditions returns a new Resolver that uses the specified export
// condition priority when resolving package.json exports.
// Example: []string{"production", "browser", "import", "default"}
func (r *Resolver) WithConditions(conditions []string) *Resolver {
	c := r.clone()
	c.conditions = slices.Clone(conditions)
	return c
}

// WithPathBase returns a new Resolver that rebases workspace package paths
// relative to the given directory instead of the resolution root.
// Paths under node_modules are not affected.
// Use this when resolving from a workspace root on behalf of a subdirectory
// package, so that the subdirectory's own exports map to "/" rather than
// their full workspace-relative path.
func (r *Resolver) WithPathBase(dir string) *Resolver {
	c := r.clone()
	c.pathBase = dir
	return c
}

// WithPackageDeps returns a new Resolver that limits dependency collection
// to only dependencies listed in the package.json at the given directory.
// Without this, workspace resolution collects dependencies from every
// workspace package, which may produce warnings for irrelevant packages.
func (r *Resolver) WithPackageDeps(dir string) *Resolver {
	c := r.clone()
	c.packageDeps = dir
	return c
}

// normalizePathOptions returns a new Resolver with pathBase and packageDeps
// resolved to absolute paths relative to rootDir, and the exclusion set built
// once for the resolution pass.
func (r *Resolver) normalizePathOptions(rootDir string) *Resolver {
	normalized := *r
	normalized.excluded = make(map[string]bool, len(r.excludePackages))
	for _, pkg := range r.excludePackages {
		normalized.excluded[pkg] = true
	}
	if normalized.pathBase != "" && !filepath.IsAbs(normalized.pathBase) {
		normalized.pathBase = filepath.Join(rootDir, normalized.pathBase)
	}
	if normalized.packageDeps != "" && !filepath.IsAbs(normalized.packageDeps) {
		normalized.packageDeps = filepath.Join(rootDir, normalized.packageDeps)
	}
	return &normalized
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
// A manifest or workspace declaring more than resolve.MaxDependencyEntries
// direct dependencies is rejected with an error; transitive packages exceeding
// the same cap have their scope skipped with a warning.
func (r *Resolver) Resolve(rootDir string) (*importmap.ImportMap, error) {
	im, _, err := r.resolveInternal(rootDir, nil)
	return im, err
}

// ResolveSpecifiers generates import map entries for specific bare specifiers.
// This directly maps each specifier to a resolved URL using the template,
// attempting to resolve subpaths through package.json exports when available.
// Falls back to direct subpath mapping if exports resolution fails.
// When a package supports trailing-slash exports, uses a single trailing-slash
// key instead of individual entries for each subpath.
func (r *Resolver) ResolveSpecifiers(rootDir string, specifiers []string) map[string]string {
	result := make(map[string]string)
	if len(specifiers) == 0 {
		return result
	}

	workspaceRoot := resolve.FindWorkspaceRoot(r.fs, rootDir)
	nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")
	opts := r.resolveOpts()

	// Group specifiers by package name
	pkgSpecifiers := make(map[string][]string)
	for _, spec := range specifiers {
		pkgName := parsePackageName(spec)
		pkgSpecifiers[pkgName] = append(pkgSpecifiers[pkgName], spec)
	}

	// Consult the root package's own exports before looking in node_modules.
	// This supports dev-server workflows where the package under development
	// imports itself by name (WithIncludeRootExports).
	var rootPkg *packagejson.PackageJSON
	if r.includeRootExports {
		if pkg, err := r.parsePackageJSON(filepath.Join(rootDir, "package.json")); err == nil && pkg.Name != "" {
			rootPkg = pkg
		}
	}

	// Process each package
	for pkgName, specs := range pkgSpecifiers {
		if !validPackageName(pkgName) {
			continue
		}

		if rootPkg != nil && pkgName == rootPkg.Name {
			for _, spec := range specs {
				subpath := strings.TrimPrefix(spec, pkgName)
				if subpath == "" {
					subpath = "."
				} else {
					subpath = "." + subpath
				}
				resolved, err := rootPkg.ResolveExport(subpath, opts)
				if err != nil || !withinPackage(rootDir, resolved) {
					continue
				}
				result[spec] = "/" + resolved
			}
			continue
		}

		pkgPath := filepath.Join(nodeModulesPath, pkgName)
		pkgJSONPath := filepath.Join(pkgPath, "package.json")
		pkg, err := r.parsePackageJSON(pkgJSONPath)

		if err != nil {
			if r.logger != nil {
				r.logger.Warning("Could not parse package.json for %s: %v", pkgName, err)
			}
			// Fall back to individual entries without exports resolution
			for _, spec := range specs {
				subpath := strings.TrimPrefix(spec, pkgName)
				if subpath == "" {
					result[spec] = r.template.Expand(pkgName, "", "index.js")
				} else {
					result[spec] = r.template.Expand(pkgName, "", strings.TrimPrefix(subpath, "/"))
				}
			}
			continue
		}

		// Add trailing-slash keys from ImportMapEntries
		trailingSlashPrefixes := make(map[string]bool)
		for _, e := range pkg.ImportMapEntries(opts) {
			if strings.HasSuffix(e.Key, "/") && withinPackage(pkgPath, e.Path) {
				importKey := pkgName + e.Key
				result[importKey] = r.template.Expand(pkgName, "", e.Path)
				trailingSlashPrefixes[importKey] = true
			}
		}

		// Add entries for each specifier
		for _, spec := range specs {
			subpath := strings.TrimPrefix(spec, pkgName)
			if subpath == "" {
				subpath = "."
			} else {
				subpath = "." + subpath
			}

			// Skip subpath entries if covered by any trailing-slash key
			if subpath != "." {
				covered := false
				for prefix := range trailingSlashPrefixes {
					// prefix ends with "/" so this checks if spec is a subpath
					if strings.HasPrefix(spec, prefix) {
						covered = true
						break
					}
				}
				if covered {
					continue
				}
			}

			// Resolve the specifier
			var resolvedPath string
			resolved, resolveErr := pkg.ResolveExport(subpath, opts)
			if resolveErr == nil && withinPackage(pkgPath, resolved) {
				resolvedPath = resolved
			}

			// Fall back to direct subpath
			if resolvedPath == "" {
				if subpath == "." {
					resolvedPath = pkg.BareEntry()
				} else {
					resolvedPath = strings.TrimPrefix(subpath, "./")
				}
			}

			if !withinPackage(pkgPath, resolvedPath) {
				continue
			}

			result[spec] = r.template.Expand(pkgName, "", resolvedPath)
		}
	}

	return result
}

// ResolveWithGraph generates an ImportMap and builds a DependencyGraph.
// Use this for the initial resolution when you plan to do incremental updates.
// The same dependency cap as Resolve applies: top-level declarations above
// resolve.MaxDependencyEntries return an error, while oversized transitive
// packages are skipped with a warning.
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

	r = r.normalizePathOptions(rootDir)

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
		if r.logger != nil {
			r.logger.Warning("Could not read %s: %v", rootPkgPath, err)
		}
		// No package.json - still apply input map if provided
		if r.inputMap != nil {
			return result.Merge(r.inputMap), graph, nil
		}
		return result, graph, nil
	}

	// Add root package's own exports if requested
	// This is useful for dev servers where you want to import the package by name
	if r.includeRootExports && rootPkg.Name != "" {
		if err := r.addRootPackageExports(result, rootPkg, rootDir); err != nil {
			if r.logger != nil {
				r.logger.Warning("Failed to add root package exports: %v", err)
			}
		}
	}

	// Collect all packages to process: dependencies + additional packages
	packagesToProcess, _ := r.directDependencyNames(rootDir, nil)

	if len(packagesToProcess) > resolve.MaxDependencyEntries {
		return nil, graph, resolve.DependencyCapError("package.json", len(packagesToProcess))
	}

	// Add direct dependencies to imports (parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 10) // limit concurrency
	nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")

	for depName := range packagesToProcess {
		sem <- struct{}{}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
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

	// Add scopes for transitive dependencies. Dependency selection uses the
	// package root (rootDir), while hoisted modules and web paths use the
	// workspace root.
	if err := r.addTransitiveDependenciesWithGraph(result, workspaceRoot, rootDir, graph); err != nil {
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

// resolveWorkspaceInternal is the core workspace resolution logic, optionally tracking dependencies.
func (r *Resolver) resolveWorkspaceInternal(rootDir string, graph *resolve.DependencyGraph) (*importmap.ImportMap, *resolve.DependencyGraph, error) {
	result := &importmap.ImportMap{
		Imports: make(map[string]string),
		Scopes:  make(map[string]map[string]string),
	}

	for _, pkg := range r.workspacePackages {
		if graph != nil {
			graph.AddWorkspacePackage(pkg.Name)
			graph.SetPackagePath(pkg.Name, pkg.Path)
		}
	}

	// 1. Add workspace packages to global imports
	for _, pkg := range r.workspacePackages {
		if r.excluded[pkg.Name] {
			continue
		}
		if err := r.addWorkspacePackageToImportMap(result, pkg, rootDir); err != nil {
			if r.logger != nil {
				r.logger.Warning("Failed to add workspace package %s: %v", pkg.Name, err)
			}
		}
	}

	// 2. Collect dependencies (excluding other workspace packages)
	allDeps, err := r.directDependencyNames(rootDir, graph)
	if err != nil {
		return nil, graph, err
	}

	if len(allDeps) > resolve.MaxDependencyEntries {
		return nil, graph, resolve.DependencyCapError("workspace", len(allDeps))
	}

	// 3. Add node_modules dependencies (parallel)
	nodeModulesPath := filepath.Join(rootDir, "node_modules")
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 10)

	for depName := range allDeps {
		sem <- struct{}{}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
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
		sem <- struct{}{}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

			r.processPackageDependenciesParallelWithGraph(result, &mu, &visited, nodeModulesPath, name, rootDir, graph)
		}(depName)
	}
	wg.Wait()

	// Clean up empty scopes
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}

	// 5. Rebase paths for serve root
	r.rebaseImportMapForPathBase(result, rootDir)

	// 6. Merge with input map if provided (input map takes precedence)
	if r.inputMap != nil {
		result = result.Merge(r.inputMap)
	}

	return result, graph, nil
}

// addWorkspacePackageToImportMap adds a workspace package's exports to the import map.
func (r *Resolver) addWorkspacePackageToImportMap(im *importmap.ImportMap, pkg resolve.WorkspacePackage, rootDir string) error {
	pkgJSON, err := r.parsePackageJSON(filepath.Join(pkg.Path, "package.json"))
	if err != nil {
		return err
	}

	// Calculate web path relative to rootDir
	webPath := resolve.ToWebPath(rootDir, pkg.Path)

	opts := r.resolveOpts()
	for _, e := range pkgJSON.ImportMapEntries(opts) {
		if !withinPackage(pkg.Path, e.Path) {
			continue
		}
		im.Imports[pkg.Name+e.Key] = webPath + "/" + e.Path
	}

	return nil
}

// addRootPackageExports adds the root package's own exports to the import map.
// This allows importing the package by name in development (e.g., import { x } from 'my-lib').
func (r *Resolver) addRootPackageExports(im *importmap.ImportMap, pkg *packagejson.PackageJSON, pkgPath string) error {
	opts := r.resolveOpts()
	for _, e := range pkg.ImportMapEntries(opts) {
		if !withinPackage(pkgPath, e.Path) {
			continue
		}
		im.Imports[pkg.Name+e.Key] = "/" + e.Path
	}
	return nil
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

	for _, e := range pkg.ImportMapEntries(opts) {
		if !withinPackage(pkgPath, e.Path) {
			continue
		}
		imports[pkgName+e.Key] = r.template.Expand(pkgName, "", e.Path)
	}

	// Warn if bare specifier won't work (no root export and no main fallback)
	if _, ok := imports[pkgName]; !ok {
		if r.logger != nil {
			r.logger.Warning("Package '%s' has no root export or main field; only subpath imports will work", pkgName)
		}
	}

	// Merge into import map under lock
	mu.Lock()
	maps.Copy(im.Imports, imports)
	mu.Unlock()

	return nil
}

// addTransitiveDependenciesWithGraph adds scopes for packages that have their own dependencies,
// optionally tracking in the dependency graph. workspaceRoot locates hoisted
// node_modules and anchors web paths; packageRoot selects which manifest's
// direct dependencies seed the traversal.
func (r *Resolver) addTransitiveDependenciesWithGraph(im *importmap.ImportMap, workspaceRoot, packageRoot string, graph *resolve.DependencyGraph) error {
	nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")

	var (
		mu      sync.Mutex
		visited sync.Map
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 10) // limit to 10 concurrent goroutines
	)

	// Direct dependencies and additional packages both get top-level imports,
	// so both need transitive scopes. Select them from the package root, not
	// the workspace root: a --package subdirectory's dependencies are hoisted
	// into the workspace node_modules but declared in the package manifest.
	packages, err := r.directDependencyNames(packageRoot, nil)
	if err != nil {
		return err
	}

	for depName := range packages {
		sem <- struct{}{} // acquire semaphore before spawning
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore

			r.processPackageDependenciesParallelWithGraph(im, &mu, &visited, nodeModulesPath, name, workspaceRoot, graph)
		}(depName)
	}

	wg.Wait()
	return nil
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
	pkgPath := filepath.Join(nodeModulesPath, pkgName)
	r.processPackageAtPath(im, mu, visited, pkgName, pkgPath, nodeModulesPath, rootDir, graph)
}

// processPackageAtPath recursively processes the dependencies of the package at
// pkgPath. rootNodeModules is the workspace-root node_modules directory; it is
// kept separate from the importer's lookup path so that hoisted dependencies of
// nested copies are still found.
func (r *Resolver) processPackageAtPath(
	im *importmap.ImportMap,
	mu *sync.Mutex,
	visited *sync.Map,
	pkgName, pkgPath, rootNodeModules, rootDir string,
	graph *resolve.DependencyGraph,
) {
	// Use resolved path as visited key so nested copies are distinct
	if _, loaded := visited.LoadOrStore(pkgPath, true); loaded {
		return
	}
	pkgJSONPath := filepath.Join(pkgPath, "package.json")

	pkg, err := r.parsePackageJSON(pkgJSONPath)
	if err != nil {
		return
	}

	// Collect all deps that affect scope: regular + peer + optional. Apply the
	// same name and exclusion filtering as the top-level dependency sites so
	// the cap counts only entries that could actually be processed.
	allDeps := make(map[string]bool)
	addDeps := func(deps map[string]string) {
		for name := range deps {
			if validPackageName(name) {
				allDeps[name] = true
			}
		}
	}
	addDeps(pkg.Dependencies)
	addDeps(pkg.PeerDependencies)
	addDeps(pkg.OptionalDependencies)
	for name := range r.excluded {
		delete(allDeps, name)
	}
	if len(allDeps) > resolve.MaxDependencyEntries {
		if r.logger != nil {
			r.logger.Warning("Package %s declares %d dependencies (maximum %d); skipping its scope", pkgName, len(allDeps), resolve.MaxDependencyEntries)
		}
		return
	}

	if len(allDeps) == 0 {
		return
	}

	// Scope keys must match the package's actual URL. Hoisted packages use the
	// configured template; nested copies use their real path so they don't
	// collide with a hoisted copy of the same name.
	hoistedPath := filepath.Join(rootNodeModules, pkgName)
	var scopeKey string
	if filepath.Clean(pkgPath) == filepath.Clean(hoistedPath) {
		scopeKey = r.template.Expand(pkgName, "", "")
	} else {
		scopeKey = resolve.ToWebPath(rootDir, pkgPath)
		if scopeKey == "" {
			scopeKey = r.template.Expand(pkgName, "", "")
		}
	}
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

	for depName := range allDeps {
		// Track dependency relationship in graph
		if graph != nil {
			graph.AddDependency(pkgName, depName)
		}

		depPath, isNested := r.findDependencyPath(pkgPath, rootNodeModules, depName)
		if depPath == "" {
			continue
		}

		depPkgPath := filepath.Join(depPath, "package.json")
		depPkg, err := r.parsePackageJSON(depPkgPath)
		if err != nil {
			continue
		}

		for _, e := range depPkg.ImportMapEntries(opts) {
			if !withinPackage(depPath, e.Path) {
				continue
			}
			url := r.expandDepURL(depName, e.Path, isNested, depPath, rootDir)
			scopeEntries[depName+e.Key] = url
		}

		// Recurse using the dependency's actual path and the workspace-root
		// node_modules fallback.
		r.processPackageAtPath(im, mu, visited, depName, depPath, rootNodeModules, rootDir, graph)
	}

	// Simplify scope entries (remove entries covered by trailing-slash keys)
	scopeEntries = importmap.SimplifyEntries(scopeEntries)

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

// findDependencyPath locates a dependency using Node-like resolution: the
// package's own node_modules first, then each ancestor's node_modules, and
// finally the workspace-root node_modules. npm may install a different version
// of a transitive dep inside the parent's own node_modules when the hoisted
// version doesn't satisfy the constraint.
// Returns the path and whether it is a nested copy rather than the hoisted
// workspace-root one.
func (r *Resolver) findDependencyPath(parentPkgPath, rootNodeModules, depName string) (string, bool) {
	rootNodeModules = filepath.Clean(rootNodeModules)
	rootCandidate := filepath.Join(rootNodeModules, depName)
	// Never walk above the workspace root: anything found there cannot be
	// served from the project and would produce URLs escaping the web root.
	workspaceRoot := filepath.Dir(rootNodeModules)
	dir := filepath.Clean(parentPkgPath)
	for resolve.IsWithinDir(workspaceRoot, dir) {
		// When dir is the root node_modules itself, the candidate would be a
		// nonsensical node_modules/node_modules path. The root candidate is
		// probed at the workspace root below instead.
		if dir != rootNodeModules {
			candidate := filepath.Join(dir, "node_modules", depName)
			if r.fs.Exists(candidate) {
				return candidate, filepath.Clean(candidate) != rootCandidate
			}
		}
		if dir == workspaceRoot {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// expandDepURL returns the URL for a dependency's file. For hoisted deps it
// uses the template; for nested deps it computes a path relative to the
// workspace root so the browser can find the nested copy.
func (r *Resolver) expandDepURL(depName, filePath string, nested bool, depPath, rootDir string) string {
	if !nested {
		return r.template.Expand(depName, "", filePath)
	}
	rel, err := filepath.Rel(rootDir, filepath.Join(depPath, filePath))
	if err != nil {
		return r.template.Expand(depName, "", filePath)
	}
	result := "/" + filepath.ToSlash(rel)
	if strings.HasSuffix(filePath, "/") || filePath == "" {
		result += "/"
	}
	return result
}

// parsePackageName extracts the package name from a package spec.
func parsePackageName(spec string) string {
	return resolve.PackageName(spec)
}

// validPackageName rejects names that could escape node_modules or name a
// path. It accepts unscoped names and a single @scope/name separator.
// isSchemeTarget reports whether an export target is a URL (for example
// http:, data:, or node:) rather than a path within the package.
func isSchemeTarget(target string) bool {
	if strings.HasPrefix(target, "//") {
		return true
	}
	i := strings.IndexByte(target, ':')
	if i <= 0 {
		return false
	}
	for j, r := range target[:i] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case j > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

// withinPackage reports whether target stays inside pkgPath. Scheme-bearing
// targets are not filesystem paths and are skipped.
func withinPackage(pkgPath, target string) bool {
	if isSchemeTarget(target) {
		return false
	}
	return resolve.IsWithinDir(pkgPath, filepath.Join(pkgPath, target))
}

func validPackageName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "/") {
		return false
	}
	if strings.ContainsAny(name, "\\:\x00") || strings.Contains(name, "..") {
		return false
	}
	if strings.HasPrefix(name, "@") {
		scope, rest, ok := strings.Cut(name, "/")
		return ok && len(scope) > 1 && rest != "" && !strings.Contains(rest, "/")
	}
	return !strings.Contains(name, "/")
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

	r = r.normalizePathOptions(rootDir)

	// Mirror resolveInternal: when no workspace packages were configured,
	// auto-discover them so direct dependency selection matches the initial
	// resolution.
	if len(r.workspacePackages) == 0 {
		discovered, discoverErr := resolve.DiscoverWorkspacePackages(r.fs, rootDir)
		if discoverErr != nil && r.logger != nil {
			r.logger.Warning("Failed to discover workspace packages: %v", discoverErr)
		}
		if len(discovered) > 0 {
			r = r.WithWorkspacePackages(discovered)
		}
	}

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

	direct, err := r.directDependencyNames(rootDir, nil)
	if err != nil {
		if r.logger != nil {
			r.logger.Warning("Failed to collect direct dependencies: %v", err)
		}
	}
	// If the root manifest cannot be read, fall back to the previous graph's
	// direct imports rather than dropping packages from the map.
	if len(direct) == 0 {
		direct = make(map[string]bool)
		for name := range update.PreviousMap.Imports {
			direct[parsePackageName(name)] = true
		}
	}

	for _, pkgName := range affected {
		sem <- struct{}{}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer func() { <-sem }()

			// Check if this is a workspace package
			if update.PreviousGraph.IsWorkspacePackage(name) {
				// Excluded workspace packages never enter the import map, so
				// re-adding one here would undo the exclusion.
				if r.excluded[name] {
					return
				}
				pkgPath := update.PreviousGraph.PackagePath(name)
				if pkgPath != "" {
					pkg := resolve.WorkspacePackage{Name: name, Path: pkgPath}
					newGraph.AddWorkspacePackage(name)
					newGraph.SetPackagePath(name, pkgPath)
					mu.Lock()
					err := r.addWorkspacePackageToImportMap(result, pkg, rootDir)
					mu.Unlock()
					if err != nil {
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

			// Only direct dependencies belong in top-level imports. A
			// transitive package's entries live in its dependents' scopes.
			if !direct[name] {
				return
			}
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
			sem <- struct{}{}
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
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

	// Rebase paths for serve root
	r.rebaseImportMapForPathBase(result, rootDir)

	// Merge with input map if provided (input map takes precedence)
	if r.inputMap != nil {
		result = result.Merge(r.inputMap)
	}

	return &resolve.IncrementalResult{
		ImportMap:       result,
		DependencyGraph: newGraph,
	}, nil
}

// directDependencyNames returns the packages that receive top-level imports.
// It is the single owner of direct-dependency selection: the packageDeps
// manifest, all workspace manifests, or the root manifest; valid-name
// filtering; workspace-name exclusion; additional packages; and exclusions.
// When graph is non-nil, workspace dependency edges are recorded.
func (r *Resolver) directDependencyNames(rootDir string, graph *resolve.DependencyGraph) (map[string]bool, error) {
	names := make(map[string]bool)
	workspaceNames := make(map[string]bool, len(r.workspacePackages))
	for _, pkg := range r.workspacePackages {
		workspaceNames[pkg.Name] = true
	}

	add := func(owner string, pkg *packagejson.PackageJSON) {
		for name := range pkg.Dependencies {
			if !validPackageName(name) || workspaceNames[name] {
				continue
			}
			names[name] = true
			if graph != nil && owner != "" {
				graph.AddDependency(owner, name)
			}
		}
	}

	switch {
	case r.packageDeps != "":
		pkg, err := r.parsePackageJSON(filepath.Join(r.packageDeps, "package.json"))
		if err != nil {
			return nil, fmt.Errorf("package-deps %s: %w", r.packageDeps, err)
		}
		var owner string
		if graph != nil {
			for _, pkg := range r.workspacePackages {
				if pkg.Path == r.packageDeps {
					owner = pkg.Name
					break
				}
			}
		}
		add(owner, pkg)
	case len(r.workspacePackages) > 0:
		// The root manifest's dependencies are ignored in workspace mode.
		for _, pkg := range r.workspacePackages {
			workspacePkg, err := r.parsePackageJSON(filepath.Join(pkg.Path, "package.json"))
			if err != nil {
				continue
			}
			add(pkg.Name, workspacePkg)
		}
	default:
		if pkg, err := r.parsePackageJSON(filepath.Join(rootDir, "package.json")); err == nil {
			add("", pkg)
		}
	}

	for _, pkg := range r.additionalPackages {
		if name := parsePackageName(pkg); validPackageName(name) && !workspaceNames[name] {
			names[name] = true
		}
	}
	for _, name := range r.excludePackages {
		delete(names, name)
	}
	return names, nil
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

func (r *Resolver) rebaseImportMapForPathBase(im *importmap.ImportMap, rootDir string) {
	if r.pathBase == "" {
		return
	}
	relPath, err := filepath.Rel(rootDir, r.pathBase)
	if err != nil || relPath == "." || strings.HasPrefix(relPath, "..") {
		return
	}
	prefix := "/" + filepath.ToSlash(relPath)

	for key, value := range im.Imports {
		im.Imports[key] = rebasePath(value, prefix)
	}

	if im.Scopes != nil {
		newScopes := make(map[string]map[string]string, len(im.Scopes))
		for scopeKey, scopeMap := range im.Scopes {
			newKey := rebasePath(scopeKey, prefix)
			newMap := make(map[string]string, len(scopeMap))
			for k, v := range scopeMap {
				newMap[k] = rebasePath(v, prefix)
			}
			newScopes[newKey] = newMap
		}
		im.Scopes = newScopes
	}
}

func rebasePath(path, prefix string) string {
	if path == prefix {
		return "/"
	}
	if after, ok := strings.CutPrefix(path, prefix+"/"); ok {
		return "/" + after
	}
	return path
}
