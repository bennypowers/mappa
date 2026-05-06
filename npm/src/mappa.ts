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
}

interface GoConstructor {
  new (): {
    run(instance: WebAssembly.Instance): void;
    importObject: WebAssembly.Imports;
  };
}

interface MappaApi {
  generate(packageJson: string, options?: GenerateOptions): Promise<string>;
  resolve(rootDir: string, options?: ResolveOptions): Promise<string>;
  version: string;
}

declare global {
  var Go: GoConstructor;
  var mappa: MappaApi;
}

let initPromise: Promise<MappaApi> | null = null;

async function init(): Promise<MappaApi> {
  if (initPromise) return initPromise;
  initPromise = (async () => {
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
  const resultStr = await mappa.generate(input, options);
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
  const resultStr = await mappa.resolve(rootDir, options);
  return JSON.parse(resultStr);
}

/**
 * Get the mappa WASM version.
 */
export async function version(): Promise<string> {
  const mappa = await init();
  return mappa.version;
}
