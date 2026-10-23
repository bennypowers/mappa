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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mappacdn "bennypowers.dev/mappa/cdn"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
)

// Resolver generates import maps pointing to CDN-hosted packages.
type Resolver struct {
	fetcher         mappacdn.Fetcher
	provider        mappacdn.Provider
	registry        *mappacdn.Registry
	template        *resolve.Template
	cache           *mappacdn.PackageCache
	logger          resolve.Logger
	conditions      []string
	includeDev      bool
	excludePackages []string
	maxDepth        int           // Maximum dependency depth (0 = unlimited)
	resolveScope    bool          // Whether to resolve transitive dependencies as scopes
	requestTimeout  time.Duration // Per-request timeout (0 = no timeout)
	concurrency     int           // Max concurrent goroutines across all depths
	allowPartial    bool          // Return a partial map plus warnings instead of an error
}

// DefaultRequestTimeout bounds each registry and CDN request.
const DefaultRequestTimeout = 30 * time.Second

// New creates a new CDN resolver with default settings.
func New(fetcher mappacdn.Fetcher) *Resolver {
	tmpl, _ := resolve.ParseTemplate(mappacdn.DefaultProvider.ModuleTemplate)
	return &Resolver{
		fetcher:  fetcher,
		provider: mappacdn.DefaultProvider,
		registry: mappacdn.NewRegistry(fetcher),
		template: tmpl,
		// At the 10 MiB manifest cap, 100 entries bound the theoretical worst
		// case to about 1 GiB. Typical manifests are a few KiB, so this is
		// ample for large dependency graphs without risking unbounded growth.
		cache:          mappacdn.NewPackageCache(100),
		resolveScope:   true,
		concurrency:    10,
		requestTimeout: DefaultRequestTimeout,
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
// 0 disables the timeout. The default is DefaultRequestTimeout (30s).
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

// WithAllowPartial controls what happens when some dependencies cannot be
// resolved. By default ResolvePackageJSON returns the partial map together with
// an error. When allowPartial is true, it instead returns the partial map with
// its Warnings field populated and a nil error.
func (r *Resolver) WithAllowPartial(allowPartial bool) *Resolver {
	c := r.clone()
	c.allowPartial = allowPartial
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
		allowPartial:    r.allowPartial,
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
// When the returned error is non-nil, the returned import map may be partial:
// dependencies that resolved successfully are included, but callers should
// treat the map as incomplete. With WithAllowPartial(true), the partial map is
// returned with Warnings populated and a nil error instead.
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

	if len(deps) > resolve.MaxDependencyEntries {
		return nil, resolve.DependencyCapError("package.json", len(deps))
	}
	if len(deps) == 0 {
		return result, nil
	}

	state := newResolveState(ctx, r, result)
	// Sort root dependencies so work (and therefore failures) happen in a
	// deterministic order.
	for _, name := range slices.Sorted(maps.Keys(deps)) {
		state.enqueue(&resolveTask{name: name, versionRange: deps[name]})
	}

	// The worker pool bounds goroutines and HTTP requests across every depth,
	// not just the declared dependencies.
	workers := max(r.concurrency, 1)
	for range workers {
		state.workers.Add(1)
		go state.worker()
	}
	state.workers.Wait()

	// Clean up empty scopes
	if len(result.Scopes) == 0 {
		result.Scopes = nil
	}

	if len(state.errs) > 0 {
		// Errors are recorded in completion order; sort by key to keep the
		// joined error deterministic.
		keys := slices.Sorted(maps.Keys(state.errs))
		errs := make([]error, len(keys))
		warnings := make([]string, len(keys))
		for i, key := range keys {
			errs[i] = state.errs[key]
			warnings[i] = state.errs[key].Error()
		}
		if r.allowPartial {
			result.Warnings = warnings
			return result, nil
		}
		return result, errors.Join(errs...)
	}
	return result, nil
}

// resolveTask describes one package to resolve. parentScope is the scope key
// of the package that depends on it, and is empty for declared dependencies.
type resolveTask struct {
	name         string
	versionRange string
	depth        int
	parentScope  string
}

// resolveState holds the shared work queue, visited set, and error collector
// for a single ResolvePackageJSON call.
type resolveState struct {
	ctx      context.Context
	resolver *Resolver
	im       *importmap.ImportMap
	mu       sync.Mutex
	errs     map[string]error
	visited  sync.Map
	queue    *taskQueue
	pending  atomic.Int64
	workers  sync.WaitGroup
	// manifests coalesces package.json fetches for the lifetime of this
	// resolution pass. The shared PackageCache is size-bounded, so a large
	// graph can evict and refetch a shared dependency; this map guarantees
	// each manifest is fetched at most once per run.
	manifests sync.Map // map[string]*manifestEntry
}

// manifestEntry is one package.json fetch shared by concurrent tasks.
type manifestEntry struct {
	once sync.Once
	pkg  *packagejson.PackageJSON
	err  error
}

// packageJSON returns the manifest for pkgName@version, fetching it at most
// once per resolution pass. Failures are not retained so another branch can
// retry.
func (s *resolveState) packageJSON(ctx context.Context, pkgName, version string) (*packagejson.PackageJSON, error) {
	key := pkgName + "@" + version
	entry := &manifestEntry{}
	actual, _ := s.manifests.LoadOrStore(key, entry)
	entry = actual.(*manifestEntry)
	entry.once.Do(func() {
		entry.pkg, entry.err = s.resolver.fetchPackageJSON(ctx, pkgName, version)
	})
	if entry.err != nil {
		s.manifests.CompareAndDelete(key, entry)
		return nil, entry.err
	}
	return entry.pkg, nil
}

func newResolveState(ctx context.Context, resolver *Resolver, im *importmap.ImportMap) *resolveState {
	return &resolveState{
		ctx:      ctx,
		resolver: resolver,
		im:       im,
		errs:     make(map[string]error),
		queue:    newTaskQueue(),
	}
}

// enqueue adds a task and counts it as pending.
func (s *resolveState) enqueue(task *resolveTask) {
	s.pending.Add(1)
	s.queue.push(task)
}

func (s *resolveState) worker() {
	defer s.workers.Done()
	for {
		task, ok := s.queue.pop()
		if !ok {
			return
		}
		s.process(task)
		// Once the caller's context is done, stop draining the queue. Tasks
		// already in flight record their own cancellation error; the rest are
		// abandoned so ResolvePackageJSON returns promptly.
		if s.ctx.Err() != nil {
			s.queue.close()
		}
		if s.pending.Add(-1) == 0 {
			s.queue.close()
		}
	}
}

// process resolves one package, records its imports in the appropriate scope,
// and enqueues its dependencies. Both successful and failed transitive
// resolutions are aggregated into the returned error.
func (s *resolveState) process(task *resolveTask) {
	r := s.resolver
	ctx := s.ctx

	if err := ctx.Err(); err != nil {
		s.recordError(task, err)
		return
	}

	version, err := r.resolveVersion(ctx, task.name, task.versionRange)
	if err != nil {
		s.recordError(task, err)
		return
	}

	pkg, err := s.packageJSON(ctx, task.name, version)
	if err != nil {
		// The package is not marked visited, so another branch referencing it
		// can retry the fetch.
		s.recordError(task, err)
		return
	}

	if task.depth == 0 {
		// Depth 0 always adds the declared dependency's top-level imports,
		// even if a transitive scope already visited the same version.
		r.addPackageImports(s.im, &s.mu, task.name, version, pkg)
	} else if task.parentScope != "" {
		entries := r.buildPackageImports(task.name, version, pkg)
		s.mu.Lock()
		if s.im.Scopes == nil {
			s.im.Scopes = make(map[string]map[string]string)
		}
		if s.im.Scopes[task.parentScope] == nil {
			s.im.Scopes[task.parentScope] = make(map[string]string)
		}
		maps.Copy(s.im.Scopes[task.parentScope], entries)
		s.mu.Unlock()
	}

	// A package's own dependencies only need enqueueing once per resolved
	// version, and only by a task that may actually traverse them. A task at
	// the depth limit still contributes imports and scope entries above, but
	// must not consume the visit: a later, shallower task may still need to
	// traverse this version.
	canTraverse := r.resolveScope && len(pkg.Dependencies) > 0 &&
		(r.maxDepth == 0 || task.depth < r.maxDepth)
	cacheKey := task.name + "@" + version
	if canTraverse {
		if _, alreadyVisited := s.visited.LoadOrStore(cacheKey, true); alreadyVisited {
			return
		}
	}

	if !canTraverse {
		return
	}
	if len(pkg.Dependencies) > resolve.MaxDependencyEntries {
		s.recordError(task, fmt.Errorf("declares %d dependencies (maximum %d)", len(pkg.Dependencies), resolve.MaxDependencyEntries))
		return
	}

	scopeKey := r.template.Expand(task.name, version, "")
	if !strings.HasSuffix(scopeKey, "/") {
		scopeKey += "/"
	}

	for _, depName := range slices.Sorted(maps.Keys(pkg.Dependencies)) {
		if slices.Contains(r.excludePackages, depName) {
			continue
		}
		s.enqueue(&resolveTask{
			name:         depName,
			versionRange: pkg.Dependencies[depName],
			depth:        task.depth + 1,
			parentScope:  scopeKey,
		})
	}
}

// recordError stores an error keyed deterministically, prefixed with the
// parent scope and dependency name for transitive failures.
func (s *resolveState) recordError(task *resolveTask, err error) {
	var key string
	if task.parentScope == "" {
		key = task.name + "@" + task.versionRange
		err = fmt.Errorf("%s@%s: %w", task.name, task.versionRange, err)
	} else {
		key = task.parentScope + " " + task.name + "@" + task.versionRange
		err = fmt.Errorf("%s: %s@%s: %w", strings.TrimSuffix(task.parentScope, "/"), task.name, task.versionRange, err)
	}

	s.mu.Lock()
	s.errs[key] = err
	s.mu.Unlock()

	if s.resolver.logger != nil {
		s.resolver.logger.Warning("Failed to resolve %s@%s: %v", task.name, task.versionRange, err)
	}
}

// taskQueue is an unbounded FIFO queue with blocking pops. An unbounded queue
// avoids deadlock when every worker is producing more work.
type taskQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []*resolveTask
	head   int
	closed bool
}

func newTaskQueue() *taskQueue {
	q := &taskQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *taskQueue) push(task *resolveTask) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, task)
		q.cond.Signal()
	}
	q.mu.Unlock()
}

// pop blocks until a task is available or the queue is closed.
func (q *taskQueue) pop() (*resolveTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.head >= len(q.items) && !q.closed {
		q.cond.Wait()
	}
	if q.closed || q.head >= len(q.items) {
		return nil, false
	}
	task := q.items[q.head]
	q.items[q.head] = nil
	q.head++
	return task, true
}

func (q *taskQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

// tryPop removes the next task without blocking. It is used to drain a queue
// when no workers are running.
func (q *taskQueue) tryPop() (*resolveTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.head >= len(q.items) {
		return nil, false
	}
	task := q.items[q.head]
	q.items[q.head] = nil
	q.head++
	return task, true
}

// drain processes every queued task until the queue is empty. Tests use it to
// drive work when no worker goroutines are running.
func (s *resolveState) drain() {
	for {
		task, ok := s.queue.tryPop()
		if !ok {
			return
		}
		s.process(task)
		if s.pending.Add(-1) == 0 {
			s.queue.close()
		}
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
		if len(data) > packagejson.MaxPackageJSONSize {
			return nil, fmt.Errorf("package.json for %s@%s exceeds maximum size (%d bytes)",
				pkgName, version, packagejson.MaxPackageJSONSize)
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
