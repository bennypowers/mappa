//go:build js && wasm

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

// Package main provides the WASM entry point for mappa.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall/js"

	"bennypowers.dev/mappa/cdn"
	mappfs "bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/internal/version"
	"bennypowers.dev/mappa/packagejson"
	cdnresolver "bennypowers.dev/mappa/resolve/cdn"
	"bennypowers.dev/mappa/resolve/local"
	"bennypowers.dev/mappa/wasm/options"
)

func main() {
	mappa := make(map[string]any)
	mappa["generate"] = js.FuncOf(generate)
	mappa["resolve"] = js.FuncOf(resolveLocal)
	mappa["version"] = version.VersionString()

	js.Global().Set("mappa", js.ValueOf(mappa))

	select {}
}

// stderrLogger writes resolution warnings to stderr (console in browsers).
type stderrLogger struct{}

func (stderrLogger) Warning(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "Warning: "+format+"\n", args...)
}

func (stderrLogger) Debug(format string, args ...any) {}

// generate creates an import map from package.json contents using a CDN resolver.
//
// Arguments:
//   - packageJsonStr: string - The package.json contents as a JSON string
//   - options: object (optional)
//   - cdn: string - CDN provider name ("esm.sh", "unpkg", "jsdelivr")
//   - template: string - Custom CDN template
//   - conditions: string[] - Export conditions
//   - exclude: string[] - Packages to exclude (including transitively)
//   - timeoutMs: number - Per-request timeout in milliseconds; 0 disables
//   - allowPartial: boolean - Return a partial map with warnings instead of failing
func generate(this js.Value, args []js.Value) any {
	handler := js.FuncOf(func(this js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			defer func() {
				if r := recover(); r != nil {
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("mappa generate panicked: %v", r)))
				}
			}()
			result, err := doGenerate(args)
			if err != nil {
				reject.Invoke(newJSError(err))
				return
			}
			resolve.Invoke(result)
		}()

		return nil
	})

	promise := js.Global().Get("Promise").New(handler)
	handler.Release()
	return promise
}

func doGenerate(args []js.Value) (string, error) {
	if len(args) < 1 {
		return "", &jsError{message: "generate requires at least one argument (package.json string)"}
	}

	pkgJSONStr := args[0].String()
	pkg, err := packagejson.Parse([]byte(pkgJSONStr))
	if err != nil {
		return "", &jsError{message: "failed to parse package.json: " + err.Error()}
	}

	opts, err := parseGenerateOptions(args)
	if err != nil {
		return "", err
	}

	fetcher := cdn.NewHTTPFetcher()
	resolver, err := opts.Apply(cdnresolver.New(fetcher).WithLogger(stderrLogger{}))
	if err != nil {
		return "", &jsError{message: err.Error()}
	}

	ctx := context.Background()
	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		return "", &jsError{message: "failed to generate import map: " + err.Error(), err: err}
	}

	jsonBytes, err := json.MarshalIndent(im, "", "  ")
	if err != nil {
		return "", &jsError{message: "failed to serialize import map: " + err.Error()}
	}

	return string(jsonBytes), nil
}

// resolveLocal creates an import map from a local directory's node_modules.
//
// Arguments:
//   - rootDir: string - Path to directory containing package.json and node_modules
//   - options: object (optional)
//   - template: string - URL template (default: /node_modules/{package}/{path})
//   - conditions: string[] - Export condition priority
//   - includePackages: string[] - Additional packages beyond dependencies
//   - exclude: string[] - Packages to exclude
//   - inputMap: string - Import map JSON to merge (input map takes precedence)
//   - optimize: number - 0=none, 1=simplify+dedup (default: 1)
func resolveLocal(this js.Value, args []js.Value) any {
	handler := js.FuncOf(func(this js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			defer func() {
				if r := recover(); r != nil {
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("mappa resolve panicked: %v", r)))
				}
			}()
			result, err := doResolve(args)
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
				return
			}
			resolve.Invoke(result)
		}()

		return nil
	})

	promise := js.Global().Get("Promise").New(handler)
	handler.Release()
	return promise
}

func doResolve(args []js.Value) (string, error) {
	if len(args) < 1 {
		return "", &jsError{message: "resolve requires at least one argument (rootDir)"}
	}

	rootDir := args[0].String()
	opts, err := parseResolveOptions(args)
	if err != nil {
		return "", err
	}

	osfs := mappfs.NewOSFileSystem()
	resolver, err := opts.localOptions().Apply(local.New(osfs, stderrLogger{}))
	if err != nil {
		return "", &jsError{message: err.Error()}
	}

	im, err := resolver.Resolve(rootDir)
	if err != nil {
		return "", &jsError{message: "failed to resolve: " + err.Error()}
	}

	if opts.optimize >= 1 {
		im = im.Simplify()
	}

	jsonBytes, err := json.MarshalIndent(im, "", "  ")
	if err != nil {
		return "", &jsError{message: "failed to serialize import map: " + err.Error()}
	}

	return string(jsonBytes), nil
}

