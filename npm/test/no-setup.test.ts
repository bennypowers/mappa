// This test intentionally does NOT import a setup module. Its process must
// resolve a real module through the WASM API using only the globals that the
// library installs itself.
import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { resolve as pathResolve } from "node:path";

import { generate, resolve } from "../dist/mappa.js";

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

  it("rejects non-object options", async () => {
    await assert.rejects(
      () => generate({ dependencies: {} }, "nope" as unknown as object),
      /options must be an object/
    );
  });

  it("rejects non-array conditions", async () => {
    await assert.rejects(
      () =>
        generate(
          { dependencies: {} },
          { conditions: "browser" } as unknown as { conditions: string[] }
        ),
      /conditions must be an array of strings/
    );
  });

  it("rejects unknown CDN providers", async () => {
    await assert.rejects(
      () => generate({ dependencies: {} }, { cdn: "bogus" as never }),
      /unknown CDN provider.*esm\.sh.*unpkg.*jsdelivr/
    );
  });
});
