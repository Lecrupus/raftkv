// Dev-only: capture the dashboard at review sizes into ../.impeccable/review/.
// Usage: node scripts/shoot.mjs [url] [--kill]
import { chromium } from "@playwright/test";
import { mkdirSync } from "node:fs";

const url = process.argv.find((a) => a.startsWith("http")) ?? "http://localhost:8080";
const kill = process.argv.includes("--kill");
const out = new URL("../../.impeccable/review/", import.meta.url).pathname.replace(/^\/([A-Z]:)/, "$1");
mkdirSync(out, { recursive: true });

const browser = await chromium.launch();
const shots = [
  { name: "desktop", width: 1440, height: 900, scheme: "dark", full: true },
  { name: "desktop-1280", width: 1280, height: 800, scheme: "dark", full: false },
  { name: "desktop-light", width: 1440, height: 900, scheme: "light", full: false },
  { name: "mobile", width: 390, height: 844, scheme: "dark", full: true },
];
for (const s of shots) {
  const page = await browser.newPage({ viewport: { width: s.width, height: s.height }, colorScheme: s.scheme });
  await page.goto(url);
  await page.getByTestId("cluster").waitFor();
  await page.waitForTimeout(2500);
  if (kill && s.name === "desktop") {
    const leader = await page.locator('[data-testid^="node-strip-"][data-state="leader"]').getAttribute("data-node-id");
    await page.getByTestId(`node-${leader}-power`).click();
    await page.waitForTimeout(2500);
    await page.getByTestId("kv-put").click();
    await page.waitForTimeout(800);
  }
  await page.screenshot({ path: `${out}${s.name}.png`, fullPage: s.full });
  await page.close();
}
await browser.close();
console.log("wrote", out);
