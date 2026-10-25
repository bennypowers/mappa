import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, basename, join } from "node:path";
import { arch, platform } from "node:process";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

const platformPackages: Record<string, string> = {
  "darwin-x64": "@pwrs/mappa-darwin-x64",
  "darwin-arm64": "@pwrs/mappa-darwin-arm64",
  "linux-x64": "@pwrs/mappa-linux-x64",
  "linux-arm64": "@pwrs/mappa-linux-arm64",
  "win32-x64": "@pwrs/mappa-win32-x64",
  "win32-arm64": "@pwrs/mappa-win32-arm64",
};

const pkg = platformPackages[`${platform}-${arch}`];

if (!pkg) {
  console.error(
    `Unsupported platform: ${platform}-${arch}. ` +
      `Please check https://github.com/bennypowers/mappa for supported platforms.`
  );
  process.exit(1);
}

const require = createRequire(import.meta.url);
const binary = `mappa${platform === "win32" ? ".exe" : ""}`;

// optionalDependencies is the primary install mechanism. If the platform
// package is already present, there is nothing to do: in particular, do not
// make a network request or mutate the parent's node_modules.
try {
  require.resolve(`${pkg}/${binary}`);
  process.exit(0);
} catch {
  // Fall through to the fallback install below.
}

// Read this package's own version so the fallback install is pinned and
// cannot pull a binary that mismatches the wrapper.
let version: string;
let versionError: unknown;
try {
  const own = JSON.parse(
    readFileSync(join(__dirname, "..", "package.json"), "utf8")
  ) as { version?: string };
  version = own.version ?? "";
} catch (err) {
  versionError = err;
  version = "";
}

if (!version) {
  console.error("mappa: could not determine package version for fallback install");
  if (versionError) {
    console.error(
      `mappa: ${
        versionError instanceof Error ? versionError.message : String(versionError)
      }`
    );
  }
  process.exit(1);
}

const installArgs = ["install", "--no-save", `${pkg}@${version}`];

// Prefer the npm CLI so the fallback keeps --no-save semantics and never
// rewrites the installed manifest. Yarn and pnpm point npm_execpath at their
// own entry points, which reject `install --no-save`, so those are ignored
// here and used only if npm cannot be launched at all.
const manager = (process.env.npm_config_user_agent ?? "").split("/")[0];
const npmExecPath = process.env.npm_execpath;

interface InstallCommand {
  command: string;
  args: string[];
  shell: boolean;
}

function npmCommand(): InstallCommand {
  const npmExecName = npmExecPath ? basename(npmExecPath) : "";
  if (npmExecPath && /^npm(-cli)?\.(c|m)?js$/.test(npmExecName)) {
    return {
      command: process.execPath,
      args: [npmExecPath, ...installArgs],
      shell: false,
    };
  }
  if (platform === "win32") {
    const npmCli = join(
      dirname(process.execPath),
      "node_modules",
      "npm",
      "bin",
      "npm-cli.js"
    );
    if (existsSync(npmCli)) {
      return {
        command: process.execPath,
        args: [npmCli, ...installArgs],
        shell: false,
      };
    }
    return { command: "npm.cmd", args: installArgs, shell: true };
  }
  return { command: "npm", args: installArgs, shell: false };
}

const primary = npmCommand();

// If npm is not installed at all, fall back to the manager that launched us.
// `add` writes to the installed package's manifest, but it is the only way to
// fetch the optional binary when npm is unavailable.
const managerFallback: InstallCommand | null =
  manager === "yarn" || manager === "pnpm"
    ? {
        command: manager,
        args: ["add", `${pkg}@${version}`],
        shell: platform === "win32",
      }
    : null;

const installTimeoutMs = 10 * 60 * 1000;

function installOptions(
  useShell: boolean
): Parameters<typeof spawnSync>[2] {
  return {
    // Leave stdin alone: a postinstall must never consume the installer's
    // input or block waiting for it.
    stdio: ["ignore", "inherit", "inherit"],
    shell: useShell,
    timeout: installTimeoutMs,
  };
}

let attempted = primary;
let result: ReturnType<typeof spawnSync>;
try {
  result = spawnSync(
    primary.command,
    primary.args,
    installOptions(primary.shell)
  );
  if (
    managerFallback &&
    result.error &&
    (result.error as NodeJS.ErrnoException).code === "ENOENT"
  ) {
    attempted = managerFallback;
    result = spawnSync(
      managerFallback.command,
      managerFallback.args,
      installOptions(managerFallback.shell)
    );
  }
} catch (err) {
  console.error(
    `Failed to launch ${attempted.command}: ${
      err instanceof Error ? err.message : String(err)
    }`
  );
  process.exit(1);
}

if (result.error) {
  const code = (result.error as NodeJS.ErrnoException).code;
  if (code === "ETIMEDOUT") {
    console.error(
      `Failed to install ${pkg}@${version} via ${attempted.command}: command timed out after ${installTimeoutMs}ms`
    );
  } else {
    console.error(
      `Failed to launch ${attempted.command}: ${result.error.message}`
    );
  }
}
if (result.signal) {
  console.error(`Failed to install ${pkg}@${version}: terminated by signal ${result.signal}`);
}
if (result.status !== 0) {
  console.error(`Failed to install platform binary package: ${pkg}@${version}`);
  process.exit(1);
}
