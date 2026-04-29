// Go's wasm_exec.js checks `if (!globalThis.fs)` (and .path) and installs
// ENOSYS stubs when missing. Node.js `--test` mode does not expose these
// globals. Patch them before any WASM imports.
import * as fs from "node:fs";
import * as path from "node:path";

const g = globalThis as Record<string, unknown>;
if (!g.fs) g.fs = fs;
if (!g.path) g.path = path;
