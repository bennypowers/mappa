#!/usr/bin/env node
/**
 * Benchmark script for @jspm/generator with nodemodules provider
 *
 * Usage (matching mappa CLI flags):
 *   node jspm-local-bench.mjs --package /path/to/project
 *   node jspm-local-bench.mjs -p /path/to/project
 *
 * Or with hyperfine:
 *   hyperfine 'mappa generate --package /path' 'node jspm-local-bench.mjs --package /path'
 */
import { Generator } from '@jspm/generator';
import { readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';

// Parse CLI args to match mappa's interface
const { values } = parseArgs({
  options: {
    package: { type: 'string', short: 'p', default: '.' },
  },
  allowPositionals: true,
});

const projectDir = resolve(values.package);

const generator = new Generator({
  mapUrl: pathToFileURL(projectDir + '/').href,
  defaultProvider: 'nodemodules',
  env: ['production', 'browser', 'module'],
});

// Read dependencies from package.json
const pkgPath = join(projectDir, 'package.json');
const pkg = JSON.parse(await readFile(pkgPath, 'utf-8'));
const deps = Object.keys(pkg.dependencies || {});

for (const dep of deps) {
  await generator.install(dep);
}

console.log(JSON.stringify(generator.getMap(), null, 2));
