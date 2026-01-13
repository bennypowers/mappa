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
package trace

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"bennypowers.dev/mappa/fs"
)

// ModuleGraph represents the complete module dependency graph.
type ModuleGraph struct {
	// Entrypoints are the starting modules (from HTML scripts or explicit entry)
	Entrypoints []string

	// Modules maps module paths to their parsed information
	Modules map[string]*Module

	// Errors collects non-fatal errors encountered during tracing
	Errors []error

	// bareSpecifiers collects all bare import specifiers (need to be resolved)
	bareSpecifiers map[string]bool
}

// Module represents a parsed module in the graph.
type Module struct {
	Path    string         // Path to the module file
	Imports []ModuleImport // All imports found in the module
	Traced  bool           // Whether this module has been fully traced
}

// Tracer builds module graphs from HTML and JavaScript entrypoints.
type Tracer struct {
	fs      fs.FileSystem
	rootDir string
}

// NewTracer creates a new Tracer for the given root directory.
func NewTracer(fs fs.FileSystem, rootDir string) *Tracer {
	return &Tracer{
		fs:      fs,
		rootDir: rootDir,
	}
}

// TraceHTML parses an HTML file and traces all module scripts.
func (t *Tracer) TraceHTML(htmlPath string) (*ModuleGraph, error) {
	content, err := t.fs.ReadFile(htmlPath)
	if err != nil {
		return nil, err
	}

	scripts, err := ExtractScripts(content)
	if err != nil {
		return nil, err
	}

	graph := &ModuleGraph{
		Modules:        make(map[string]*Module),
		bareSpecifiers: make(map[string]bool),
	}

	htmlDir := filepath.Dir(htmlPath)

	for _, script := range scripts {
		if script.Type != "module" {
			continue
		}

		if script.Src != "" {
			// External module script - trace it
			modulePath := t.resolvePath(htmlDir, script.Src)
			graph.Entrypoints = append(graph.Entrypoints, modulePath)
			if err := t.traceModule(graph, modulePath); err != nil {
				graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", modulePath, err))
				continue
			}
		} else if script.Inline {
			// Inline module - collect its imports
			for _, imp := range script.Imports {
				if isBareSpecifier(imp) {
					graph.bareSpecifiers[imp] = true
				} else {
					// Relative import from inline script
					modulePath := t.resolvePath(htmlDir, imp)
					if err := t.traceModule(graph, modulePath); err != nil {
						graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", modulePath, err))
						continue
					}
				}
			}
		}
	}

	return graph, nil
}

// TraceModule traces a single module and all its dependencies.
func (t *Tracer) TraceModule(modulePath string) (*ModuleGraph, error) {
	graph := &ModuleGraph{
		Entrypoints:    []string{modulePath},
		Modules:        make(map[string]*Module),
		bareSpecifiers: make(map[string]bool),
	}

	if err := t.traceModule(graph, modulePath); err != nil {
		return nil, err
	}

	return graph, nil
}

// traceModule recursively traces a module and its dependencies.
func (t *Tracer) traceModule(graph *ModuleGraph, modulePath string) error {
	// Already traced?
	if mod, exists := graph.Modules[modulePath]; exists && mod.Traced {
		return nil
	}

	// Read the module
	content, err := t.fs.ReadFile(modulePath)
	if err != nil {
		return err
	}

	// Parse imports
	imports, err := ExtractImports(content)
	if err != nil {
		return err
	}

	// Record this module
	mod := &Module{
		Path:    modulePath,
		Imports: imports,
		Traced:  true,
	}
	graph.Modules[modulePath] = mod

	// Process imports
	moduleDir := filepath.Dir(modulePath)
	for _, imp := range imports {
		if isBareSpecifier(imp.Specifier) {
			graph.bareSpecifiers[imp.Specifier] = true
		} else {
			// Relative or absolute path - resolve and trace
			depPath := t.resolvePath(moduleDir, imp.Specifier)
			if err := t.traceModule(graph, depPath); err != nil {
				graph.Errors = append(graph.Errors, fmt.Errorf("tracing %s: %w", depPath, err))
				continue
			}
		}
	}

	return nil
}

// resolvePath resolves a specifier relative to a base directory.
// For web-style paths:
// - "./foo" and "../foo" are resolved relative to baseDir
// - "/foo" is resolved relative to rootDir (web-style absolute)
func (t *Tracer) resolvePath(baseDir, specifier string) string {
	if strings.HasPrefix(specifier, "/") {
		// Web-style absolute path - relative to root
		return filepath.Join(t.rootDir, specifier)
	}
	// Relative path (./ or ../ or no prefix)
	return filepath.Join(baseDir, specifier)
}

// isBareSpecifier returns true if the specifier is a bare module specifier
// (needs to be resolved via import map or node_modules).
func isBareSpecifier(specifier string) bool {
	// Bare specifiers don't start with ./, ../, or /
	if specifier == "" {
		return false
	}
	if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
		return false
	}
	if strings.HasPrefix(specifier, "/") {
		return false
	}
	// Check for URL schemes
	if strings.Contains(specifier, "://") {
		return false
	}
	return true
}

// BareSpecifiers returns a sorted slice of all bare specifiers found.
func (g *ModuleGraph) BareSpecifiers() []string {
	specifiers := make([]string, 0, len(g.bareSpecifiers))
	for spec := range g.bareSpecifiers {
		specifiers = append(specifiers, spec)
	}
	sort.Strings(specifiers)
	return specifiers
}

// PackageNames extracts sorted package names from bare specifiers.
// e.g., "lit/decorators.js" -> "lit"
func (g *ModuleGraph) PackageNames() []string {
	packages := make(map[string]bool)
	for spec := range g.bareSpecifiers {
		pkgName := getPackageName(spec)
		packages[pkgName] = true
	}

	result := make([]string, 0, len(packages))
	for pkg := range packages {
		result = append(result, pkg)
	}
	sort.Strings(result)
	return result
}

// getPackageName extracts the package name from a bare specifier.
func getPackageName(specifier string) string {
	// Handle scoped packages: @scope/package/path -> @scope/package
	if strings.HasPrefix(specifier, "@") {
		parts := strings.SplitN(specifier, "/", 3)
		if len(parts) >= 2 {
			return path.Join(parts[0], parts[1])
		}
		return specifier
	}
	// Regular package: package/path -> package
	parts := strings.SplitN(specifier, "/", 2)
	return parts[0]
}
