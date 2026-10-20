import { defineConfig, devices } from "@playwright/test";
import { tmpdir } from "node:os";
import { join } from "node:path";

const PORT = Number(process.env.RAFTKV_E2E_PORT ?? 8099);

/**
 * End-to-end tests drive the REAL cluster: the runner starts
 * `raftkv cluster` (three Raft nodes + this dashboard, embedded) on a spare
 * port with a fresh WAL directory. Tests share one cluster and mutate it, so
 * they run serially and each one restores every node before it finishes.
 *
 *   npm run build && npm run test:e2e
 */
export default defineConfig({
  testDir: "tests",
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  timeout: 45_000,
  expect: { timeout: 8_000 },
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } } },
    { name: "mobile", use: { ...devices["Pixel 7"] }, grep: /@mobile/ },
  ],
  webServer: {
    command: `go run ./cmd/raftkv cluster -port ${PORT} -base-port ${PORT + 1000} -quiet -data ${JSON.stringify(join(tmpdir(), `raftkv-e2e-${PORT}`))}`,
    cwd: "..",
    url: `http://localhost:${PORT}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
