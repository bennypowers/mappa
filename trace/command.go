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

// Package trace provides module graph tracing for ES module import analysis.
// It parses HTML and JavaScript files to extract import dependencies and
// generate minimal import maps.
package trace

import (
	"errors"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
	"bennypowers.dev/mappa/resolve/local"
)

// Options configures the trace command.
type Options struct {
	// Template is the URL template for import map values.
	// Defaults to resolve.DefaultLocalTemplate if empty.
	Template string
	// Conditions is the export condition priority.
	Conditions []string
	// Parallel is the number of parallel workers for batch mode.
	// Defaults to runtime.NumCPU() if <= 0.
	Parallel int
	// Logger receives resolution warnings. If nil, warnings are discarded.
	Logger resolve.Logger
}

// SingleResult holds the result of tracing a single HTML file.
type SingleResult struct {
	// ImportMap is the generated import map.
	ImportMap *importmap.ImportMap
	// Issues contains any validation warnings.
	Issues []ImportIssue
	// SpecifiersResult is populated when format is "specifiers".
	SpecifiersResult *SpecifiersResult
	// Errors contains non-fatal tracing errors (for example, modules that
	// could not be resolved or read) encountered while building the graph.
	Errors []error
}

// SpecifiersResult holds the legacy specifiers format output.
type SpecifiersResult struct {
	Entrypoints    []string    `json:"entrypoints"`
	Modules        []string    `json:"modules"`
	BareSpecifiers []string    `json:"bare_specifiers"`
	Packages       []string    `json:"packages"`
	Issues         []IssueJSON `json:"issues,omitempty"`
}

// IssueJSON is the JSON representation of an ImportIssue.
type IssueJSON struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Specifier string `json:"specifier"`
	Package   string `json:"package"`
	IssueType string `json:"issue_type"`
}

// BatchResult holds the result of tracing a single file in batch mode.
type BatchResult struct {
	File     string                       `json:"file"`
	Imports  map[string]string            `json:"imports"`
	Scopes   map[string]map[string]string `json:"scopes,omitempty"`
	Error    string                       `json:"error,omitempty"`
	Warnings []Warning                    `json:"warnings,omitempty"`
	// GraphErrors surfaces non-fatal tracing errors (unresolved or unreadable
	// modules) so callers do not silently receive a partial map.
	GraphErrors []string `json:"errors,omitempty"`
}

// Warning represents a single import validation warning.
type Warning struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Specifier string `json:"specifier"`
	IssueType string `json:"issue_type"`
	Package   string `json:"package"`
}

// tracerSetup holds common tracing prerequisites.
type tracerSetup struct {
	workspaceRoot string
	tracer        *Tracer
	pkg           *packagejson.PackageJSON
	pkgErr        error
	pkgCache      packagejson.Cache
}

// setupTracer creates common tracing prerequisites for a package root.
func setupTracer(osfs fs.FileSystem, absRoot string, conditions []string, logger resolve.Logger) tracerSetup {
	workspaceRoot := resolve.FindWorkspaceRoot(osfs, absRoot)
	nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")

	pkgPath := filepath.Join(absRoot, "package.json")
	pkg, pkgErr := packagejson.ParseFile(osfs, pkgPath)

	pkgCache := packagejson.NewMemoryCache()
	tracer := NewTracer(osfs, absRoot).WithPackageCache(pkgCache).WithLogger(logger).WithNodeModules(nodeModulesPath).WithConditions(conditions)
	if pkgErr == nil && pkg.Name != "" {
		tracer = tracer.WithSelfPackage(pkg, absRoot)
	}

	return tracerSetup{workspaceRoot, tracer, pkg, pkgErr, pkgCache}
}

