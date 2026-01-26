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
	"bennypowers.dev/mappa/packagejson"
	cdnresolver "bennypowers.dev/mappa/resolve/cdn"
)

// Version is the mappa WASM version.
const Version = "0.1.0"

func main() {
	// Create the mappa namespace object
	mappa := make(map[string]any)
	mappa["generate"] = js.FuncOf(generate)
	mappa["version"] = Version

	// Export to global scope
	js.Global().Set("mappa", js.ValueOf(mappa))

	// Keep the program running
	select {}
}

// generate is the main entry point for generating import maps.
// Arguments:
//   - packageJsonStr: string - The package.json contents as a JSON string
//   - options: object (optional) - Generation options
//     - cdn: string - CDN provider name ("esm.sh", "unpkg", "jsdelivr")
//     - template: string - Custom CDN template
//     - conditions: string[] - Export conditions
//     - includeDev: boolean - Include devDependencies
//
// Returns a Promise that resolves to the import map JSON string.
func generate(this js.Value, args []js.Value) any {
	// Create a new Promise
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

// doGenerate performs the actual import map generation.
func doGenerate(args []js.Value) (string, error) {
	if len(args) < 1 {
		return "", &jsError{message: "generate requires at least one argument (package.json string)"}
	}

	// Parse package.json
	pkgJSONStr := args[0].String()
	pkg, err := packagejson.Parse([]byte(pkgJSONStr))
	if err != nil {
		return "", &jsError{message: "failed to parse package.json: " + err.Error()}
	}

	// Parse options
	opts := parseOptions(args)

	// Create fetcher and resolver
	fetcher := cdn.NewHTTPFetcher()
	resolver := cdnresolver.New(fetcher)

	// Apply options
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
	if opts.includeDev {
		resolver = resolver.WithIncludeDev(true)
	}

	// Generate import map
	ctx := context.Background()
	im, err := resolver.ResolvePackageJSON(ctx, pkg)
	if err != nil {
		return "", &jsError{message: "failed to generate import map: " + err.Error()}
	}

	// Convert to JSON
	jsonBytes, err := json.MarshalIndent(im, "", "  ")
	if err != nil {
		return "", &jsError{message: "failed to serialize import map: " + err.Error()}
	}

	return string(jsonBytes), nil
}

// generateOptions holds parsed generation options.
type generateOptions struct {
	cdn        string
	template   string
	conditions []string
	includeDev bool
}

// parseOptions extracts options from the JavaScript arguments.
func parseOptions(args []js.Value) generateOptions {
	opts := generateOptions{}

	if len(args) < 2 || args[1].IsUndefined() || args[1].IsNull() {
		return opts
	}

	optionsObj := args[1]

	// CDN provider
	if cdnVal := optionsObj.Get("cdn"); !cdnVal.IsUndefined() && !cdnVal.IsNull() {
		opts.cdn = cdnVal.String()
	}

	// Custom template
	if templateVal := optionsObj.Get("template"); !templateVal.IsUndefined() && !templateVal.IsNull() {
		opts.template = templateVal.String()
	}

	// Export conditions
	if conditionsVal := optionsObj.Get("conditions"); !conditionsVal.IsUndefined() && !conditionsVal.IsNull() {
		length := conditionsVal.Length()
		opts.conditions = make([]string, length)
		for i := range length {
			opts.conditions[i] = conditionsVal.Index(i).String()
		}
	}

	// Include devDependencies
	if includeDevVal := optionsObj.Get("includeDev"); !includeDevVal.IsUndefined() && !includeDevVal.IsNull() {
		opts.includeDev = includeDevVal.Bool()
	}

	return opts
}

// jsError represents an error to be returned to JavaScript.
type jsError struct {
	message string
}

func (e *jsError) Error() string {
	return e.message
}
