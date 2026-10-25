import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { resolve as pathResolve } from "node:path";

const npmDir = pathResolve(import.meta.dirname!, "..");
const distPostinstall = join(npmDir, "dist", "postinstall.js");

const platformPackages: Record<string, string> = {
  "darwin-x64": "@pwrs/mappa-darwin-x64",
  "darwin-arm64": "@pwrs/mappa-darwin-arm64",
  "linux-x64": "@pwrs/mappa-linux-x64",
  "linux-arm64": "@pwrs/mappa-linux-arm64",
  "win32-x64": "@pwrs/mappa-win32-x64",
  "win32-arm64": "@pwrs/mappa-win32-arm64",
};

// The compiled postinstall exits at import time, so exercise it as a
// subprocess with a copied dist tree and a fixture package.json.
function makeTempInstall(): string {
  const tmp = mkdtempSync(join(tmpdir(), "mappa-postinstall-"));
  mkdirSync(join(tmp, "dist"), { recursive: true });
  copyFileSync(distPostinstall, join(tmp, "dist", "postinstall.js"));
  return tmp;
}

function cleanup(tmp: string): void {
  rmSync(tmp, { recursive: true, force: true });
}

// writeStub installs an executable Node script named for a package manager and
// links node beside it so tests can run with PATH containing only the stub dir.
function writeStub(dir: string, name: string): string {
  mkdirSync(dir, { recursive: true });
  try {
    symlinkSync(process.execPath, join(dir, "node"));
  } catch {
    // Another stub already linked node.
  }
  const stub = join(dir, name);
  writeFileSync(
    stub,
    '#!/usr/bin/env node\n' +
      'import { writeFileSync } from "node:fs";\n' +
      "writeFileSync(process.env.ARGV_OUT, JSON.stringify(process.argv.slice(2)));\n"
  );
  chmodSync(stub, 0o755);
  return stub;
}

