#!/usr/bin/env node
import { platform, arch } from "node:os";
import { createRequire } from "node:module";
import { chmodSync, accessSync, constants } from "node:fs";
import { spawn } from "node:child_process";

const require = createRequire(import.meta.url);
const supportedTargets = new Set([
  "linux-x64",
  "linux-arm64",
  "darwin-x64",
  "darwin-arm64",
  "win32-x64",
  "win32-arm64",
]);

const target = `${platform()}-${arch()}`;

if (!supportedTargets.has(target)) {
  console.error(`mappa: Unsupported platform/arch: ${target}`);
  process.exit(1);
}

let binPath: string | undefined;
try {
  binPath = require.resolve(
    `@pwrs/mappa-${target}/mappa${platform() === "win32" ? ".exe" : ""}`
  );
} catch {
  console.error(
    `mappa: Platform binary package @pwrs/mappa-${target} not installed. Was there an install error?`
  );
  process.exit(1);
}

if (platform() !== "win32") {
  try {
    accessSync(binPath!, constants.X_OK);
  } catch {
    chmodSync(binPath!, 0o755);
  }
}

const child = spawn(binPath!, process.argv.slice(2), { stdio: "inherit" });

const signals: NodeJS.Signals[] = ["SIGTERM", "SIGINT", "SIGHUP"];
signals.forEach((signal) => {
  process.on(signal, () => {
    child.kill(signal);
  });
});

child.on("error", (err: Error) => {
  console.error(`mappa: Failed to spawn binary: ${err.message}`);
  process.exit(1);
});

const signalNumbers: Record<string, number> = {
  SIGHUP: 1, SIGINT: 2, SIGQUIT: 3, SIGTERM: 15,
};

child.on("exit", (code: number | null, signal: NodeJS.Signals | null) => {
  if (signal) {
    process.exit(128 + (signalNumbers[signal] ?? 1));
  }
  process.exit(code ?? 1);
});
