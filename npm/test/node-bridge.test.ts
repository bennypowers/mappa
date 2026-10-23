// The node filesystem bridge test must run in its own process because the
// library memoizes init(): importing any other test file first would already
// have installed globalThis.fs and globalThis.path.
import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { resolve as pathResolve } from "node:path";

import { resolve } from "../dist/mappa.js";

const fixtureDir = (...segments: string[]) =>
  pathResolve(import.meta.dirname!, "..", "..", "testdata", ...segments);

const g = globalThis as Record<string, unknown>;

describe("node filesystem bridge", () => {
  it("resolves from a real module file without pre-patched globals", async () => {
    // Node does not expose fs/path on globalThis; wasm_exec.js would install
    // ENOSYS stubs without the library's bridge.
    delete g.fs;
    delete g.path;

    const im = await resolve(fixtureDir("resolve", "simple-pkg"));
    assert.ok(im.imports, "expected imports");
    assert.equal(im.imports["lit"], "/node_modules/lit/index.js");

    assert.ok(g.fs, "expected library to install globalThis.fs");
    assert.ok(g.path, "expected library to install globalThis.path");
  });
});
