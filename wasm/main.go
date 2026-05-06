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
	"syscall/js"

	"bennypowers.dev/mappa/cdn"
	mappfs "bennypowers.dev/mappa/fs"
	"bennypowers.dev/mappa/importmap"
	"bennypowers.dev/mappa/packagejson"
	cdnresolver "bennypowers.dev/mappa/resolve/cdn"
	"bennypowers.dev/mappa/resolve/local"
)

// Version is the mappa WASM version.
const Version = "0.1.0"

func main() {
	mappa := make(map[string]any)
	mappa["generate"] = js.FuncOf(generate)
	mappa["resolve"] = js.FuncOf(resolveLocal)
	mappa["version"] = Version

	js.Global().Set("mappa", js.ValueOf(mappa))

	select {}
}

// generate creates an import map from package.json contents using a CDN resolver.
//
// Arguments:
//   - packageJsonStr: string - The package.json contents as a JSON string
//   - options: object (optional)
//     - cdn: string - CDN provider name ("esm.sh", "unpkg", "jsdelivr")
//     - template: string - Custom CDN template
//     - conditions: string[] - Export conditions
func generate(this js.Value, args []js.Value) any {
	handler := js.FuncOf(func(this js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			result, err := doGenerate(args)
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

func doGenerate(args []js.Value) (string, error) {
	if len(args) < 1 {
		return "", &jsError{message: "generate requires at least one argument (package.json string)"}
	}

	pkgJSONStr := args[0].String()
	pkg, err := packagejson.Parse([]byte(pkgJSONStr))
	if err != nil {
		return "", &jsError{message: "failed to parse package.json: " + err.Error()}
	}

	opts := parseGenerateOptions(args)

	fetcher := cdn.NewHTTPFetcher()
	resolver := cdnresolver.New(fetcher)

	if opts.cdn != "" {
		provider := cdn.ProviderByName(opts.cdn)
		if provider != nil {
			resolver = resolver.WithProvider(*provider)
		}
	}
	if opts.template != "" {
		var err error
		resolver, err = resolver.WithTemplate(opts.template)
		if err != nil {
			return "", &jsError{message: "invalid template: " + err.Error()}
		}
	}
	if len(opts.conditions) > 0 {
		resolver = resolver.WithConditions(opts.conditions)
	}
	if len(opts.exclude) > 0 {
		resolver = resolver.WithExclude(opts.exclude)
	}

	ctx := context.Background()
	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		return "", &jsError{message: "failed to generate import map: " + err.Error()}
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
//     - template: string - URL template (default: /node_modules/{package}/{path})
//     - conditions: string[] - Export condition priority
//     - includePackages: string[] - Additional packages beyond dependencies
//     - exclude: string[] - Packages to exclude
//     - inputMap: string - Import map JSON to merge (input map takes precedence)
//     - optimize: number - 0=none, 1=simplify+dedup (default: 1)
func resolveLocal(this js.Value, args []js.Value) any {
	handler := js.FuncOf(func(this js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
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
	resolver, err := opts.localOptions().Apply(local.New(osfs, nil))
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

type generateOptions struct {
	cdn        string
	template   string
	conditions []string
	exclude    []string
}

func parseGenerateOptions(args []js.Value) generateOptions {
	opts := generateOptions{}
	if len(args) < 2 || args[1].IsUndefined() || args[1].IsNull() {
		return opts
	}

	obj := args[1]

	if v := obj.Get("cdn"); !v.IsUndefined() && !v.IsNull() {
		opts.cdn = v.String()
	}
	if v := obj.Get("template"); !v.IsUndefined() && !v.IsNull() {
		opts.template = v.String()
	}
	if v := obj.Get("conditions"); !v.IsUndefined() && !v.IsNull() {
		opts.conditions = jsStringArray(v)
	}
	if v := obj.Get("exclude"); !v.IsUndefined() && !v.IsNull() {
		opts.exclude = jsStringArray(v)
	}

	return opts
}

type resolveOptions struct {
	template        string
	conditions      []string
	includePackages []string
	exclude         []string
	inputMap        *importmap.ImportMap
	optimize        int
	pathBase         string
	packageDeps     string
}

func (o *resolveOptions) localOptions() *local.Options {
	return &local.Options{
		Template:        o.template,
		Conditions:      o.conditions,
		IncludePackages: o.includePackages,
		Exclude:         o.exclude,
		InputMap:        o.inputMap,
		PathBase:         o.pathBase,
		PackageDeps:     o.packageDeps,
	}
}

func parseResolveOptions(args []js.Value) (resolveOptions, error) {
	opts := resolveOptions{optimize: 1}
	if len(args) < 2 || args[1].IsUndefined() || args[1].IsNull() {
		return opts, nil
	}

	obj := args[1]

	if v := obj.Get("template"); !v.IsUndefined() && !v.IsNull() {
		opts.template = v.String()
	}
	if v := obj.Get("conditions"); !v.IsUndefined() && !v.IsNull() {
		opts.conditions = jsStringArray(v)
	}
	if v := obj.Get("includePackages"); !v.IsUndefined() && !v.IsNull() {
		opts.includePackages = jsStringArray(v)
	}
	if v := obj.Get("exclude"); !v.IsUndefined() && !v.IsNull() {
		opts.exclude = jsStringArray(v)
	}
	if v := obj.Get("inputMap"); !v.IsUndefined() && !v.IsNull() {
		inputMapStr := js.Global().Get("JSON").Call("stringify", v).String()
		im, err := importmap.Parse([]byte(inputMapStr))
		if err != nil {
			return opts, &jsError{message: "invalid inputMap: " + err.Error()}
		}
		opts.inputMap = im
	}
	if v := obj.Get("optimize"); !v.IsUndefined() && !v.IsNull() {
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

func jsStringArray(v js.Value) []string {
	length := v.Length()
	result := make([]string, length)
	for i := range length {
		result[i] = v.Index(i).String()
	}
	return result
}

type jsError struct {
	message string
}

func (e *jsError) Error() string {
	return e.message
}
