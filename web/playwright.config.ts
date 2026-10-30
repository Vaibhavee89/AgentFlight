import { defineConfig, devices } from "@playwright/test";

// The smoke test drives a real agentflight binary (built by `make e2e`), on its
// own port and database, so it never touches demo data.
const PORT = 18080;

export default defineConfig({
  testDir: "e2e",
  timeout: 60_000,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: `http://127.0.0.1:${String(PORT)}`,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `rm -f ../e2e.db ../e2e.db-wal ../e2e.db-shm && ../bin/agentflight serve --addr 127.0.0.1:${String(PORT)} --db ../e2e.db --mock-latency 400ms --bench-dir ../docs`,
    url: `http://127.0.0.1:${String(PORT)}/healthz`,
    reuseExistingServer: false,
    timeout: 60_000,
  },
});
