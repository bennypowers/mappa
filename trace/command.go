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
	// Errors contains tracing errors encountered while building the graph.
	// ModuleTraceError values are fatal; callers use ClassifyGraphErrors.
	Errors []error
}

// SpecifiersResult holds the legacy specifiers format output.
type SpecifiersResult struct {
	Entrypoints    []string    `json:"entrypoints"`
	Modules        []string    `json:"modules"`
	BareSpecifiers []string    `json:"bare_specifiers"`
	Packages       []string    `json:"packages"`
	Issues         []IssueJSON `json:"issues,omitempty"`
	// Errors carries fatal graph errors for callers that need to fail the
	// command. It is deliberately excluded from the JSON contract.
	Errors []error `json:"-"`
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
	File    string                       `json:"file"`
	Imports map[string]string            `json:"imports"`
	Scopes  map[string]map[string]string `json:"scopes,omitempty"`
	Error   string                       `json:"error,omitempty"`
	// Warnings are printed to stderr by the CLI and deliberately excluded
	// from the NDJSON contract, which reserves "warnings" for GraphErrors.
	Warnings []Warning `json:"-"`
	// GraphErrors surfaces non-fatal tracing errors (unresolved or unreadable
	// modules) so callers do not silently receive a partial map. Fatal errors
	// go in Error, so the JSON contract maps them to "error" and "warnings".
	GraphErrors []string `json:"warnings,omitempty"`
}

// Warning represents a single import validation warning.
type Warning struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Specifier string `json:"specifier"`
	IssueType string `json:"issue_type"`
	Package   string `json:"package"`
}

// Setup bundles the tracing prerequisites shared by the trace and inject
// commands for one package root.
type Setup struct {
	// WorkspaceRoot is the discovered workspace root for the package.
	WorkspaceRoot string
	// Tracer is configured with the shared package cache, node_modules
	// traversal, conditions, and self-package awareness.
	Tracer *Tracer
	// Package is the parsed root package.json, or nil if it is missing or
	// malformed.
	Package *packagejson.PackageJSON
	// PackageErr is the error from parsing the root package.json, if any.
	PackageErr error
	// PackageCache is shared by the tracer and every resolver so each
	// manifest is parsed once.
	PackageCache packagejson.Cache
}

// NewSetup discovers the workspace root, parses the root package.json, and
// builds a tracer for absRoot. Parsing a missing or malformed package.json is
// tolerated: Package/PackageErr report the outcome and callers decide whether
// to warn, validate, or continue.
func NewSetup(osfs fs.FileSystem, absRoot string, conditions []string, logger resolve.Logger) *Setup {
	workspaceRoot := resolve.FindWorkspaceRoot(osfs, absRoot)
	nodeModulesPath := filepath.Join(workspaceRoot, "node_modules")

	pkgPath := filepath.Join(absRoot, "package.json")
	pkg, pkgErr := packagejson.ParseFile(osfs, pkgPath)

	pkgCache := packagejson.NewMemoryCache()
	tracer := NewTracer(osfs, absRoot).
		WithPackageCache(pkgCache).
		WithLogger(logger).
		WithNodeModules(nodeModulesPath).
		WithConditions(conditions)

	// Workspace packages are legitimate trace roots even though they live
	// outside the package root.
	if discovered, err := resolve.DiscoverWorkspacePackages(osfs, workspaceRoot); err == nil && len(discovered) > 0 {
		paths := make([]string, 0, len(discovered))
		for _, workspacePkg := range discovered {
			paths = append(paths, workspacePkg.Path)
		}
		tracer = tracer.WithAllowedPaths(paths)
	}

	if pkgErr == nil && pkg != nil && pkg.Name != "" {
		tracer = tracer.WithSelfPackage(pkg, absRoot)
	}

	return &Setup{
		WorkspaceRoot: workspaceRoot,
		Tracer:        tracer,
		Package:       pkg,
		PackageErr:    pkgErr,
		PackageCache:  pkgCache,
	}
}

