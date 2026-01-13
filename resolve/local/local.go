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
	fs                  fs.FileSystem
	logger              resolve.Logger
	additionalPackages  []string
	template            *resolve.Template
	inputMap            *importmap.ImportMap
	workspacePackages   []resolve.WorkspacePackage
	includeRootExports  bool
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
		fs:                  r.fs,
		logger:              r.logger,
		additionalPackages:  packages,
		template:            r.template,
		inputMap:            r.inputMap,
		workspacePackages:   r.workspacePackages,
		includeRootExports:  r.includeRootExports,
	}
}

// WithTemplate returns a new Resolver that uses the specified URL template.
func (r *Resolver) WithTemplate(pattern string) (*Resolver, error) {
	tmpl, err := resolve.ParseTemplate(pattern)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		fs:                  r.fs,
		logger:              r.logger,
		additionalPackages:  r.additionalPackages,
		template:            tmpl,
		inputMap:            r.inputMap,
		workspacePackages:   r.workspacePackages,
		includeRootExports:  r.includeRootExports,
	}, nil
}

// WithInputMap returns a new Resolver that merges the given import map
// with the generated output. Input map entries take precedence.
func (r *Resolver) WithInputMap(im *importmap.ImportMap) *Resolver {
	return &Resolver{
		fs:                  r.fs,
		logger:              r.logger,
		additionalPackages:  r.additionalPackages,
		template:            r.template,
		inputMap:            im,
		workspacePackages:   r.workspacePackages,
		includeRootExports:  r.includeRootExports,
	}
}

// WithWorkspacePackages returns a new Resolver configured for workspace mode.
// When workspace packages are provided, they are added to global imports and
// their dependencies are collected for node_modules resolution.
func (r *Resolver) WithWorkspacePackages(packages []resolve.WorkspacePackage) *Resolver {
	return &Resolver{
		fs:                  r.fs,
		logger:              r.logger,
		additionalPackages:  r.additionalPackages,
		template:            r.template,
		inputMap:            r.inputMap,
		workspacePackages:   packages,
		includeRootExports:  r.includeRootExports,
	}
}

// WithIncludeRootExports returns a new Resolver that includes the root package's
// own exports in the import map. This is useful for development servers where
// you want to import the package you're developing by name.
func (r *Resolver) WithIncludeRootExports() *Resolver {
	return &Resolver{
		fs:                  r.fs,
		logger:              r.logger,
		additionalPackages:  r.additionalPackages,
		template:            r.template,
		inputMap:            r.inputMap,
		workspacePackages:   r.workspacePackages,
		includeRootExports:  true,
	}
}

// Resolve generates an ImportMap for a project rooted at the given directory.
func (r *Resolver) Resolve(rootDir string) (*importmap.ImportMap, error) {
	// Normalize rootDir
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		absRoot = rootDir
	}
	rootDir = absRoot

	// Use workspace mode if workspace packages are configured
	if len(r.workspacePackages) > 0 {
		return r.resolveWorkspace(rootDir)
	}

	result := &importmap.ImportMap{
		Imports: make(map[string]string),
		Scopes:  make(map[string]map[string]string),
	}

	// Find workspace root (may be different from rootDir in monorepos)
	workspaceRoot := resolve.FindWorkspaceRoot(r.fs, rootDir)

	// Parse root package.json
	rootPkgPath := filepath.Join(rootDir, "package.json")
	rootPkg, err := packagejson.ParseFile(r.fs, rootPkgPath)
	if err != nil {
		// No package.json - still apply input map if provided
		if r.inputMap != nil {
			return result.Merge(r.inputMap), nil
		}
		return result, nil
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

	for depName := range packagesToProcess {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			depPath := filepath.Join(workspaceRoot, "node_modules", name)
			if !r.fs.Exists(depPath) {
				if r.logger != nil {
					r.logger.Warning("Dependency %s not found in node_modules", name)
				}
				return
			}

			if err := r.addPackageToImportMapSafe(result, &mu, name, depPath); err != nil {
				if r.logger != nil {
					r.logger.Warning("Failed to add package %s: %v", name, err)
				}
			}
		}(depName)
	}
	wg.Wait()

	// Add scopes for transitive dependencies
	if err := r.addTransitiveDependencies(result, workspaceRoot, rootPkg); err != nil {
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

	return result, nil
}

