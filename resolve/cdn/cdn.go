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

// Package cdn provides a resolver for CDN-hosted npm packages.
package cdn

import (
	"context"
	"maps"
	"strings"
	"sync"

	mappacdn "bennypowers.dev/mappa/cdn"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
)

// Resolver generates import maps pointing to CDN-hosted packages.
type Resolver struct {
	fetcher      mappacdn.Fetcher
	provider     mappacdn.Provider
	registry     *mappacdn.Registry
	template     *resolve.Template
	cache        *mappacdn.PackageCache
	logger       resolve.Logger
	conditions   []string
	includeDev   bool
	maxDepth     int  // Maximum dependency depth (0 = unlimited)
	resolveScope bool // Whether to resolve transitive dependencies as scopes
}

// New creates a new CDN resolver with default settings.
func New(fetcher mappacdn.Fetcher) *Resolver {
	tmpl, _ := resolve.ParseTemplate(mappacdn.DefaultProvider.ModuleTemplate)
	return &Resolver{
		fetcher:      fetcher,
		provider:     mappacdn.DefaultProvider,
		registry:     mappacdn.NewRegistry(fetcher),
		template:     tmpl,
		cache:        mappacdn.NewPackageCache(100),
		resolveScope: true,
	}
}

// WithProvider returns a new Resolver using the specified CDN provider.
func (r *Resolver) WithProvider(provider mappacdn.Provider) *Resolver {
	tmpl, _ := resolve.ParseTemplate(provider.ModuleTemplate)
	return &Resolver{
		fetcher:      r.fetcher,
		provider:     provider,
		registry:     r.registry,
		template:     tmpl,
		cache:        r.cache,
		logger:       r.logger,
		conditions:   r.conditions,
		includeDev:   r.includeDev,
		maxDepth:     r.maxDepth,
		resolveScope: r.resolveScope,
	}
}

// WithTemplate returns a new Resolver using a custom URL template.
func (r *Resolver) WithTemplate(pattern string) (*Resolver, error) {
	tmpl, err := resolve.ParseTemplate(pattern)
	if err != nil {
		return nil, err
	}
	return &Resolver{
		fetcher:      r.fetcher,
		provider:     r.provider,
		registry:     r.registry,
		template:     tmpl,
		cache:        r.cache,
		logger:       r.logger,
		conditions:   r.conditions,
		includeDev:   r.includeDev,
		maxDepth:     r.maxDepth,
		resolveScope: r.resolveScope,
	}, nil
}

// WithLogger returns a new Resolver with the specified logger.
func (r *Resolver) WithLogger(logger resolve.Logger) *Resolver {
	return &Resolver{
		fetcher:      r.fetcher,
		provider:     r.provider,
		registry:     r.registry,
		template:     r.template,
		cache:        r.cache,
		logger:       logger,
		conditions:   r.conditions,
		includeDev:   r.includeDev,
		maxDepth:     r.maxDepth,
		resolveScope: r.resolveScope,
	}
}

// WithConditions returns a new Resolver with the specified export conditions.
func (r *Resolver) WithConditions(conditions []string) *Resolver {
	return &Resolver{
		fetcher:      r.fetcher,
		provider:     r.provider,
		registry:     r.registry,
		template:     r.template,
		cache:        r.cache,
		logger:       r.logger,
		conditions:   conditions,
		includeDev:   r.includeDev,
		maxDepth:     r.maxDepth,
		resolveScope: r.resolveScope,
	}
}

// WithIncludeDev returns a new Resolver that includes devDependencies.
func (r *Resolver) WithIncludeDev(include bool) *Resolver {
	return &Resolver{
		fetcher:      r.fetcher,
		provider:     r.provider,
		registry:     r.registry,
		template:     r.template,
		cache:        r.cache,
		logger:       r.logger,
		conditions:   r.conditions,
		includeDev:   include,
		maxDepth:     r.maxDepth,
		resolveScope: r.resolveScope,
	}
}

