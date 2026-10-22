import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { arch, platform } from "node:process";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

// When running from source (for example `npm install` in the repo before
// `npm run build`), there is no dist entry point; do nothing.
if (!existsSync(join(__dirname, "postinstall.js"))) {
  process.exit(0);
}

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
try {
  const own = JSON.parse(
    readFileSync(join(__dirname, "..", "package.json"), "utf8")
  ) as { version?: string };
  version = own.version ?? "";
} catch {
  version = "";
}

if (!version) {
  console.error("mappa: could not determine package version for fallback install");
  process.exit(1);
}

// Prefer the npm CLI that is running this install. Executing npm-cli.js with
// the current Node binary avoids shell:true and the bare "npm" command.
const npmExecPath = process.env.npm_execpath;
const command = npmExecPath ? process.execPath : platform === "win32" ? "npm.cmd" : "npm";
const args = npmExecPath
  ? [npmExecPath, "install", "--no-save", `${pkg}@${version}`]
  : ["install", "--no-save", `${pkg}@${version}`];

const result = spawnSync(command, args, { stdio: "inherit", shell: false });
if (result.status !== 0) {
  console.error(`Failed to install platform binary package: ${pkg}@${version}`);
  process.exit(1);
}
