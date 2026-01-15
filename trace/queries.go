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
	"embed"
	"fmt"
	"path"
	"sync"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsTypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

//go:embed queries/*/*.scm
var queryFiles embed.FS

// typescript holds the pre-initialized TypeScript language grammar.
var typescript = ts.NewLanguage(tsTypescript.LanguageTypescript())

// tsParserPool provides reusable TypeScript parsers.
var tsParserPool = sync.Pool{
	New: func() any {
		parser := ts.NewParser()
		if err := parser.SetLanguage(typescript); err != nil {
			panic("failed to set TypeScript language: " + err.Error())
		}
		return parser
	},
}

// getTSParser retrieves a TypeScript parser from the pool.
func getTSParser() *ts.Parser {
	return tsParserPool.Get().(*ts.Parser)
}

// putTSParser returns a TypeScript parser to the pool.
func putTSParser(p *ts.Parser) {
	p.Reset()
	tsParserPool.Put(p)
}

// QueryManager manages tree-sitter queries for TypeScript/JavaScript parsing.
type QueryManager struct {
	mu      sync.Mutex
	closed  bool
	queries map[string]*ts.Query
}

// NewQueryManager creates a new QueryManager with the specified queries loaded.
func NewQueryManager(queryNames []string) (*QueryManager, error) {
	qm := &QueryManager{
		queries: make(map[string]*ts.Query),
	}

	for _, name := range queryNames {
		if err := qm.loadQuery(name); err != nil {
			qm.Close()
			return nil, err
		}
	}

	return qm, nil
}

func (qm *QueryManager) loadQuery(name string) error {
	queryPath := path.Join("queries", "typescript", name+".scm")
	data, err := queryFiles.ReadFile(queryPath)
	if err != nil {
		return fmt.Errorf("failed to read query %s: %w", queryPath, err)
	}

	query, qerr := ts.NewQuery(typescript, string(data))
	if qerr != nil {
		return fmt.Errorf("failed to parse query %s: %w", name, qerr)
	}

	qm.queries[name] = query
	return nil
}

// Close releases all query resources. Safe to call multiple times.
func (qm *QueryManager) Close() {
	qm.mu.Lock()
	if qm.closed {
		qm.mu.Unlock()
		return
	}
	qm.closed = true
	queries := qm.queries
	qm.queries = nil
	qm.mu.Unlock()

	for _, q := range queries {
		q.Close()
	}
}

// Query returns a query by name.
func (qm *QueryManager) Query(name string) (*ts.Query, error) {
	q, ok := qm.queries[name]
	if !ok {
		return nil, fmt.Errorf("query not found: %s", name)
	}
	return q, nil
}

// Global query manager singleton
var (
	globalQM     *QueryManager
	globalQMOnce sync.Once
	globalQMErr  error
)

// GetQueryManager returns the global query manager instance.
func GetQueryManager() (*QueryManager, error) {
	globalQMOnce.Do(func() {
		globalQM, globalQMErr = NewQueryManager([]string{"imports"})
	})
	return globalQM, globalQMErr
}

// ScriptTag represents a <script> tag found in HTML.
type ScriptTag struct {
	Type    string   // The type attribute (e.g., "module")
	Src     string   // The src attribute (external script)
	Inline  bool     // True if script has inline content
	Content string   // The inline script content
	Imports []string // Import specifiers found in inline content
}

// ModuleImport represents an import statement in a module.
type ModuleImport struct {
	Specifier string // The import specifier (e.g., "lit", "./foo.js")
	IsDynamic bool   // True if this is a dynamic import()
	Line      int    // 1-indexed line number of the specifier
}