// WithMaxDepth returns a new Resolver with a maximum dependency depth.
// 0 means unlimited (default), 1 means direct dependencies only.
func (r *Resolver) WithMaxDepth(depth int) *Resolver {
	return &Resolver{
		fetcher:      r.fetcher,
		provider:     r.provider,
		registry:     r.registry,
		template:     r.template,
		cache:        r.cache,
		logger:       r.logger,
		conditions:   r.conditions,
		includeDev:   r.includeDev,
		maxDepth:     depth,
		resolveScope: r.resolveScope,
	}
}

// WithResolveScope controls whether to generate scopes for transitive dependencies.
func (r *Resolver) WithResolveScope(resolveScope bool) *Resolver {
	return &Resolver{
		fetcher:      r.fetcher,
		provider:     r.provider,
		registry:     r.registry,
		template:     r.template,
		cache:        r.cache,
		logger:       r.logger,
		conditions:   r.conditions,
		includeDev:   r.includeDev,
		maxDepth:     r.maxDepth,
		resolveScope: resolveScope,
	}
}

// resolveOpts returns ResolveOptions for the configured conditions.
func (r *Resolver) resolveOpts() *packagejson.ResolveOptions {
	if len(r.conditions) == 0 {
		return nil
	}
	return &packagejson.ResolveOptions{Conditions: r.conditions}
}

// ResolvePackageJSON generates an ImportMap from a parsed package.json.
func (r *Resolver) ResolvePackageJSON(ctx context.Context, pkg *packagejson.PackageJSON) (*importmap.ImportMap, error) {
	result := &importmap.ImportMap{
		Imports: make(map[string]string),
		Scopes:  make(map[string]map[string]string),
	}

	// Collect dependencies to process
	deps := maps.Clone(pkg.Dependencies)
	if deps == nil {
		deps = make(map[string]string)
	}
	if r.includeDev {
		for name, version := range pkg.DevDependencies {
			if _, exists := deps[name]; !exists {
				deps[name] = version
			}
		}
	}

	// Resolve each dependency
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 10) // Limit concurrency
	visited := sync.Map{}

	for name, versionRange := range deps {
		wg.Add(1)
		go func(pkgName, verRange string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := r.resolvePackage(ctx, result, &mu, &visited, pkgName, verRange, 0); err != nil {
				if r.logger != nil {
					r.logger.Warning("Failed to resolve %s@%s: %v", pkgName, verRange, err)
				}
			}
		}(name, versionRange)
	}
	wg.Wait()

	// Clean up empty scopes
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}

	return result, nil
}

// resolvePackage resolves a single package and its dependencies.
func (r *Resolver) resolvePackage(
	ctx context.Context,
	im *importmap.ImportMap,
	mu *sync.Mutex,
	visited *sync.Map,
	pkgName, versionRange string,
	depth int,
) error {
	// Check max depth
	if r.maxDepth > 0 && depth >= r.maxDepth {
		return nil
	}

	// Resolve version
	version, err := r.registry.ResolveVersion(ctx, pkgName, versionRange)
	if err != nil {
		return err
	}

	// Check if already visited at this or higher version
	cacheKey := pkgName + "@" + version
	if _, loaded := visited.LoadOrStore(cacheKey, true); loaded {
		return nil
	}

	// Fetch package.json from CDN
	pkg, err := r.fetchPackageJSON(ctx, pkgName, version)
	if err != nil {
		return err
	}

	// Add to imports
	r.addPackageImports(im, mu, pkgName, version, pkg)

	// Resolve transitive dependencies if enabled
	if r.resolveScope && (r.maxDepth == 0 || depth < r.maxDepth) && len(pkg.Dependencies) > 0 {
		scopeKey := r.template.Expand(pkgName, version, "")
		if !strings.HasSuffix(scopeKey, "/") {
			scopeKey += "/"
		}

		scopeEntries := make(map[string]string)
		var wg sync.WaitGroup
		var scopeMu sync.Mutex
		sem := make(chan struct{}, 10)

		for depName, depVer := range pkg.Dependencies {
			wg.Add(1)
			go func(name, ver string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				// Resolve transitive dependency version
				resolvedVer, err := r.registry.ResolveVersion(ctx, name, ver)
				if err != nil {
					if r.logger != nil {
						r.logger.Warning("Failed to resolve transitive dep %s@%s: %v", name, ver, err)
					}
					return
				}

				// Fetch package.json
				depPkg, err := r.fetchPackageJSON(ctx, name, resolvedVer)
				if err != nil {
					if r.logger != nil {
						r.logger.Warning("Failed to fetch %s@%s: %v", name, resolvedVer, err)
					}
					return
				}

				// Build scope entries
				entries := r.buildPackageImports(name, resolvedVer, depPkg)
				scopeMu.Lock()
				maps.Copy(scopeEntries, entries)
				scopeMu.Unlock()

				// Recursively resolve deeper dependencies using resolved version
				if err := r.resolvePackage(ctx, im, mu, visited, name, resolvedVer, depth+1); err != nil {
					if r.logger != nil {
						r.logger.Warning("Failed to resolve transitive dep %s: %v", name, err)
					}
				}
			}(depName, depVer)
		}
		wg.Wait()

		if len(scopeEntries) > 0 {
			mu.Lock()
			if im.Scopes == nil {
				im.Scopes = make(map[string]map[string]string)
			}
			if im.Scopes[scopeKey] == nil {
				im.Scopes[scopeKey] = make(map[string]string)
			}
			maps.Copy(im.Scopes[scopeKey], scopeEntries)
			mu.Unlock()
		}
	}

	return nil
}

