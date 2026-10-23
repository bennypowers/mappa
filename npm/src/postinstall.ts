import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
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

// Choose how to run the fallback install. Executing npm-cli.js with the
// current Node binary avoids shell:true and the bare "npm" command. On
// Windows without npm_execpath, spawning npm.cmd directly fails with EINVAL
// (BatBadBut), so use the npm-cli.js inside the Node installation instead and
// keep shell:true only as a last resort.
let command: string;
let args: string[];
let shell = false;

// npm_execpath is not always a file Node can run with npm's arguments: Yarn
// and pnpm point it at their own entry points, which reject
// `install --no-save`. Use each manager's add form instead.
const manager = (process.env.npm_config_user_agent ?? "").split("/")[0];

if (manager === "yarn") {
  command = "yarn";
  args = ["add", `${pkg}@${version}`];
  // Yarn is a shell wrapper on Windows, so it needs a shell to launch.
  shell = platform === "win32";
} else if (manager === "pnpm") {
  command = "pnpm";
  args = ["add", `${pkg}@${version}`];
  shell = platform === "win32";
} else if (process.env.npm_execpath) {
  command = process.execPath;
  args = [process.env.npm_execpath, ...installArgs];
} else if (platform === "win32") {
  const npmCli = join(
    dirname(process.execPath),
    "node_modules",
    "npm",
    "bin",
    "npm-cli.js"
  );
  if (existsSync(npmCli)) {
    command = process.execPath;
    args = [npmCli, ...installArgs];
  } else {
    command = "npm.cmd";
    args = installArgs;
    shell = true;
  }
} else {
  command = "npm";
  args = installArgs;
}

let result: ReturnType<typeof spawnSync>;
try {
  result = spawnSync(command, args, { stdio: "inherit", shell });
} catch (err) {
  console.error(
    `Failed to launch ${command}: ${
      err instanceof Error ? err.message : String(err)
    }`
  );
  process.exit(1);
}

if (result.error) {
  console.error(`Failed to launch ${command}: ${result.error.message}`);
}
if (result.status !== 0) {
  console.error(`Failed to install platform binary package: ${pkg}@${version}`);
  process.exit(1);
}
