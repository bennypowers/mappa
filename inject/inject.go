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

// Package inject provides import map injection for HTML files.
// It traces module imports and writes minimal import maps directly into
// HTML files, updating existing import map script tags or inserting new ones.
package inject

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	"bennypowers.dev/mappa/resolve"
	"bennypowers.dev/mappa/resolve/local"
	"bennypowers.dev/mappa/trace"
)

// Options configures the inject command.
type Options struct {
	// Template is the URL template for import map values.
	Template string
	// Conditions is the export condition priority.
	Conditions []string
	// Parallel is the number of parallel workers for batch mode.
	Parallel int
	// DryRun prevents writing files when true.
	DryRun bool
}

// Result holds the result of injecting into a single file.
type Result struct {
	File     string `json:"file"`
	Modified bool   `json:"modified"`
	Inserted bool   `json:"inserted,omitempty"` // true if new import map, false if replaced
	Error    string `json:"error,omitempty"`
}

// Stats holds aggregate statistics from an inject operation.
type Stats struct {
	Total    int   `json:"total"`
	Updated  int   `json:"updated"`
	Inserted int   `json:"inserted"`
	Skipped  int   `json:"skipped"`
	Errors   int   `json:"errors"`
	Duration int64 `json:"duration_ms"`
}

// InjectBatch injects import maps into multiple HTML files in parallel.
func InjectBatch(osfs fs.FileSystem, files []string, absRoot string, opts Options) <-chan Result {
	results := make(chan Result, len(files))

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

		// Parse package.json once
		pkgPath := filepath.Join(absRoot, "package.json")
		pkg, _ := packagejson.ParseFile(osfs, pkgPath)

		// Create shared tracer
		tracer := trace.NewTracer(osfs, absRoot).WithNodeModules(nodeModulesPath)
		if pkg != nil && pkg.Name != "" {
			tracer = tracer.WithSelfPackage(pkg, absRoot)
		}

		// Create shared base resolver
		pkgCache := packagejson.NewMemoryCache()
		baseResolver := local.New(osfs, nil).WithPackageCache(pkgCache)
		baseResolver, err := baseResolver.WithTemplate(templateArg)
		if err != nil {
			for _, file := range files {
				results <- Result{File: file, Error: err.Error()}
			}
			return
		}
		if len(opts.Conditions) > 0 {
			baseResolver = baseResolver.WithConditions(opts.Conditions)
		}

		// Create jobs channel
		jobs := make(chan string, len(files))

		// Start worker goroutines
		var wg sync.WaitGroup
		for range parallel {
			wg.Go(func() {
				for htmlFile := range jobs {
					result := injectFile(osfs, tracer, htmlFile, workspaceRoot, baseResolver, pkg, opts.DryRun)
					results <- result
				}
			})
		}

		// Send jobs
		for _, file := range files {
			jobs <- file
		}
		close(jobs)

		wg.Wait()
	}()

	return results
}

// injectFile processes a single HTML file and injects/updates its import map.
func injectFile(osfs fs.FileSystem, tracer *trace.Tracer, htmlFile, workspaceRoot string, baseResolver *local.Resolver, pkg *packagejson.PackageJSON, dryRun bool) Result {
	result := Result{File: htmlFile}

	// Read HTML content
	content, err := osfs.ReadFile(htmlFile)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Trace the file to get its import map
	tracedMap, err := traceForInjection(tracer, htmlFile, workspaceRoot, baseResolver, pkg)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Find existing import map tag
	loc := trace.FindImportMapTag(content)

	var existingMap *importmap.ImportMap
	if loc.Found {
		// Parse existing import map for merging
		existingJSON := content[loc.ContentStart:loc.ContentEnd]
		if len(strings.TrimSpace(string(existingJSON))) > 0 {
			existingMap = &importmap.ImportMap{}
			if err := json.Unmarshal(existingJSON, existingMap); err != nil {
				// Warn and skip on parse error
				result.Error = fmt.Sprintf("failed to parse existing import map at line %d: %v", loc.Line, err)
				return result
			}
		}
	}

	// Merge: existing imports preserved, traced imports take precedence
	var mergedMap *importmap.ImportMap
	if existingMap != nil {
		mergedMap = existingMap.Merge(tracedMap)
	} else {
		mergedMap = tracedMap
	}

	// Simplify the merged import map
	mergedMap = mergedMap.Simplify()

	// Generate new HTML content
	newContent, inserted, err := buildNewContent(content, loc, mergedMap)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Check if content actually changed
	if string(newContent) == string(content) {
		return result // No changes needed
	}

	result.Modified = true
	result.Inserted = inserted

	// Write file if not dry-run
	if !dryRun {
		if err := osfs.WriteFile(htmlFile, newContent, 0644); err != nil {
			result.Error = err.Error()
			return result
		}
	}

	return result
}

