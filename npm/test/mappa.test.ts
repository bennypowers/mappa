import "./setup.ts";

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { resolve as pathResolve } from "node:path";

import { resolve, version } from "../dist/mappa.js";
import type { ImportMap } from "../dist/mappa.js";

const fixtureDir = (...segments: string[]) =>
  pathResolve(import.meta.dirname!, "..", "..", "testdata", ...segments);

describe("version", () => {
  it("returns a string", async () => {
    const v = await version();
    assert.equal(typeof v, "string");
    assert.ok(v.length > 0);
  });
});

describe("resolve", () => {
  it("resolves simple-pkg with default options", async () => {
    const im = await resolve(fixtureDir("resolve", "simple-pkg"));
    assert.ok(im.imports);
    assert.equal(im.imports["lit"], "/node_modules/lit/index.js");
  });

  it("applies template option", async () => {
    const im = await resolve(fixtureDir("resolve", "simple-pkg"), {
      template: "/assets/{package}/{path}",
    });
    assert.ok(im.imports);
    assert.equal(im.imports["lit"], "/assets/lit/index.js");
  });

  it("excludes packages", async () => {
    const im = await resolve(fixtureDir("resolve", "with-exclude"), {
      exclude: ["lodash"],
    });
    assert.ok(im.imports);
    assert.equal(im.imports["lodash"], undefined);
    assert.ok(im.imports["lit"]);
  });

  it("returns scopes for packages with dependencies", async () => {
    const im = await resolve(fixtureDir("resolve", "with-scopes"));
    assert.ok(im.scopes);
    assert.ok(Object.keys(im.scopes).length > 0);
  });

  it("accepts inputMap option", async () => {
    const inputMap: ImportMap = {
      imports: { "custom-pkg": "/custom/path.js" },
    };
    const im = await resolve(fixtureDir("resolve", "simple-pkg"), {
      inputMap,
    });
    assert.ok(im.imports);
    assert.equal(im.imports["custom-pkg"], "/custom/path.js");
    assert.ok(im.imports["lit"]);
  });

  it("returns empty map for invalid rootDir", async () => {
    const im = await resolve("/nonexistent/path");
    assert.equal(im.imports, undefined);
  });

  it("applies optimize=0 to skip simplification", async () => {
    const optimized = await resolve(fixtureDir("resolve", "simple-pkg"), {
      optimize: 1,
    });
    const unoptimized = await resolve(fixtureDir("resolve", "simple-pkg"), {
      optimize: 0,
    });
    assert.ok(optimized.imports);
    assert.ok(unoptimized.imports);
  });
});
