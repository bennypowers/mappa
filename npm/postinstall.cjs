// Cross-platform postinstall guard.
//
// The real postinstall logic is compiled to dist/postinstall.js. It is absent
// when installing from a source checkout before `npm run build`, in which case
// there is nothing to do (exit 0). Using a Node script avoids POSIX-only
// shell syntax, which fails under cmd.exe on Windows.
"use strict";

const { existsSync } = require("node:fs");
const { join } = require("node:path");
const { spawnSync } = require("node:child_process");

const target = join(__dirname, "dist", "postinstall.js");
if (!existsSync(target)) {
  process.exit(0);
}

const result = spawnSync(process.execPath, [target], { stdio: "inherit" });
process.exit(result.status ?? 1);
