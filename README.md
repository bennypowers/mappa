# Mappa

The high-performance import map generator for the modern web, available as a CLI tool and Go library.

**[Try the web demo](https://bennypowers.github.io/mappa/)** - Generate import maps directly in your browser.

![mappa - import map generator](./docs/images/mappa.png)

> *Mappa* (מַפָּה) is Hebrew for "map".

Modern web applications use ES modules with bare specifiers like `import { html } from 'lit'`. Browsers need [import maps][importmaps] to resolve these specifiers to actual URLs.

Mappa generates import maps from your `package.json` dependencies, pointing to your local `node_modules` paths or to a path of your choosing (like `/assets/packages/...`. It's designed to be fast, with parallel dependency resolution.

## Features

- **Local resolution**: Generate import maps pointing to your install node modules paths
- **Custom URL templates**: Use any asset path with `{package}` and `{path}` variables
- **Export maps**: Full support for package.json `exports` field including subpaths and wildcards
- **Scopes**: Automatic scope generation for transitive dependencies
- **Merge input maps**: Combine generated maps with manual overrides
- **Parallel resolution**: Fast transitive dependency resolution

## Installation

### npm

```bash
npm install @pwrs/mappa
```

Provides both a CLI (`npx mappa generate`) and a JavaScript API:

```typescript
import { resolve } from '@pwrs/mappa';

const importMap = await resolve('.', {
  template: '/assets/packages/{package}/{path}',
});
```

### Gentoo Linux

Enable the `bennypowers` overlay, then install:

```bash
eselect repository enable bennypowers
emaint sync -r bennypowers
emerge dev-util/mappa
```

### From Source

```bash
go install bennypowers.dev/mappa@latest
```

## Quick Start

Generate an import map for your project:

```bash
# Local paths (default)
mappa generate

# Output as HTML script tag
mappa generate --format html

# Custom asset path
mappa generate --template "/assets/packages/{package}/{path}"
```

## CLI Reference

### `mappa generate`

Generate an import map from `package.json` dependencies.

```
Flags:
  -f, --format string        Output format: json, html (default "json")
      --include-package      Additional packages to include (repeatable)
      --input-map string     Import map file to merge with generated output
      --template string      URL template (default: /node_modules/{package}/{path})
  -p, --package string       Package directory (default ".")
  -o, --output string        Output file (default: stdout)
```

**Examples:**

```bash
# Include devDependencies
mappa generate --include-package fuse.js --include-package vitest

# Merge with manual overrides
mappa generate --input-map manual-imports.json

# Custom asset path
mappa generate --template "/assets/packages/{package}/{path}"
```

### `mappa trace`

Trace HTML files to discover ES module imports and generate minimal import maps containing only the specifiers actually used.

```
Flags:
  -f, --format string        Output format: json, html, specifiers (default "json")
      --template string      URL template (default: /node_modules/{package}/{path})
      --conditions string    Export condition priority (e.g., production,browser,import,default)
      --glob string          Glob pattern to match HTML files (e.g., "_site/**/*.html")
  -j, --jobs int             Number of parallel workers (default: number of CPUs)
  -p, --package string       Package directory (default ".")
  -o, --output string        Output file (default: stdout)
```

**Examples:**

```bash
# Trace a single HTML file
mappa trace index.html

# Trace with custom template
mappa trace index.html --template "/assets/packages/{package}/{path}"

# Batch mode with glob pattern (outputs NDJSON)
mappa trace --glob "_site/**/*.html" -j 8

# Output raw traced specifiers for debugging
mappa trace index.html --format specifiers
```

**How it works:**

1. Parses HTML to find `<script type="module">` tags
2. Uses tree-sitter to extract all `import` statements from JS modules
3. Follows transitive dependencies through local and node_modules files
4. Generates an import map with only the bare specifiers actually imported

**Static Analysis Limitations:**

The trace command uses static analysis to find imports. This means:

- **Dynamic imports with variable specifiers cannot be detected.** For example:
  ```javascript
  // This WILL be traced
  import '@rhds/icons/standard/web-icon.js';

  // This will NOT be traced (specifier is computed at runtime)
  const iconName = 'web-icon';
  import(`@rhds/icons/standard/${iconName}.js`);
  ```

- To handle dynamic imports, mappa includes **trailing-slash import map keys** for all direct dependencies that have wildcard exports. For example, if your package.json lists `@rhds/icons` as a dependency and that package exports `./*`, the import map will include `"@rhds/icons/": "/assets/packages/@rhds/icons/"` to cover any dynamic imports.

- The same applies to scopes: transitive dependencies with wildcard exports get trailing-slash keys so their dynamic imports work correctly.

## URL Templates

Templates use `{variable}` syntax for dynamic URL generation:

| Variable    | Description                | Example                    |
| ----------- | -------------------------- | -------------------------- |
| `{package}` | Full package name          | `@scope/name` or `name` |
| `{name}`    | Package name without scope | `name`                     |
| `{scope}`   | Scope without @ prefix     | `scope`                    |
| `{path}`    | File path within package   | `index.js`                 |

**Examples:**

```bash
# Default (node_modules)
--template "/node_modules/{package}/{path}"

# Custom assets directory
--template "/assets/packages/{package}/{path}"

# Scoped package handling
--template "/libs/{scope}/{name}/{path}"
```

## Performance

Mappa is written in Go for speed. Benchmarked against [@jspm/generator][jspm] on a real-world project ([Red Hat Design System][rhds]):

| Tool            | Time          |
| --------------- | ------------- |
| mappa           | 3.2ms ± 0.2ms |
| @jspm/generator | 230ms ± 6ms   |

**mappa is ~72x faster** for local import map generation.

[jspm]: https://jspm.org/
[rhds]: https://github.com/RedHat-UX/red-hat-design-system

## JavaScript API

The npm package includes a WASM-powered JavaScript API alongside the CLI.

### `resolve(rootDir, options?)`

Generate an import map from a local directory's `node_modules`:

```typescript
import { resolve } from '@pwrs/mappa';

const importMap = await resolve('.', {
  template: '/assets/packages/{package}/{path}',
  conditions: ['browser', 'import', 'default'],
  exclude: ['lodash'],
});
```

**Options:**

| Option | Type | Description |
|--------|------|-------------|
| `template` | `string` | URL template (default: `/node_modules/{package}/{path}`) |
| `conditions` | `string[]` | Export condition priority |
| `includePackages` | `string[]` | Additional packages beyond dependencies |
| `exclude` | `string[]` | Packages to exclude |
| `inputMap` | `ImportMap` | Import map to merge (takes precedence) |
| `optimize` | `0 \| 1` | 0=none, 1=simplify+dedup (default: 1) |

### `generate(packageJson, options?)`

Generate an import map from package.json contents using a CDN:

```typescript
import { generate } from '@pwrs/mappa';

const importMap = await generate(
  { dependencies: { lit: '^3.0.0' } },
  { cdn: 'esm.sh' }
);
```

**Options:**

| Option | Type | Description |
|--------|------|-------------|
| `cdn` | `"esm.sh" \| "unpkg" \| "jsdelivr"` | CDN provider |
| `template` | `string` | Custom CDN URL template |
| `conditions` | `string[]` | Export conditions |

### `version()`

Returns the WASM engine version string.

## Integration Examples

### 11ty / Eleventy

```typescript
import { resolve } from '@pwrs/mappa';

export default async function(eleventyConfig) {
  const importMap = await resolve('.', {
    template: '/assets/packages/{package}/{path}',
  });

  for (const [, path] of Object.entries(importMap.imports)) {
    const match = path.match(/^\/assets\/packages\/(@[^/]+\/[^/]+|[^/]+)/);
    if (match) {
      eleventyConfig.addPassthroughCopy({
        [`node_modules/${match[1]}`]: `/assets/packages/${match[1]}`,
      });
    }
  }

  eleventyConfig.addTransform('importmap', (content, outputPath) => {
    if (!outputPath?.endsWith('.html')) return content;

    const script = `<script type="importmap">\n${JSON.stringify(importMap, null, 2)}\n</script>`;
    return content.replace('</head>', `${script}\n</head>`);
  });
}
```

## License

GPLv3

[importmaps]: https://developer.mozilla.org/en-US/docs/Web/HTML/Element/script/type/importmap
