## Go

Getter methods should be named `Foo()`, not `GetFoo()`.

Run `go vet` to surface gopls suggestions. Common examples:
- replace `interface{}` with `any`
- replace `if/else` with `min`
- replace `m[k]=v` loop with `maps.Copy` [mapsloop]
- Loop can be simplified using slices.Contains [slicescontains]
    ```go
		found := false
		for _, p := range providers {
			if p == name {
				found = true
				break
			}
		}
    ```
    Should be written
    ```go
		found := slices.Contains(providers, name)
    ```

## Testing

Practice TDD. When writing tests, always use the fixture/golden patterns:

- **Fixtures**: Input test data in `testdata/` directories
- **Goldens**: Expected output files to compare against (e.g., `expected.json`)
- Tests should support `--update` flag to regenerate golden files when intentional changes occur

### Fixture Structure

Each test scenario is a subdirectory containing:
- `input.json`, `index.html`, `module.js`, or `package.json` (required)
- `expected.json` (required for assertions)

### Using NewFixtureFS

**Always use `testutil.NewFixtureFS` for tests**, never use `NewOSFileSystem()` in tests:

```go
func TestSomething(t *testing.T) {
    // Load fixtures into MapFileSystem
    mfs := testutil.NewFixtureFS(t, "packagejson/simple-exports", "/test")

    // Read fixture files from the virtual filesystem
    input, err := mfs.ReadFile("/test/input.json")
    expected, err := mfs.ReadFile("/test/expected.json")

    // Pass the MapFileSystem to functions under test
    result, err := someFunction(mfs, "/test/input.json")

    // Compare with expected
    // ...
}
```

### Why MapFileSystem?

1. **Isolation**: Tests don't depend on working directory or real filesystem state
2. **Speed**: In-memory filesystem is faster than disk I/O
3. **Reproducibility**: Same test runs identically on any machine
4. **Parallelism**: Tests can run concurrently without filesystem conflicts
5. **Integration**: Compatible with cem's testing infrastructure

### MapFileSystem for Unit Tests

For tests that don't need fixtures, create an empty MapFileSystem:

```go
func TestNoFiles(t *testing.T) {
    mfs := importmaps.NewMapFileSystem()
    mfs.AddDir("/empty", 0755)

    resolver := local.New(mfs, nil)
    result, err := resolver.Resolve("/empty")
    // ...
}
```

### Avoiding Inline Test Data

**Don't inline source code in tests:**

```go
// Bad - inline source
js := []byte(`import { foo } from 'bar';`)
imports, _ := ExtractImports(js)

// Good - use fixture file
mfs := testutil.NewFixtureFS(t, "trace/extract-imports", "/test")
js, _ := mfs.ReadFile("/test/module.js")
imports, _ := ExtractImports(js)
```

Fixtures should contain:
- Input files (`.js`, `.html`, `.json`, etc.)
- Expected output (`expected.json`)

## Git

When commit messages mention AI agents, always use `Assisted-By`, never use `Co-Authored-By`.

## FileSystem Interface

This package defines a `FileSystem` interface that is congruent with `bennypowers.dev/cem/internal/platform.FileSystem`. This enables duck typing compatibility - cem can pass its filesystem implementations to this library without import dependencies.

**Always use the pluggable FileSystem:**
- Never use `os.ReadFile`, `os.Stat`, `os.ReadDir`, etc. directly
- All functions that read from disk must accept a `FileSystem` parameter
- This enables testability with mock filesystems and integration with cem
- Use `NewOSFileSystem()` only at the top level (CLI entry point)
- Use `NewMapFileSystem()` and `NewFixtureFS()` in tests

Example:
```go
// Good - accepts FileSystem
func ParsePackageJSON(fs FileSystem, path string) (*PackageJSON, error)

// Bad - uses os directly
func ParsePackageJSON(path string) (*PackageJSON, error) {
    data, _ := os.ReadFile(path)  // Don't do this
}
```

When adding new filesystem operations, ensure the interface stays compatible with cem's version.

## Shared logic

similar packages like generate and trace often share concerns (tracing module graphs, caching package.json files, etc). Be sure to share logic where possible, instead of duplicating.
