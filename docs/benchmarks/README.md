# Benchmarks

Comparing mappa with @jspm/generator for import map generation.

## Test Setup

Benchmarks run against [Red Hat Design System](https://github.com/RedHat-UX/red-hat-design-system), a real-world monorepo with ~20 dependencies including lit, PatternFly, and RHDS packages.

Both use local resolution (no network requests).

## Results

### Real-World Project (RHDS)

| Tool | Time |
|------|------|
| mappa | 3.2ms ± 0.2ms |
| @jspm/generator | 230ms ± 6ms |

**mappa is ~72x faster** for local import map generation.

## Running the Benchmarks

### Prerequisites

```bash
# Install hyperfine for accurate benchmarking
# macOS: brew install hyperfine
# Linux: cargo install hyperfine

# Install @jspm/generator in your test project
npm install @jspm/generator
```

### Run Comparison

```bash
# Copy benchmark script to your project
cp docs/benchmarks/jspm-local-bench.mjs ./

# Run comparison
hyperfine --warmup 3 'mappa generate' 'node jspm-local-bench.mjs'
```

### Individual Benchmarks

```bash
# mappa
hyperfine -N 'mappa generate'

# jspm (local)
time node jspm-local-bench.mjs > /dev/null
```

## Why the Difference?

mappa is faster because:
1. Written in Go with efficient JSON parsing
2. Parallel dependency resolution with bounded concurrency
3. No JavaScript runtime overhead
4. Minimal memory allocations
5. Reads only package.json metadata (no file content parsing)

@jspm/generator is slower because:
1. Runs in Node.js with JavaScript overhead
2. More complex resolution algorithm with async/await
3. Parses actual file contents to trace imports
4. Sequential dependency resolution

## Hardware

Benchmarks run on:
- CPU: AMD Ryzen Threadripper 9960X
- OS: Linux
- Node.js: v22.13.1
- Go: 1.25.5