// TraceSingle traces a single HTML file and generates an import map.
func TraceSingle(osfs fs.FileSystem, htmlFile, absRoot string, opts Options) (*SingleResult, error) {
	setup := setupTracer(osfs, absRoot, opts.Conditions, opts.Logger)

	graph, err := setup.tracer.TraceHTML(htmlFile)
	if err != nil {
		return nil, err
	}

	// Validate imports against dependencies if package.json was parsed
	var issues []ImportIssue
	if setup.pkgErr == nil {
		issues = graph.ValidateImports(osfs, absRoot, setup.pkg.Name, setup.pkg.Dependencies, setup.pkg.DevDependencies)
	}

	result := &SingleResult{Issues: issues, Errors: graph.Errors}

	// Get bare specifiers once for reuse
	bareSpecs := graph.BareSpecifiers()

	// Build resolver for the traced packages
	templateArg := opts.Template
	if templateArg == "" {
		templateArg = resolve.DefaultLocalTemplate
	}

	resolver := local.New(osfs, opts.Logger).WithPackageCache(setup.pkgCache)
	resolver, err = resolver.WithTemplate(templateArg)
	if err != nil {
		return nil, err
	}
	if len(opts.Conditions) > 0 {
		resolver = resolver.WithConditions(opts.Conditions)
	}

	result.ImportMap, err = ResolveForSpecifiers(resolver, setup.workspaceRoot, bareSpecs, setup.pkg)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// overlayGeneratedImports replaces specifier resolutions with entries from a
// fully generated map where available. This is what makes self-references and
// workspace packages point at their source paths rather than node_modules.
func overlayGeneratedImports(traced map[string]string, generated *importmap.ImportMap, specs []string) {
	if generated == nil {
		return
	}
	for _, spec := range specs {
		if value, ok := generated.Lookup(spec); ok {
			traced[spec] = value
		}
	}
}

// TraceSpecifiers returns the legacy specifiers format for debugging.
func TraceSpecifiers(osfs fs.FileSystem, htmlFile, absRoot string) (*SpecifiersResult, []ImportIssue, error) {
	setup := setupTracer(osfs, absRoot, nil, nil)

	graph, err := setup.tracer.TraceHTML(htmlFile)
	if err != nil {
		return nil, nil, err
	}

	// Validate imports against dependencies if package.json was parsed
	var issues []ImportIssue
	if setup.pkgErr == nil {
		issues = graph.ValidateImports(osfs, absRoot, setup.pkg.Name, setup.pkg.Dependencies, setup.pkg.DevDependencies)
	}

	// Convert absolute paths to relative for portable output
	relativize := func(absPath string) string {
		if rel, err := filepath.Rel(setup.workspaceRoot, absPath); err == nil {
			return rel
		}
		return absPath
	}

	var entrypoints []string
	for _, ep := range graph.Entrypoints {
		entrypoints = append(entrypoints, relativize(ep))
	}

	result := &SpecifiersResult{
		Entrypoints:    entrypoints,
		BareSpecifiers: graph.BareSpecifiers(),
		Packages:       graph.PackageNames(),
	}

	for p := range graph.Modules {
		result.Modules = append(result.Modules, relativize(p))
	}
	sort.Strings(result.Modules)

	for _, issue := range issues {
		result.Issues = append(result.Issues, IssueJSON{
			File:      issue.File,
			Line:      issue.Line,
			Specifier: issue.Specifier,
			Package:   issue.Package,
			IssueType: issue.IssueType.String(),
		})
	}

	return result, issues, nil
}

// TraceBatch traces multiple HTML files in parallel.
// Returns a channel of BatchResults that will be closed when all files are processed.
// Also returns the total count and error count after processing completes.
func TraceBatch(osfs fs.FileSystem, files []string, absRoot string, opts Options) <-chan BatchResult {
	results := make(chan BatchResult, len(files))

	go func() {
		defer close(results)

		parallel := opts.Parallel
		if parallel <= 0 {
			parallel = runtime.NumCPU()
		}

		templateArg := opts.Template
		if templateArg == "" {
			templateArg = resolve.DefaultLocalTemplate
		}

		// Find workspace root once for all files
		workspaceRoot := resolve.FindWorkspaceRoot(osfs, absRoot)
		nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")

		// Parse package.json once for self-referencing imports and dependency validation
		pkgPath := filepath.Join(absRoot, "package.json")
		pkg, _ := packagejson.ParseFile(osfs, pkgPath)

		// One cache shared by the tracer and every per-file resolver.
		pkgCache := packagejson.NewMemoryCache()

		// Create shared tracer with self-package awareness and transitive dependency following
		tracer := NewTracer(osfs, absRoot).WithPackageCache(pkgCache).WithLogger(opts.Logger).WithNodeModules(nodeModulesPath).WithConditions(opts.Conditions)
		if pkg != nil && pkg.Name != "" {
			tracer = tracer.WithSelfPackage(pkg, absRoot)
		}

		// Create shared base resolver with template, conditions, and package cache
		baseResolver := local.New(osfs, opts.Logger).WithPackageCache(pkgCache)
		baseResolver, err := baseResolver.WithTemplate(templateArg)
		if err != nil {
			for _, file := range files {
				results <- BatchResult{File: file, Error: err.Error()}
			}
			return
		}
		if len(opts.Conditions) > 0 {
			baseResolver = baseResolver.WithConditions(opts.Conditions)
		}

		// Create jobs channel
		jobs := make(chan string, len(files))

		// Memoize resolutions by the file's bare-specifier set. Many pages in
		// a batch share the same dependency set.
		memo := &sync.Map{} // map[string]*importmap.ImportMap

		// Start worker goroutines
		var wg sync.WaitGroup
		for range parallel {
			wg.Go(func() {
				for htmlFile := range jobs {
					result := traceFileForBatch(tracer, osfs, htmlFile, absRoot, workspaceRoot, baseResolver, pkg, memo)
					results <- result
				}
			})
		}

		// Send jobs
		for _, file := range files {
			jobs <- file
		}
		close(jobs)

		// Wait for workers
		wg.Wait()
	}()

	return results
}

// traceFileForBatch traces a single file and returns a BatchResult.
func traceFileForBatch(tracer *Tracer, osfs fs.FileSystem, htmlFile, absRoot, workspaceRoot string, baseResolver *local.Resolver, pkg *packagejson.PackageJSON, memo *sync.Map) BatchResult {
	result := BatchResult{File: htmlFile, Imports: make(map[string]string)}

	graph, err := tracer.TraceHTML(htmlFile)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var tracingErrs []error
	for _, graphErr := range graph.Errors {
		// Read/parse failures make the map incomplete and are fatal. Export
		// restriction failures are expected for deep imports and remain
		// warnings; the resolvers emit exact fallback entries for those.
		if strings.HasPrefix(graphErr.Error(), "tracing ") {
			tracingErrs = append(tracingErrs, graphErr)
			continue
		}
		result.GraphErrors = append(result.GraphErrors, graphErr.Error())
	}
	if len(tracingErrs) > 0 {
		result.Error = errors.Join(tracingErrs...).Error()
		return result
	}

	// Validate imports if package.json was parsed
	if pkg != nil {
		issues := graph.ValidateImports(osfs, absRoot, pkg.Name, pkg.Dependencies, pkg.DevDependencies)
		for _, issue := range issues {
			result.Warnings = append(result.Warnings, Warning{
				File:      issue.File,
				Line:      issue.Line,
				Specifier: issue.Specifier,
				IssueType: issue.IssueType.String(),
				Package:   issue.Package,
			})
		}
	}

	// Get bare specifiers once for reuse
	bareSpecs := graph.BareSpecifiers()
	if len(bareSpecs) == 0 {
		result.Imports = make(map[string]string)
		return result
	}

	memoKey := strings.Join(bareSpecs, "\x00")
	simplified, ok := memo.Load(memoKey)
	var resolved *importmap.ImportMap
	if ok {
		resolved = simplified.(*importmap.ImportMap)
	} else {
		var err error
		resolved, err = ResolveForSpecifiers(baseResolver, workspaceRoot, bareSpecs, pkg)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		memo.Store(memoKey, resolved)
	}

	result.Imports = resolved.Imports
	result.Scopes = resolved.Scopes

	return result
}
