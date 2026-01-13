# Mappa

The high-performance import map generator for the modern web, available as a CLI tool and Go library.

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

Trace an HTML file to discover module imports.

```bash
mappa trace index.html
```

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

## Integration Examples

### 11ty / Eleventy

```typescript
import { execSync } from 'node:child_process';

export default function(eleventyConfig) {
  const result = execSync('mappa generate --template "/assets/packages/{package}/{path}"', {
    encoding: 'utf-8',
  });

  const importMap = JSON.parse(result);

  // Set up passthrough copies for each package
  for (const [, path] of Object.entries(importMap.imports)) {
    const match = path.match(/^\/assets\/packages\/(@[^/]+\/[^/]+|[^/]+)/);
    if (match) {
      eleventyConfig.addPassthroughCopy({
        [`node_modules/${match[1]}`]: `/assets/packages/${match[1]}`,
      });
    }
  }

  // Inject import map into HTML
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
