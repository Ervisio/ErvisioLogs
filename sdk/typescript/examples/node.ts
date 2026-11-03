// Run from sdk/typescript with:
//   ERVISIO_LOGS_URL=http://localhost:8080 ERVISIO_LOGS_TOKEN=<ingest token> npx tsx examples/node.ts
// In your own project, import from "@ervisio/logs" instead of "../src/index.js".
import { ErvisioLogs, type ErvisioError } from "../src/index.js";

const baseUrl = process.env.ERVISIO_LOGS_URL ?? "http://localhost:8080";
const token = process.env.ERVISIO_LOGS_TOKEN;
if (!token) {
  console.error("Set ERVISIO_LOGS_TOKEN to an API token with the logs:ingest scope.");
  process.exit(1);
}

const logs = new ErvisioLogs({
  baseUrl,
  token,
  stream: "default",
  environment: process.env.ERVISIO_LOGS_ENV,
  service: "example-app",
  gzip: true,
  onError: (err: ErvisioError) => {
    console.error(`[ervisio-logs] ${err.kind}: ${err.message}`);
  },
});

logs.info("app.started", "example app started", { pid: process.pid, node: process.version });
logs.warn("cache.miss", "cold cache", { key: "user:42", ttl_ms: 0 });
logs.error("payment.failed", "card declined", { order: 1234, reason: "insufficient_funds" }, { trace_id: "0af7651916cd43dd8448eb211c80319c" });

// Flush and stop the timer before the process exits.
process.on("SIGTERM", () => {
  void logs.close().finally(() => process.exit(0));
});

const result = await logs.flush();
console.log("flush:", result.accepted, "accepted,", result.rejected, "rejected,", result.failed, "failed");
console.log("stats:", logs.stats());
await logs.close();