// TraceSingle traces a single HTML file and generates an import map.
func TraceSingle(osfs fs.FileSystem, htmlFile, absRoot string, opts Options) (*SingleResult, error) {
	setup := NewSetup(osfs, absRoot, opts.Conditions, opts.Logger)

	graph, err := setup.Tracer.TraceHTML(htmlFile)
	if err != nil {
		return nil, err
	}

	// Validate imports against dependencies if package.json was parsed
	var issues []ImportIssue
	if setup.PackageErr == nil && setup.Package != nil {
		issues = graph.ValidateImports(osfs, absRoot, setup.Package.Name, setup.Package.Dependencies, setup.Package.DevDependencies)
	}

	result := &SingleResult{
		Issues: issues,
		Errors: RelativizeGraphErrors(graph.Errors, setup.WorkspaceRoot),
	}

	// Get bare specifiers once for reuse
	bareSpecs := graph.BareSpecifiers()

	// Build resolver for the traced packages
	templateArg := opts.Template
	if templateArg == "" {
		templateArg = resolve.DefaultLocalTemplate
	}

	resolver := local.New(osfs, opts.Logger).WithPackageCache(setup.PackageCache)
	resolver, err = resolver.WithTemplate(templateArg)
	if err != nil {
		return nil, err
	}
	if len(opts.Conditions) > 0 {
		resolver = resolver.WithConditions(opts.Conditions)
	}

	result.ImportMap, err = ResolveForSpecifiers(resolver, setup.WorkspaceRoot, bareSpecs, setup.Package)
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
// opts supplies export conditions and the warning logger so the traced graph
// matches the import-map format.
func TraceSpecifiers(osfs fs.FileSystem, htmlFile, absRoot string, opts Options) (*SpecifiersResult, []ImportIssue, error) {
	setup := NewSetup(osfs, absRoot, opts.Conditions, opts.Logger)

	graph, err := setup.Tracer.TraceHTML(htmlFile)
	if err != nil {
		return nil, nil, err
	}

	// Validate imports against dependencies if package.json was parsed
	var issues []ImportIssue
	if setup.PackageErr == nil && setup.Package != nil {
		issues = graph.ValidateImports(osfs, absRoot, setup.Package.Name, setup.Package.Dependencies, setup.Package.DevDependencies)
	}

	// Convert absolute paths to relative for portable output
	relativize := func(absPath string) string {
		if rel, err := filepath.Rel(setup.WorkspaceRoot, absPath); err == nil {
			return rel
		}
		return absPath
	}

	entrypoints := make([]string, 0, len(graph.Entrypoints))
	for _, ep := range graph.Entrypoints {
		entrypoints = append(entrypoints, relativize(ep))
	}

	result := &SpecifiersResult{
		Entrypoints:    entrypoints,
		Modules:        make([]string, 0, len(graph.Modules)),
		BareSpecifiers: graph.BareSpecifiers(),
		Packages:       graph.PackageNames(),
		Errors:         RelativizeGraphErrors(graph.Errors, setup.WorkspaceRoot),
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

		// Find the workspace root, parse package.json once for self-referencing
		// imports and dependency validation, and build the shared tracer.
		setup := NewSetup(osfs, absRoot, opts.Conditions, opts.Logger)
		workspaceRoot := setup.WorkspaceRoot
		tracer := setup.Tracer
		pkg := setup.Package

		// Create shared base resolver with template, conditions, and package cache
		baseResolver := local.New(osfs, opts.Logger).WithPackageCache(setup.PackageCache)
		baseResolver, err := baseResolver.WithTemplate(templateArg)
		if err != nil {
			for _, file := range files {
				results <- BatchResult{
					File:    file,
					Imports: make(map[string]string),
					Error:   err.Error(),
				}
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
		memo := NewResolutionMemo()

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
func traceFileForBatch(tracer *Tracer, osfs fs.FileSystem, htmlFile, absRoot, workspaceRoot string, baseResolver *local.Resolver, pkg *packagejson.PackageJSON, memo *ResolutionMemo) BatchResult {
	result := BatchResult{File: htmlFile, Imports: make(map[string]string)}

	graph, err := tracer.TraceHTML(htmlFile)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	// Read/parse failures and containment failures make the map incomplete and
	// are fatal. Export restriction failures are expected for deep imports and
	// remain warnings; the resolvers emit exact fallback entries for those.
	fatalErrs, nonFatalErrs := ClassifyGraphErrors(graph.Errors)
	for _, graphErr := range nonFatalErrs {
		result.GraphErrors = append(result.GraphErrors, RelativizeGraphErrors([]error{graphErr}, workspaceRoot)[0].Error())
	}
	if len(fatalErrs) > 0 {
		result.Error = errors.Join(RelativizeGraphErrors(fatalErrs, workspaceRoot)...).Error()
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

	resolved, err := memo.Resolve(MemoKey(bareSpecs), func() (*importmap.ImportMap, error) {
		return ResolveForSpecifiers(baseResolver, workspaceRoot, bareSpecs, pkg)
	})
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Resolve returns a clone owned by this result.
	result.Imports = resolved.Imports
	result.Scopes = resolved.Scopes

	return result
}