func parseGenerateOptions(args []js.Value) (options.Generate, error) {
	opts := options.Generate{}
	if len(args) < 2 || args[1].IsUndefined() || args[1].IsNull() {
		return opts, nil
	}

	obj := args[1]
	if obj.Type() != js.TypeObject {
		return opts, &jsError{message: "options must be an object"}
	}

	if v := obj.Get("cdn"); !v.IsUndefined() && !v.IsNull() {
		opts.CDN = v.String()
	}
	if v := obj.Get("template"); !v.IsUndefined() && !v.IsNull() {
		opts.Template = v.String()
	}
	if v := obj.Get("conditions"); !v.IsUndefined() && !v.IsNull() {
		var err error
		opts.Conditions, err = jsStringArray("conditions", v)
		if err != nil {
			return opts, err
		}
	}
	if v := obj.Get("exclude"); !v.IsUndefined() && !v.IsNull() {
		var err error
		opts.Exclude, err = jsStringArray("exclude", v)
		if err != nil {
			return opts, err
		}
	}
	if v := obj.Get("timeoutMs"); !v.IsUndefined() && !v.IsNull() {
		if v.Type() != js.TypeNumber {
			return opts, &jsError{message: "timeoutMs must be a number"}
		}
		ms := v.Int()
		if ms < 0 {
			return opts, &jsError{message: "timeoutMs must not be negative"}
		}
		opts.TimeoutMs = &ms
	}
	if v := obj.Get("allowPartial"); !v.IsUndefined() && !v.IsNull() {
		if v.Type() != js.TypeBoolean {
			return opts, &jsError{message: "allowPartial must be a boolean"}
		}
		opts.AllowPartial = v.Bool()
	}

	return opts, nil
}

// newJSError builds a JS Error with a stable code for classified failures.
func newJSError(err error) js.Value {
	jsErr := js.Global().Get("Error").New(err.Error())
	switch {
	case cdn.IsTooLarge(err):
		jsErr.Set("code", "ERR_RESPONSE_TOO_LARGE")
	case cdn.IsTimeout(err):
		jsErr.Set("code", "ERR_TIMEOUT")
	case cdn.IsNotFound(err):
		jsErr.Set("code", "ERR_NOT_FOUND")
	case errors.Is(err, context.DeadlineExceeded):
		jsErr.Set("code", "ERR_TIMEOUT")
	}
	return jsErr
}

type resolveOptions struct {
	template        string
	conditions      []string
	includePackages []string
	exclude         []string
	inputMap        *importmap.ImportMap
	optimize        int
	pathBase        string
	packageDeps     string
}

func (o *resolveOptions) localOptions() *local.Options {
	return &local.Options{
		Template:        o.template,
		Conditions:      o.conditions,
		IncludePackages: o.includePackages,
		Exclude:         o.exclude,
		InputMap:        o.inputMap,
		PathBase:        o.pathBase,
		PackageDeps:     o.packageDeps,
	}
}

func parseResolveOptions(args []js.Value) (resolveOptions, error) {
	opts := resolveOptions{optimize: 1}
	if len(args) < 2 || args[1].IsUndefined() || args[1].IsNull() {
		return opts, nil
	}

	obj := args[1]
	if obj.Type() != js.TypeObject {
		return opts, &jsError{message: "options must be an object"}
	}

	if v := obj.Get("template"); !v.IsUndefined() && !v.IsNull() {
		opts.template = v.String()
	}
	if v := obj.Get("conditions"); !v.IsUndefined() && !v.IsNull() {
		var err error
		opts.conditions, err = jsStringArray("conditions", v)
		if err != nil {
			return opts, err
		}
	}
	if v := obj.Get("includePackages"); !v.IsUndefined() && !v.IsNull() {
		var err error
		opts.includePackages, err = jsStringArray("includePackages", v)
		if err != nil {
			return opts, err
		}
	}
	if v := obj.Get("exclude"); !v.IsUndefined() && !v.IsNull() {
		var err error
		opts.exclude, err = jsStringArray("exclude", v)
		if err != nil {
			return opts, err
		}
	}
	if v := obj.Get("inputMap"); !v.IsUndefined() && !v.IsNull() {
		if v.Type() != js.TypeObject {
			return opts, &jsError{message: "inputMap must be an object"}
		}
		inputMapStr := js.Global().Get("JSON").Call("stringify", v).String()
		im, err := importmap.Parse([]byte(inputMapStr))
		if err != nil {
			return opts, &jsError{message: "invalid inputMap: " + err.Error()}
		}
		opts.inputMap = im
	}
	if v := obj.Get("optimize"); !v.IsUndefined() && !v.IsNull() {
		if v.Type() != js.TypeNumber {
			return opts, &jsError{message: "optimize must be a number"}
		}
		opts.optimize = v.Int()
	}
	if v := obj.Get("pathBase"); !v.IsUndefined() && !v.IsNull() {
		opts.pathBase = v.String()
	}
	if v := obj.Get("packageDeps"); !v.IsUndefined() && !v.IsNull() {
		opts.packageDeps = v.String()
	}

	return opts, nil
}

func jsStringArray(name string, v js.Value) ([]string, error) {
	// TypeObject alone also matches plain objects and Sets, which have no
	// length or indexed elements and would silently become empty arrays.
	if !js.Global().Get("Array").Call("isArray", v).Bool() {
		return nil, &jsError{message: name + " must be an array of strings"}
	}
	length := v.Length()
	result := make([]string, length)
	for i := range length {
		item := v.Index(i)
		if item.Type() != js.TypeString {
			return nil, &jsError{message: name + " must contain only strings"}
		}
		result[i] = item.String()
	}
	return result, nil
}

type jsError struct {
	message string
	err     error
}

func (e *jsError) Error() string {
	return e.message
}

// Unwrap exposes the classified cause so the JS error code can be set.
func (e *jsError) Unwrap() error {
	return e.err
}
