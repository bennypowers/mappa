import { platform, arch } from "node:process";
import { spawnSync } from "node:child_process";

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

const result = spawnSync("npm", ["install", "--no-save", pkg], { stdio: "inherit", shell: true });
if (result.status !== 0) {
  console.error(`Failed to install platform binary package: ${pkg}`);
  process.exit(1);
}
