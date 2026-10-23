import { readFile } from "node:fs/promises";
import { pathToFileURL, fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));

export interface GenerateOptions {
  /** CDN provider name */
  cdn?: "esm.sh" | "unpkg" | "jsdelivr";
  /** Custom CDN URL template */
  template?: string;
  /** Export conditions to resolve */
  conditions?: string[];
  /** Packages to exclude from the generated map, including as transitive dependencies */
  exclude?: string[];
  /**
   * Per-request timeout in milliseconds. Defaults to 30000. Set to 0 to
   * disable the timeout.
   */
  timeoutMs?: number;
  /**
   * Return the partial import map (with a `warnings` array) when some
   * dependencies fail to resolve, instead of rejecting.
   * @default false
   */
  allowPartial?: boolean;
}

export interface ResolveOptions {
  /** URL template (default: /node_modules/{package}/{path}) */
  template?: string;
  /** Export condition priority (e.g., ["production", "browser", "import", "default"]) */
  conditions?: string[];
  /** Additional packages to include beyond dependencies */
  includePackages?: string[];
  /** Packages to exclude from the generated map */
  exclude?: string[];
  /** Import map to merge with generated output (input map takes precedence) */
  inputMap?: ImportMap;
  /** Optimization level: 0=none, 1=simplify+dedup (default: 1) */
  optimize?: 0 | 1;
  /** Rebase workspace paths relative to this directory (node_modules paths unaffected) */
  pathBase?: string;
  /** Limit dependency resolution to this package's dependencies */
  packageDeps?: string;
}

export interface ImportMap {
  imports?: Record<string, string>;
  scopes?: Record<string, Record<string, string>>;
  /** Non-fatal resolution problems, present when `allowPartial` is enabled. */
  warnings?: string[];
}

interface GoConstructor {
  new (): {
    run(instance: WebAssembly.Instance): void;
    importObject: WebAssembly.Imports;
  };
}

interface MappaApi {
  generate(
    packageJson: string,
    options?: GenerateOptions | null
  ): Promise<string>;
  resolve(rootDir: string, options?: ResolveOptions | null): Promise<string>;
  version: string;
}

declare global {
  var Go: GoConstructor;
  var mappa: MappaApi;
}

let initPromise: Promise<MappaApi> | null = null;

const generateOptionKeys = [
  "cdn",
  "template",
  "conditions",
  "exclude",
  "timeoutMs",
  "allowPartial",
] as const satisfies readonly (keyof GenerateOptions)[];

const resolveOptionKeys = [
  "template",
  "conditions",
  "includePackages",
  "exclude",
  "inputMap",
  "optimize",
  "pathBase",
  "packageDeps",
] as const satisfies readonly (keyof ResolveOptions)[];

// Completeness assertions: adding an option without listing its key above
// makes these assignments fail compilation.
type MissingGenerateKeys = Exclude<
  keyof GenerateOptions,
  (typeof generateOptionKeys)[number]
>;
type MissingResolveKeys = Exclude<
  keyof ResolveOptions,
  (typeof resolveOptionKeys)[number]
>;
const _generateKeysComplete: [MissingGenerateKeys] extends [never] ? true : never =
  true;
const _resolveKeysComplete: [MissingResolveKeys] extends [never] ? true : never =
  true;
void _generateKeysComplete;
void _resolveKeysComplete;

// Copy caller options into a fresh plain object before they cross into Go.
// syscall/js property access is unguarded: a throwing getter or Proxy trap on
// the original object would abort the host process instead of rejecting the
// promise. Reading the known keys here keeps any such throw inside JS.
function normalizeOptions<T>(
  options: T | undefined | null,
  keys: readonly string[]
): T | undefined | null {
  if (options === undefined || options === null) return options;
  // Arrays are objects, so pass them through unchanged for Go to reject with
  // its own error instead of silently normalizing them to an object.
  if (Array.isArray(options)) return options;
  if (typeof options !== "object") return options;

  const normalized: Record<string, unknown> = {};
  for (const key of keys) {
    const value = (options as Record<string, unknown>)[key];
    if (value === undefined) continue;
    // Array.from would turn non-arrays into arrays, so only copy real arrays.
    // Anything else passes through for Go to validate.
    normalized[key] = Array.isArray(value) ? value.slice() : value;
  }
  return normalized as T;
}

async function init(): Promise<MappaApi> {
  if (initPromise) return initPromise;
  initPromise = (async () => {
    // Go's wasm_exec.js installs ENOSYS filesystem stubs when globalThis.fs
    // and globalThis.path are missing. Node does not define them on
    // globalThis, so a normal Node ESM process must bridge them before
    // importing wasm_exec.js; otherwise local resolution silently fails.
    if (typeof process !== "undefined" && process.versions?.node) {
      const g = globalThis as Record<string, unknown>;
      // Assign unconditionally. A pre-existing globalThis.fs or globalThis.path
      // may be the ENOSYS stub installed by an earlier wasm_exec.js import, and
      // keeping it makes local resolution silently return an empty map.
      g.fs = await import("node:fs");
      g.path = await import("node:path");
    }

    await import(pathToFileURL(join(__dirname, "wasm_exec.js")).href);

    const wasmPath = join(__dirname, "mappa.wasm");
    const wasmBytes = await readFile(wasmPath);

    const go = new Go();
    const { instance } = await WebAssembly.instantiate(
      wasmBytes,
      go.importObject
    );

    go.run(instance);

    if (typeof mappa == "undefined") {
      await new Promise<void>((resolve, reject) => {
        let attempts = 0;
        const check = () => {
          if (typeof mappa !== "undefined") {
            resolve();
          } else if (++attempts > 100) {
            reject(new Error("mappa WASM failed to initialize"));
          } else {
            setTimeout(check, 10);
          }
        };
        check();
      });
    }

    return mappa!;
  })().catch((err) => {
    initPromise = null;
    throw err;
  });
  return initPromise;
}

/**
 * Generate an import map from package.json contents using a CDN resolver.
 */
export async function generate(
  packageJson: string | Record<string, unknown>,
  options?: GenerateOptions
): Promise<ImportMap> {
  const mappa = await init();
  const input =
    typeof packageJson === "string"
      ? packageJson
      : JSON.stringify(packageJson);
  const resultStr = await mappa.generate(
    input,
    normalizeOptions(options, generateOptionKeys)
  );
  return JSON.parse(resultStr);
}

/**
 * Generate an import map from a local directory's node_modules.
 */
export async function resolve(
  rootDir: string,
  options?: ResolveOptions
): Promise<ImportMap> {
  const mappa = await init();
  const resultStr = await mappa.resolve(
    rootDir,
    normalizeOptions(options, resolveOptionKeys)
  );
  return JSON.parse(resultStr);
}

/**
 * Get the mappa WASM version.
 */
export async function version(): Promise<string> {
  const mappa = await init();
  return mappa.version;
}