// resolveWorkspace generates an import map for a monorepo workspace.
// Workspace packages are added to global imports, and their dependencies
// from node_modules are resolved using the template.
func (r *Resolver) resolveWorkspace(rootDir string) (*importmap.ImportMap, error) {
	result := &importmap.ImportMap{
		Imports: make(map[string]string),
		Scopes:  make(map[string]map[string]string),
	}

	// Build set of workspace package names (to exclude from node_modules resolution)
	workspaceNames := make(map[string]bool)
	for _, pkg := range r.workspacePackages {
		workspaceNames[pkg.Name] = true
	}

	// 1. Add workspace packages to global imports
	for _, pkg := range r.workspacePackages {
		if err := r.addWorkspacePackageToImportMap(result, pkg, rootDir); err != nil {
			if r.logger != nil {
				r.logger.Warning("Failed to add workspace package %s: %v", pkg.Name, err)
			}
		}
	}

	// 2. Collect dependencies from all workspace packages (excluding other workspace packages)
	allDeps := make(map[string]bool)
	for _, pkg := range r.workspacePackages {
		pkgJSON, err := packagejson.ParseFile(r.fs, filepath.Join(pkg.Path, "package.json"))
		if err != nil {
			continue
		}
		for depName := range pkgJSON.Dependencies {
			if !workspaceNames[depName] {
				allDeps[depName] = true
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
			if err := r.addPackageToImportMapSafe(result, &mu, name, depPath); err != nil {
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

			r.processPackageDependenciesParallel(result, &mu, &visited, nodeModulesPath, name, rootDir)
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

	return result, nil
}

// addWorkspacePackageToImportMap adds a workspace package's exports to the import map.
// Unlike node_modules packages, workspace packages use web paths relative to rootDir.
func (r *Resolver) addWorkspacePackageToImportMap(im *importmap.ImportMap, pkg resolve.WorkspacePackage, rootDir string) error {
	pkgJSON, err := packagejson.ParseFile(r.fs, filepath.Join(pkg.Path, "package.json"))
	if err != nil {
		return err
	}

	// Calculate web path relative to rootDir
	webPath := resolve.ToWebPath(rootDir, pkg.Path)

	// Get all export entries
	entries := pkgJSON.ExportEntries()
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
	wildcards := pkgJSON.WildcardExports()
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
	if pkgJSON.HasTrailingSlashExport() && len(wildcards) == 0 {
		im.Imports[pkg.Name+"/"] = webPath + "/"
	}

	return nil
}

// addRootPackageExports adds the root package's own exports to the import map.
// This allows importing the package by name in development (e.g., import { x } from 'my-lib').
func (r *Resolver) addRootPackageExports(im *importmap.ImportMap, pkg *packagejson.PackageJSON, rootDir string) error {
	// Get all export entries
	entries := pkg.ExportEntries()
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
	wildcards := pkg.WildcardExports()
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
	if pkg.HasTrailingSlashExport() && len(wildcards) == 0 {
		im.Imports[pkg.Name+"/"] = "/"
	}

	return nil
}

// addPackageToImportMap adds a package's exports to the import map.
func (r *Resolver) addPackageToImportMap(im *importmap.ImportMap, pkgName, pkgPath string) error {
	pkgJSONPath := filepath.Join(pkgPath, "package.json")
	pkg, err := packagejson.ParseFile(r.fs, pkgJSONPath)
	if err != nil {
		return err
	}

	// Get all export entries
	entries := pkg.ExportEntries()
	for _, entry := range entries {
		var importKey string
		if entry.Subpath == "." {
			importKey = pkgName
		} else {
			subpath := strings.TrimPrefix(entry.Subpath, "./")
			importKey = pkgName + "/" + subpath
		}
		im.Imports[importKey] = r.template.Expand(pkgName, "", entry.Target)
	}

	// Handle wildcard exports (trailing slash imports)
	wildcards := pkg.WildcardExports()
	for _, w := range wildcards {
		patternPrefix := strings.TrimSuffix(strings.TrimPrefix(w.Pattern, "./"), "*")
		importKey := pkgName + "/" + patternPrefix
		im.Imports[importKey] = r.template.Expand(pkgName, "", w.Target)
	}

	// Fallback to main if no exports
	if len(entries) == 0 && pkg.Main != "" {
		im.Imports[pkgName] = r.template.Expand(pkgName, "", strings.TrimPrefix(pkg.Main, "./"))
	}

	// Add trailing slash for packages that support it
	if pkg.HasTrailingSlashExport() && len(wildcards) == 0 {
		im.Imports[pkgName+"/"] = r.template.Expand(pkgName, "", "")
	}

	return nil
}

// addPackageToImportMapSafe is a thread-safe version of addPackageToImportMap.
// It builds entries locally first, then acquires the lock to merge them.
func (r *Resolver) addPackageToImportMapSafe(im *importmap.ImportMap, mu *sync.Mutex, pkgName, pkgPath string) error {
	pkgJSONPath := filepath.Join(pkgPath, "package.json")
	pkg, err := packagejson.ParseFile(r.fs, pkgJSONPath)
	if err != nil {
		return err
	}

	// Build entries locally
	imports := make(map[string]string)

	entries := pkg.ExportEntries()
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

	wildcards := pkg.WildcardExports()
	for _, w := range wildcards {
		patternPrefix := strings.TrimSuffix(strings.TrimPrefix(w.Pattern, "./"), "*")
		importKey := pkgName + "/" + patternPrefix
		imports[importKey] = r.template.Expand(pkgName, "", w.Target)
	}

	// Fallback to main if no exports
	if len(entries) == 0 && pkg.Main != "" {
		imports[pkgName] = r.template.Expand(pkgName, "", strings.TrimPrefix(pkg.Main, "./"))
	}

	// Add trailing slash for packages that support it
	if pkg.HasTrailingSlashExport() && len(wildcards) == 0 {
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

			r.processPackageDependenciesParallel(im, &mu, &visited, nodeModulesPath, name, rootDir)
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
	// Check if already visited (atomic)
	if _, loaded := visited.LoadOrStore(pkgName, true); loaded {
		return
	}

	pkgPath := filepath.Join(nodeModulesPath, pkgName)
	pkgJSONPath := filepath.Join(pkgPath, "package.json")

	pkg, err := packagejson.ParseFile(r.fs, pkgJSONPath)
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

	// Build scope entries locally first to minimize lock time
	scopeEntries := make(map[string]string)

	for depName := range pkg.Dependencies {
		depPath := filepath.Join(nodeModulesPath, depName)
		if !r.fs.Exists(depPath) {
			continue
		}

		depPkgPath := filepath.Join(depPath, "package.json")
		depPkg, err := packagejson.ParseFile(r.fs, depPkgPath)
		if err != nil {
			continue
		}

		// Add all export entries for this dependency
		entries := depPkg.ExportEntries()
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
		wildcards := depPkg.WildcardExports()
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
		r.processPackageDependenciesParallel(im, mu, visited, nodeModulesPath, depName, rootDir)
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
