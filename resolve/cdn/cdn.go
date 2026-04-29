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
	"slices"
	"strings"
	"sync"
	"time"

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
	excludePackages []string
	maxDepth        int           // Maximum dependency depth (0 = unlimited)
	resolveScope    bool          // Whether to resolve transitive dependencies as scopes
	requestTimeout  time.Duration // Per-request timeout (0 = no timeout)
	concurrency     int           // Max concurrent goroutines across all depths
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
		concurrency:  10,
	}
}

// WithProvider returns a new Resolver using the specified CDN provider.
func (r *Resolver) WithProvider(provider mappacdn.Provider) *Resolver {
	tmpl, _ := resolve.ParseTemplate(provider.ModuleTemplate)
	c := r.clone()
	c.provider = provider
	c.template = tmpl
	return c
}

// WithTemplate returns a new Resolver using a custom URL template.
func (r *Resolver) WithTemplate(pattern string) (*Resolver, error) {
	tmpl, err := resolve.ParseTemplate(pattern)
	if err != nil {
		return nil, err
	}
	c := r.clone()
	c.template = tmpl
	return c, nil
}

// WithLogger returns a new Resolver with the specified logger.
func (r *Resolver) WithLogger(logger resolve.Logger) *Resolver {
	c := r.clone()
	c.logger = logger
	return c
}

// WithConditions returns a new Resolver with the specified export conditions.
func (r *Resolver) WithConditions(conditions []string) *Resolver {
	c := r.clone()
	c.conditions = conditions
	return c
}

// WithIncludeDev returns a new Resolver that includes devDependencies.
func (r *Resolver) WithIncludeDev(include bool) *Resolver {
	c := r.clone()
	c.includeDev = include
	return c
}

// WithMaxDepth returns a new Resolver with a maximum dependency depth.
// 0 means unlimited (default), 1 means direct dependencies only.
func (r *Resolver) WithMaxDepth(depth int) *Resolver {
	c := r.clone()
	c.maxDepth = depth
	return c
}

// WithResolveScope controls whether to generate scopes for transitive dependencies.
func (r *Resolver) WithResolveScope(resolveScope bool) *Resolver {
	c := r.clone()
	c.resolveScope = resolveScope
	return c
}

// WithExclude returns a new Resolver that excludes the specified packages
// from the generated import map, including as transitive dependencies.
func (r *Resolver) WithExclude(packages []string) *Resolver {
	c := r.clone()
	c.excludePackages = packages
	return c
}

// WithRequestTimeout returns a new Resolver with a per-request timeout.
// 0 means no timeout (default). Applied to each HTTP fetch and registry call.
func (r *Resolver) WithRequestTimeout(d time.Duration) *Resolver {
	c := r.clone()
	c.requestTimeout = d
	return c
}

// WithConcurrency returns a new Resolver with the specified max concurrent goroutines.
// Shared across all recursion depths to prevent unbounded fan-out.
func (r *Resolver) WithConcurrency(n int) *Resolver {
	c := r.clone()
	if n > 0 {
		c.concurrency = n
	}
	return c
}

