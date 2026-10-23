// This test intentionally does NOT import a setup module. Its process must
// resolve a real module through the WASM API using only the globals that the
// library installs itself.
import { describe, it } from "node:test";
import { strict as assert } from "node:assert";

import { generate, resolve } from "../dist/mappa.js";

describe("option validation", () => {
  it("rejects non-object options", async () => {
    await assert.rejects(
      () => generate({ dependencies: {} }, "nope" as unknown as object),
      /options must be an object/
    );
  });

  it("rejects non-object resolve options", async () => {
    await assert.rejects(
      () =>
        resolve(
          "/nonexistent/path",
          "nope" as unknown as { conditions: string[] }
        ),
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

  it("rejects plain objects as conditions", async () => {
    await assert.rejects(
      () =>
        resolve("/nonexistent/path", {
          conditions: {} as unknown as string[],
        }),
      /conditions must be an array of strings/
    );
  });

  it("rejects non-array exclude", async () => {
    await assert.rejects(
      () =>
        resolve("/nonexistent/path", {
          exclude: "x" as unknown as string[],
        }),
      /exclude must be an array of strings/
    );
  });

  it("rejects array entries that are not strings", async () => {
    await assert.rejects(
      () =>
        generate(
          { dependencies: {} },
          { exclude: [42] } as unknown as { exclude: string[] }
        ),
      /exclude must contain only strings/
    );
    await assert.rejects(
      () =>
        resolve("/nonexistent/path", {
          conditions: [1] as unknown as string[],
        }),
      /conditions must contain only strings/
    );
  });

  it("rejects non-object inputMap", async () => {
    await assert.rejects(
      () =>
        resolve("/nonexistent/path", {
          inputMap: "nope" as unknown as Record<string, unknown>,
        }),
      /inputMap must be an object/
    );
  });

  it("rejects non-number optimize", async () => {
    await assert.rejects(
      () =>
        resolve("/nonexistent/path", {
          optimize: "1" as unknown as 1,
        }),
      /optimize must be a number/
    );
  });

  it("rejects unknown CDN providers", async () => {
    await assert.rejects(
      () => generate({ dependencies: {} }, { cdn: "bogus" as never }),
      /unknown CDN provider.*esm\.sh.*unpkg.*jsdelivr/
    );
  });
});
