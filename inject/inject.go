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
	"errors"
	"fmt"
	"os"
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
// Note: Some fields (Template, Conditions, Parallel) overlap with trace.Options.
// This is intentional to keep the inject package's public API self-contained
// and avoid coupling to internal trace implementation details.
type Options struct {
	// Template is the URL template for import map values.
	Template string
	// Conditions is the export condition priority.
	Conditions []string
	// Parallel is the number of parallel workers for batch mode.
	Parallel int
	// DryRun prevents writing files when true.
	DryRun bool
	// Logger receives resolution warnings. If nil, warnings are discarded.
	Logger resolve.Logger
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

		// Find workspace root, parse package.json, and build a shared tracer
		// (conditions included) exactly as the trace command does.
		setup := trace.NewSetup(osfs, absRoot, opts.Conditions, opts.Logger)
		workspaceRoot := setup.WorkspaceRoot
		pkg := setup.Package
		if setup.PackageErr != nil && osfs.Exists(filepath.Join(absRoot, "package.json")) {
			// File exists but is malformed - warn but continue
			fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", filepath.Join(absRoot, "package.json"), setup.PackageErr)
		}
		tracer := setup.Tracer

		// Create shared base resolver
		baseResolver := local.New(osfs, opts.Logger).WithPackageCache(setup.PackageCache)
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

		// Memoize resolutions by the file's bare-specifier set. Many pages in
		// a batch share the same dependency set.
		memo := &sync.Map{} // map[string]*importmap.ImportMap

		// Create jobs channel
		jobs := make(chan string, len(files))

		// Start worker goroutines
		var wg sync.WaitGroup
		for range parallel {
			wg.Go(func() {
				for htmlFile := range jobs {
					result := injectFile(osfs, tracer, htmlFile, workspaceRoot, baseResolver, pkg, opts, memo)
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
func injectFile(osfs fs.FileSystem, tracer *trace.Tracer, htmlFile, workspaceRoot string, baseResolver *local.Resolver, pkg *packagejson.PackageJSON, opts Options, memo *sync.Map) Result {
	result := Result{File: htmlFile}

	// Read HTML content
	content, err := osfs.ReadFile(htmlFile)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Trace the file to get its import map
	tracedMap, err := traceForInjection(tracer, htmlFile, workspaceRoot, baseResolver, pkg, opts.Logger, memo)
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

	// Nothing to inject: leave the document untouched rather than writing an
	// empty import map (an empty script body is not valid JSON).
	if !hasEntries(mergedMap) {
		return result
	}

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
	if !opts.DryRun {
		var err error
		if atomic, ok := osfs.(fs.AtomicWriter); ok {
			err = atomic.WriteFileAtomic(htmlFile, newContent, 0644)
		} else {
			err = osfs.WriteFile(htmlFile, newContent, 0644)
		}
		if err != nil {
			result.Error = err.Error()
			return result
		}
	}

	return result
}

// traceForInjection traces an HTML file and returns its import map.
//
// Fatal graph errors (unreadable or unparseable modules) are returned so the
// caller records a failure instead of claiming a partial map succeeded.
// Non-fatal resolution warnings are logged when logger is non-nil. The
// returned map may be shared through memo and must be treated as read-only.
func traceForInjection(tracer *trace.Tracer, htmlFile, workspaceRoot string, baseResolver *local.Resolver, pkg *packagejson.PackageJSON, logger resolve.Logger, memo *sync.Map) (*importmap.ImportMap, error) {
	graph, err := tracer.TraceHTML(htmlFile)
	if err != nil {
		return nil, err
	}

	fatal, nonFatal := trace.ClassifyGraphErrors(graph.Errors)
	for _, graphErr := range nonFatal {
		if logger != nil {
			logger.Warning("%v", graphErr)
		}
	}
	if len(fatal) > 0 {
		return nil, errors.Join(fatal...)
	}

	bareSpecs := graph.BareSpecifiers()
	memoKey := strings.Join(bareSpecs, "\x00")
	if cached, ok := memo.Load(memoKey); ok {
		return cached.(*importmap.ImportMap), nil
	}

	resolved, err := trace.ResolveForSpecifiers(baseResolver, workspaceRoot, bareSpecs, pkg)
	if err != nil {
		return nil, err
	}
	memo.Store(memoKey, resolved)
	return resolved, nil
}

// indentLines prefixes each non-empty line in s with the given indent.
func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// buildNewContent generates new HTML content with the import map inserted or replaced.
func buildNewContent(content []byte, loc trace.ImportMapLocation, im *importmap.ImportMap) ([]byte, bool, error) {
	// Format("json") normalizes an empty map to "{}", which is valid JSON.
	importMapJSON := im.Format("json")

	if loc.Found {
		// Replace existing import map content
		// Extract the indent from the closing tag line
		indent := extractIndent(content, loc.TagEnd)
		indentedJSON := indentLines(importMapJSON, indent)

		var newContent []byte
		newContent = append(newContent, content[:loc.ContentStart]...)
		newContent = append(newContent, '\n')
		newContent = append(newContent, indentedJSON...)
		newContent = append(newContent, '\n')
		newContent = append(newContent, content[loc.ContentEnd:]...)
		return newContent, false, nil
	}

	// Insert new import map
	insertPoint := trace.FindInsertPoint(content)
	if !insertPoint.Found {
		return nil, false, fmt.Errorf("could not find insertion point (no <head> tag)")
	}

	// Indent JSON to match the tag indentation
	indentedJSON := indentLines(importMapJSON, insertPoint.Indent)

	// Build the import map tag with proper indentation
	var tag strings.Builder
	tag.WriteString(insertPoint.Indent)
	tag.WriteString("<script type=\"importmap\">\n")
	tag.WriteString(indentedJSON)
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

// hasEntries reports whether the import map contains anything to write.
func hasEntries(im *importmap.ImportMap) bool {
	return im != nil && (len(im.Imports) > 0 || len(im.Scopes) > 0 || len(im.Integrity) > 0)
}

// extractIndent extracts the leading whitespace from the line containing the given offset.
func extractIndent(content []byte, offset int) string {
	// Find the start of the line
	lineStart := offset
	for lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart--
	}

	// Extract leading whitespace
	var indent strings.Builder
	for i := lineStart; i < len(content) && (content[i] == ' ' || content[i] == '\t'); i++ {
		indent.WriteByte(content[i])
	}
	return indent.String()
}