func (r *Resolver) clone() *Resolver {
	return &Resolver{
		fetcher:         r.fetcher,
		provider:        r.provider,
		registry:        r.registry,
		template:        r.template,
		cache:           r.cache,
		logger:          r.logger,
		conditions:      slices.Clone(r.conditions),
		includeDev:      r.includeDev,
		excludePackages: slices.Clone(r.excludePackages),
		maxDepth:        r.maxDepth,
		resolveScope:    r.resolveScope,
		requestTimeout:  r.requestTimeout,
		concurrency:     r.concurrency,
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

	for _, pkg := range r.excludePackages {
		delete(deps, pkg)
	}

	// Resolve each dependency
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, r.concurrency)
	visited := sync.Map{}

	for name, versionRange := range deps {
		wg.Add(1)
		go func(pkgName, verRange string) {
			defer wg.Done()
			if err := r.resolvePackage(ctx, result, &mu, &visited, sem, pkgName, verRange, 0); err != nil {
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
// sem is shared across all recursion depths to bound total concurrency.
// Acquires sem only for HTTP work, releases before spawning children
// to prevent deadlock when parents hold slots while waiting on children.
func (r *Resolver) resolvePackage(
	ctx context.Context,
	im *importmap.ImportMap,
	mu *sync.Mutex,
	visited *sync.Map,
	sem chan struct{},
	pkgName, versionRange string,
	depth int,
) error {
	if r.maxDepth > 0 && depth >= r.maxDepth {
		return nil
	}

	if err := r.acquireSem(ctx, sem); err != nil {
		return err
	}

	version, err := r.resolveVersion(ctx, pkgName, versionRange)
	if err != nil {
		<-sem
		return err
	}

	cacheKey := pkgName + "@" + version
	if _, loaded := visited.LoadOrStore(cacheKey, true); loaded {
		<-sem
		return nil
	}

	pkg, err := r.fetchPackageJSON(ctx, pkgName, version)
	// Release sem -- HTTP work done, recursive work doesn't need it
	<-sem
	if err != nil {
		return err
	}

	r.addPackageImports(im, mu, pkgName, version, pkg)

	if r.resolveScope && (r.maxDepth == 0 || depth < r.maxDepth) && len(pkg.Dependencies) > 0 {
		scopeKey := r.template.Expand(pkgName, version, "")
		if !strings.HasSuffix(scopeKey, "/") {
			scopeKey += "/"
		}

		scopeEntries := make(map[string]string)
		var wg sync.WaitGroup
		var scopeMu sync.Mutex

		for depName, depVer := range pkg.Dependencies {
			if slices.Contains(r.excludePackages, depName) {
				continue
			}
			wg.Add(1)
			go func(name, ver string) {
				defer wg.Done()

				if err := r.acquireSem(ctx, sem); err != nil {
					return
				}

				resolvedVer, err := r.resolveVersion(ctx, name, ver)
				if err != nil {
					<-sem
					if r.logger != nil {
						r.logger.Warning("Failed to resolve transitive dep %s@%s: %v", name, ver, err)
					}
					return
				}

				depPkg, err := r.fetchPackageJSON(ctx, name, resolvedVer)
				<-sem
				if err != nil {
					if r.logger != nil {
						r.logger.Warning("Failed to fetch %s@%s: %v", name, resolvedVer, err)
					}
					return
				}

				entries := r.buildPackageImports(name, resolvedVer, depPkg)
				scopeMu.Lock()
				maps.Copy(scopeEntries, entries)
				scopeMu.Unlock()

				if err := r.resolvePackage(ctx, im, mu, visited, sem, name, resolvedVer, depth+1); err != nil {
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

// acquireSem blocks until a semaphore slot is available or the context is cancelled.
func (r *Resolver) acquireSem(ctx context.Context, sem chan struct{}) error {
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// resolveVersion wraps registry.ResolveVersion with an optional per-request timeout.
func (r *Resolver) resolveVersion(ctx context.Context, pkgName, versionRange string) (string, error) {
	reqCtx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.registry.ResolveVersion(reqCtx, pkgName, versionRange)
}

// fetchPackageJSON fetches and parses a package.json from the CDN.
func (r *Resolver) fetchPackageJSON(ctx context.Context, pkgName, version string) (*packagejson.PackageJSON, error) {
	return r.cache.GetOrLoad(pkgName, version, func() (*packagejson.PackageJSON, error) {
		url := r.buildPackageJSONURL(pkgName, version)
		reqCtx, cancel := r.withTimeout(ctx)
		defer cancel()
		data, err := r.fetcher.Fetch(reqCtx, url)
		if err != nil {
			return nil, err
		}
		return packagejson.Parse(data)
	})
}

// withTimeout derives a context with the configured request timeout.
// Returns the original context and a no-op cancel if no timeout is set.
func (r *Resolver) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.requestTimeout > 0 {
		return context.WithTimeout(ctx, r.requestTimeout)
	}
	return ctx, func() {}
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
	for _, e := range pkg.ImportMapEntries(opts) {
		imports[pkgName+e.Key] = r.template.Expand(pkgName, version, e.Path)
	}
	return imports
}
