// Minimal HTTP server for the js/wasm transport tests.
//
// Usage: node scripts/wasm-test-server.mjs <url-file>
//
// Writes the base URL to <url-file> once listening, then serves the fixture
// endpoints that cdn/fetch_wasm_test.go exercises.
import { createServer } from "node:http";
import { writeFileSync } from "node:fs";

const urlFile = process.argv[2];
if (!urlFile) {
  console.error("usage: wasm-test-server.mjs <url-file>");
  process.exit(1);
}

const server = createServer((req, res) => {
  const { pathname } = new URL(req.url ?? "/", "http://localhost");
  switch (pathname) {
    case "/ok":
      res.writeHead(200, { "content-type": "application/json" });
      res.end('{"ok":true}');
      break;
    case "/status/404":
      res.writeHead(404, { "content-type": "text/plain" });
      res.end("not found");
      break;
    case "/large":
      res.writeHead(200, { "content-type": "text/plain" });
      res.end("x".repeat(4096));
      break;
    case "/slow":
      setTimeout(() => {
        res.writeHead(200, { "content-type": "text/plain" });
        res.end("slow");
      }, 500);
      break;
    default:
      res.writeHead(404);
      res.end();
  }
});

server.listen(0, "127.0.0.1", () => {
  const { port } = server.address();
  writeFileSync(urlFile, `http://127.0.0.1:${port}`);
});

// Never outlive the test run.
setTimeout(() => server.close(() => process.exit(0)), 120000).unref();
