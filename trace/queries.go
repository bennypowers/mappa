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
	tsHtml "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tsTypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

//go:embed queries/*/*.scm
var queryFiles embed.FS

// Languages holds pre-initialized tree-sitter language grammars.
var languages = struct {
	html       *ts.Language
	typescript *ts.Language
}{
	ts.NewLanguage(tsHtml.Language()),
	ts.NewLanguage(tsTypescript.LanguageTypescript()),
}

// Parser pools for reuse.
var (
	htmlParserPool = sync.Pool{
		New: func() any {
			parser := ts.NewParser()
			if err := parser.SetLanguage(languages.html); err != nil {
				panic("failed to set HTML language: " + err.Error())
			}
			return parser
		},
	}

	tsParserPool = sync.Pool{
		New: func() any {
			parser := ts.NewParser()
			if err := parser.SetLanguage(languages.typescript); err != nil {
				panic("failed to set TypeScript language: " + err.Error())
			}
			return parser
		},
	}
)

// getHTMLParser retrieves an HTML parser from the pool.
func getHTMLParser() *ts.Parser {
	return htmlParserPool.Get().(*ts.Parser)
}

// putHTMLParser returns an HTML parser to the pool.
func putHTMLParser(p *ts.Parser) {
	p.Reset()
	htmlParserPool.Put(p)
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

// QueryManager manages tree-sitter queries for HTML and TypeScript parsing.
type QueryManager struct {
	mu         sync.Mutex
	closed     bool
	html       map[string]*ts.Query
	typescript map[string]*ts.Query
}

// NewQueryManager creates a new QueryManager with the specified queries loaded.
func NewQueryManager(htmlQueries, tsQueries []string) (*QueryManager, error) {
	qm := &QueryManager{
		html:       make(map[string]*ts.Query),
		typescript: make(map[string]*ts.Query),
	}

	for _, name := range htmlQueries {
		if err := qm.loadQuery("html", name); err != nil {
			qm.Close()
			return nil, err
		}
	}

	for _, name := range tsQueries {
		if err := qm.loadQuery("typescript", name); err != nil {
			qm.Close()
			return nil, err
		}
	}

	return qm, nil
}

func (qm *QueryManager) loadQuery(language, name string) error {
	queryPath := path.Join("queries", language, name+".scm")
	data, err := queryFiles.ReadFile(queryPath)
	if err != nil {
		return fmt.Errorf("failed to read query %s: %w", queryPath, err)
	}

	var lang *ts.Language
	switch language {
	case "html":
		lang = languages.html
	case "typescript":
		lang = languages.typescript
	default:
		return fmt.Errorf("unknown language: %s", language)
	}

	query, qerr := ts.NewQuery(lang, string(data))
	if qerr != nil {
		return fmt.Errorf("failed to parse query %s: %w", name, qerr)
	}

	switch language {
	case "html":
		qm.html[name] = query
	case "typescript":
		qm.typescript[name] = query
	}

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
	htmlQueries := qm.html
	tsQueries := qm.typescript
	qm.html = nil
	qm.typescript = nil
	qm.mu.Unlock()

	for _, q := range htmlQueries {
		q.Close()
	}
	for _, q := range tsQueries {
		q.Close()
	}
}

// Query returns a query by language and name.
func (qm *QueryManager) Query(language, name string) (*ts.Query, error) {
	var q *ts.Query
	var ok bool
	switch language {
	case "html":
		q, ok = qm.html[name]
	case "typescript":
		q, ok = qm.typescript[name]
	}
	if !ok {
		return nil, fmt.Errorf("query not found: %s/%s", language, name)
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
		globalQM, globalQMErr = NewQueryManager(
			[]string{"scriptTags"},
			[]string{"imports"},
		)
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
}
