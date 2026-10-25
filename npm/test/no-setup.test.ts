// This test intentionally does NOT import a setup module. Its process must
// resolve a real module through the WASM API using only the globals that the
// library installs itself.
import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { createServer, type RequestListener } from "node:http";
import type { AddressInfo } from "node:net";

import { generate, resolve } from "../dist/mappa.js";

// Route the WASM fetcher's requests to a local HTTP server so CDN failures
// are deterministic. The wrapper keeps the original path and query, which is
// all the CDN templates vary.
async function withLocalCDN<T>(
  handler: RequestListener,
  fn: () => Promise<T>
): Promise<T> {
  const server = createServer(handler);
  await new Promise<void>((resolveListen) =>
    server.listen(0, "127.0.0.1", resolveListen)
  );
  const address = server.address() as AddressInfo;
  const base = `http://127.0.0.1:${address.port}`;
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((input: string | URL | Request, init?: RequestInit) => {
    const raw =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.href
          : input.url;
    const url = new URL(raw);
    return realFetch(new URL(url.pathname + url.search, base), init);
  }) as typeof globalThis.fetch;

  try {
    return await fn();
  } finally {
    globalThis.fetch = realFetch;
    await new Promise<void>((resolveClose) => server.close(() => resolveClose()));
  }
}

function sendJSON(
  res: import("node:http").ServerResponse,
  status: number,
  body: string
): void {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(body);
}

describe("CDN error classification", () => {
  it("rejects with ERR_NOT_FOUND for a missing package", async () => {
    await withLocalCDN(
      (_req, res) => sendJSON(res, 404, '{"error":"not found"}'),
      async () => {
        await assert.rejects(
          () => generate({ dependencies: { missing: "1.0.0" } }),
          (err: NodeJS.ErrnoException) => err.code === "ERR_NOT_FOUND"
        );
      }
    );
  });

  it("rejects with ERR_RESPONSE_TOO_LARGE for an oversized response", async () => {
    const oversized = "x".repeat(64 * 1024 * 1024 + 1);
    await withLocalCDN(
      (_req, res) => sendJSON(res, 200, oversized),
      async () => {
        await assert.rejects(
          () => generate({ dependencies: { huge: "1.0.0" } }),
          (err: NodeJS.ErrnoException) =>
            err.code === "ERR_RESPONSE_TOO_LARGE"
        );
      }
    );
  });

  it("returns warnings instead of rejecting with allowPartial", async () => {
    await withLocalCDN(
      (req, res) => {
        if (req.url?.startsWith("/good@1.0.0/")) {
          sendJSON(
            res,
            200,
            JSON.stringify({
              name: "good",
              version: "1.0.0",
              exports: "./index.js",
            })
          );
          return;
        }
        sendJSON(res, 404, '{"error":"not found"}');
      },
      async () => {
        const im = await generate(
          { dependencies: { good: "1.0.0", missing: "1.0.0" } },
          { allowPartial: true }
        );
        assert.equal(im.imports?.["good"], "https://esm.sh/good@1.0.0/index.js");
        assert.ok(im.warnings && im.warnings.length > 0);
        assert.match(im.warnings.join("\n"), /missing@1\.0\.0/);
      }
    );
  });
});

describe("option validation", () => {
  it("rejects non-object options", async () => {
    await assert.rejects(
      () => generate({ dependencies: {} }, "nope" as unknown as object),
      /options must be an object/
    );
  });

  it("rejects array options instead of normalizing them", async () => {
    await assert.rejects(
      () => generate({ dependencies: {} }, [] as unknown as object),
      /options must be an object/
    );
  });

  it("rejects instead of dying when option access throws", async () => {
    const options = new Proxy(
      {},
      {
        get() {
          throw new Error("throwing getter");
        },
      }
    );
    await assert.rejects(
      () => generate({ dependencies: {} }, options as never),
      /throwing getter/
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