// fetchPackageJSON fetches and parses a package.json from the CDN.
func (r *Resolver) fetchPackageJSON(ctx context.Context, pkgName, version string) (*packagejson.PackageJSON, error) {
	return r.cache.GetOrLoad(pkgName, version, func() (*packagejson.PackageJSON, error) {
		url := r.buildPackageJSONURL(pkgName, version)
		data, err := r.fetcher.Fetch(ctx, url)
		if err != nil {
			return nil, err
		}
		return packagejson.Parse(data)
	})
}

// buildPackageJSONURL builds the URL for a package.json file.
func (r *Resolver) buildPackageJSONURL(pkgName, version string) string {
	url := r.provider.PackageJSONTemplate
	url = strings.ReplaceAll(url, "{package}", pkgName)
	url = strings.ReplaceAll(url, "{version}", version)
	return url
}

// addPackageImports adds a package's exports to the import map.
func (r *Resolver) addPackageImports(im *importmap.ImportMap, mu *sync.Mutex, pkgName, version string, pkg *packagejson.PackageJSON) {
	entries := r.buildPackageImports(pkgName, version, pkg)
	mu.Lock()
	maps.Copy(im.Imports, entries)
	mu.Unlock()
}

// buildPackageImports builds import map entries for a package.
func (r *Resolver) buildPackageImports(pkgName, version string, pkg *packagejson.PackageJSON) map[string]string {
	imports := make(map[string]string)
	opts := r.resolveOpts()

	// Add explicit exports
	entries := pkg.ExportEntries(opts)
	for _, entry := range entries {
		var importKey string
		if entry.Subpath == "." {
			importKey = pkgName
		} else {
			subpath := strings.TrimPrefix(entry.Subpath, "./")
			importKey = pkgName + "/" + subpath
		}
		imports[importKey] = r.template.Expand(pkgName, version, entry.Target)
	}

	// Handle wildcard exports
	wildcards := pkg.WildcardExports(opts)
	for _, w := range wildcards {
		patternPrefix := strings.TrimSuffix(strings.TrimPrefix(w.Pattern, "./"), "*")
		importKey := pkgName + "/" + patternPrefix
		imports[importKey] = r.template.Expand(pkgName, version, w.Target)
	}

	// Fallback to main if no exports
	if len(entries) == 0 && pkg.Main != "" {
		imports[pkgName] = r.template.Expand(pkgName, version, strings.TrimPrefix(pkg.Main, "./"))
	}

	// Add trailing slash for packages that support it
	if pkg.HasTrailingSlashExport(opts) && len(wildcards) == 0 {
		imports[pkgName+"/"] = r.template.Expand(pkgName, version, "")
	}

	return imports
}