describe("postinstall", () => {
  it("exits 0 when the compiled postinstall is absent", () => {
    const tmp = mkdtempSync(join(tmpdir(), "mappa-postinstall-wrapper-"));
    try {
      const wrapper = join(tmp, "postinstall.cjs");
      copyFileSync(join(npmDir, "postinstall.cjs"), wrapper);

      const result = spawnSync(process.execPath, [wrapper], {
        encoding: "utf8",
        env: { ...process.env },
      });

      assert.equal(result.status, 0, result.stderr);
    } finally {
      cleanup(tmp);
    }
  });

  it("propagates the compiled postinstall exit code", () => {
    const tmp = mkdtempSync(join(tmpdir(), "mappa-postinstall-wrapper-"));
    try {
      const wrapper = join(tmp, "postinstall.cjs");
      copyFileSync(join(npmDir, "postinstall.cjs"), wrapper);
      mkdirSync(join(tmp, "dist"), { recursive: true });
      writeFileSync(join(tmp, "dist", "postinstall.js"), "process.exit(7);\n");

      const result = spawnSync(process.execPath, [wrapper], {
        encoding: "utf8",
        env: { ...process.env },
      });

      assert.equal(result.status, 7, result.stderr);
    } finally {
      cleanup(tmp);
    }
  });

  it("exits when the package version cannot be determined", () => {
    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({ name: "@pwrs/mappa-fixture", type: "module" })
      );

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env: { ...process.env } }
      );

      assert.equal(result.status, 1);
      assert.match(
        result.stderr,
        /could not determine package version for fallback install/
      );
    } finally {
      cleanup(tmp);
    }
  });

  it("spawns the npm CLI from npm_execpath with the pinned package", () => {
    const expectedPkg =
      platformPackages[`${process.platform}-${process.arch}`];
    assert.ok(expectedPkg, "test requires a supported platform");

    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      const argvOut = join(tmp, "argv.json");
      const npmCli = join(tmp, "npm-cli.js");
      writeFileSync(
        npmCli,
        'import { writeFileSync } from "node:fs";\n' +
          "writeFileSync(process.env.ARGV_OUT, JSON.stringify(process.argv.slice(2)));\n"
      );

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            npm_execpath: npmCli,
            npm_config_user_agent:
              "npm/10.0.0 node/v22.0.0 linux x64 workspaces/false",
            ARGV_OUT: argvOut,
          },
        }
      );

      assert.equal(result.status, 0, result.stderr);
      assert.deepEqual(JSON.parse(readFileSync(argvOut, "utf8")), [
        "install",
        "--no-save",
        `${expectedPkg}@1.2.3`,
      ]);
    } finally {
      cleanup(tmp);
    }
  });

  it("prefers npm --no-save when yarn launched the install", () => {
    const expectedPkg =
      platformPackages[`${process.platform}-${process.arch}`];
    assert.ok(expectedPkg, "test requires a supported platform");

    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      const argvOut = join(tmp, "argv.json");
      writeStub(join(tmp, "bin"), "npm");

      const env: NodeJS.ProcessEnv = {
        ...process.env,
        PATH: join(tmp, "bin"),
        ARGV_OUT: argvOut,
        npm_config_user_agent: "yarn/1.22.22 npm/? node/v22.0.0 linux x64",
      };
      delete env.npm_execpath;

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env }
      );

      assert.equal(result.status, 0, result.stderr);
      assert.deepEqual(JSON.parse(readFileSync(argvOut, "utf8")), [
        "install",
        "--no-save",
        `${expectedPkg}@1.2.3`,
      ]);
    } finally {
      cleanup(tmp);
    }
  });

  it("falls back to yarn add when npm is unavailable", () => {
    const expectedPkg =
      platformPackages[`${process.platform}-${process.arch}`];
    assert.ok(expectedPkg, "test requires a supported platform");

    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      const argvOut = join(tmp, "argv.json");
      writeStub(join(tmp, "bin"), "yarn");

      const env: NodeJS.ProcessEnv = {
        ...process.env,
        PATH: join(tmp, "bin"),
        ARGV_OUT: argvOut,
        npm_config_user_agent: "yarn/1.22.22 npm/? node/v22.0.0 linux x64",
      };
      delete env.npm_execpath;

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env }
      );

      assert.equal(result.status, 0, result.stderr);
      assert.deepEqual(JSON.parse(readFileSync(argvOut, "utf8")), [
        "add",
        `${expectedPkg}@1.2.3`,
      ]);
    } finally {
      cleanup(tmp);
    }
  });

  it("prefers npm --no-save when pnpm launched the install", () => {
    const expectedPkg =
      platformPackages[`${process.platform}-${process.arch}`];
    assert.ok(expectedPkg, "test requires a supported platform");

    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      const argvOut = join(tmp, "argv.json");
      writeStub(join(tmp, "bin"), "npm");

      const env: NodeJS.ProcessEnv = {
        ...process.env,
        PATH: join(tmp, "bin"),
        ARGV_OUT: argvOut,
        npm_config_user_agent: "pnpm/9.0.0 npm/? node/v22.0.0 linux x64",
      };
      delete env.npm_execpath;

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env }
      );

      assert.equal(result.status, 0, result.stderr);
      assert.deepEqual(JSON.parse(readFileSync(argvOut, "utf8")), [
        "install",
        "--no-save",
        `${expectedPkg}@1.2.3`,
      ]);
    } finally {
      cleanup(tmp);
    }
  });

  it("does not treat pnpm's npm_execpath as npm", () => {
    const expectedPkg =
      platformPackages[`${process.platform}-${process.arch}`];
    assert.ok(expectedPkg, "test requires a supported platform");

    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      const argvOut = join(tmp, "argv.json");
      const pnpmCjs = join(tmp, "pnpm.cjs");
      writeFileSync(pnpmCjs, "");
      writeStub(join(tmp, "bin"), "npm");

      const env: NodeJS.ProcessEnv = {
        ...process.env,
        PATH: join(tmp, "bin"),
        ARGV_OUT: argvOut,
        npm_execpath: pnpmCjs,
        npm_config_user_agent: "pnpm/9.0.0 npm/? node/v22.0.0 linux x64",
      };

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env }
      );

      assert.equal(result.status, 0, result.stderr);
      assert.deepEqual(JSON.parse(readFileSync(argvOut, "utf8")), [
        "install",
        "--no-save",
        `${expectedPkg}@1.2.3`,
      ]);
    } finally {
      cleanup(tmp);
    }
  });

  it("names the manager that actually failed to launch", () => {
    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      // Empty PATH: neither npm nor yarn can be launched.
      const binDir = join(tmp, "bin");
      mkdirSync(binDir);

      const env: NodeJS.ProcessEnv = {
        ...process.env,
        PATH: binDir,
        npm_config_user_agent: "yarn/1.22.22 npm/? node/v22.0.0 linux x64",
      };
      delete env.npm_execpath;

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env }
      );

      assert.equal(result.status, 1);
      assert.match(result.stderr, /Failed to launch yarn/);
    } finally {
      cleanup(tmp);
    }
  });

  it("falls back to pnpm add when npm is unavailable", () => {
    const expectedPkg =
      platformPackages[`${process.platform}-${process.arch}`];
    assert.ok(expectedPkg, "test requires a supported platform");

    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      const argvOut = join(tmp, "argv.json");
      writeStub(join(tmp, "bin"), "pnpm");

      const env: NodeJS.ProcessEnv = {
        ...process.env,
        PATH: join(tmp, "bin"),
        ARGV_OUT: argvOut,
        npm_config_user_agent: "pnpm/9.0.0 npm/? node/v22.0.0 linux x64",
      };
      delete env.npm_execpath;

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env }
      );

      assert.equal(result.status, 0, result.stderr);
      assert.deepEqual(JSON.parse(readFileSync(argvOut, "utf8")), [
        "add",
        `${expectedPkg}@1.2.3`,
      ]);
    } finally {
      cleanup(tmp);
    }
  });

  it("falls back to npm when no manager is identified", () => {
    const expectedPkg =
      platformPackages[`${process.platform}-${process.arch}`];
    assert.ok(expectedPkg, "test requires a supported platform");

    const tmp = makeTempInstall();
    try {
      writeFileSync(
        join(tmp, "package.json"),
        JSON.stringify({
          name: "@pwrs/mappa-fixture",
          type: "module",
          version: "1.2.3",
        })
      );

      const argvOut = join(tmp, "argv.json");
      const binDir = join(tmp, "bin");
      mkdirSync(binDir);
      const npm = join(binDir, "npm");
      writeFileSync(
        npm,
        '#!/usr/bin/env node\n' +
          'import { writeFileSync } from "node:fs";\n' +
          "writeFileSync(process.env.ARGV_OUT, JSON.stringify(process.argv.slice(2)));\n"
      );
      chmodSync(npm, 0o755);

      const env: NodeJS.ProcessEnv = {
        ...process.env,
        PATH: `${binDir}:${process.env.PATH ?? ""}`,
        ARGV_OUT: argvOut,
      };
      delete env.npm_execpath;
      delete env.npm_config_user_agent;

      const result = spawnSync(
        process.execPath,
        [join(tmp, "dist", "postinstall.js")],
        { encoding: "utf8", env }
      );

      assert.equal(result.status, 0, result.stderr);
      assert.deepEqual(JSON.parse(readFileSync(argvOut, "utf8")), [
        "install",
        "--no-save",
        `${expectedPkg}@1.2.3`,
      ]);
    } finally {
      cleanup(tmp);
    }
  });
});