// traceForInjection traces an HTML file and returns its import map.
func traceForInjection(tracer *trace.Tracer, htmlFile, workspaceRoot string, baseResolver *local.Resolver, pkg *packagejson.PackageJSON) (*importmap.ImportMap, error) {
	graph, err := tracer.TraceHTML(htmlFile)
	if err != nil {
		return nil, err
	}

	// Get bare specifiers
	bareSpecs := graph.BareSpecifiers()
	if len(bareSpecs) == 0 {
		// No bare imports to resolve
		return &importmap.ImportMap{
			Imports: make(map[string]string),
		}, nil
	}

	// Build resolver with traced packages for this file
	resolver := baseResolver.WithPackages(bareSpecs)

	// Include root package exports if traced specifiers reference the root package
	if pkg != nil && pkg.Name != "" {
		for _, spec := range bareSpecs {
			if spec == pkg.Name || strings.HasPrefix(spec, pkg.Name+"/") {
				resolver = resolver.WithIncludeRootExports()
				break
			}
		}
	}

	// Generate full import map for scopes and trailing-slash keys
	generatedMap, err := resolver.Resolve(workspaceRoot)
	if err != nil {
		return nil, err
	}

	// Resolve traced specifiers
	tracedImports := resolver.ResolveSpecifiers(workspaceRoot, bareSpecs)

	// Add trailing-slash keys from generated imports
	for key, value := range generatedMap.Imports {
		if strings.HasSuffix(key, "/") {
			tracedImports[key] = value
		}
	}

	// Build and return the import map
	return &importmap.ImportMap{
		Imports: tracedImports,
		Scopes:  generatedMap.Scopes,
	}, nil
}

// buildNewContent generates new HTML content with the import map inserted or replaced.
func buildNewContent(content []byte, loc trace.ImportMapLocation, im *importmap.ImportMap) ([]byte, bool, error) {
	importMapJSON := im.ToJSON()

	if loc.Found {
		// Replace existing import map content
		var newContent []byte
		newContent = append(newContent, content[:loc.ContentStart]...)
		newContent = append(newContent, '\n')
		newContent = append(newContent, importMapJSON...)
		newContent = append(newContent, '\n')
		newContent = append(newContent, content[loc.ContentEnd:]...)
		return newContent, false, nil
	}

	// Insert new import map
	insertPoint := trace.FindInsertPoint(content)
	if !insertPoint.Found {
		return nil, false, fmt.Errorf("could not find insertion point (no <head> tag)")
	}

	// Build the import map tag with proper indentation
	var tag strings.Builder
	tag.WriteString("<script type=\"importmap\">\n")
	tag.WriteString(importMapJSON)
	tag.WriteString("\n")
	tag.WriteString(insertPoint.Indent)
	tag.WriteString("</script>\n")
	tag.WriteString(insertPoint.Indent)

	// Insert at the found position
	var newContent []byte
	newContent = append(newContent, content[:insertPoint.Offset]...)
	newContent = append(newContent, tag.String()...)
	newContent = append(newContent, content[insertPoint.Offset:]...)

	return newContent, true, nil
}
