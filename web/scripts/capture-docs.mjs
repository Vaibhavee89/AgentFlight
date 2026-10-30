// Captures the README screenshots and the streaming GIF from a running demo
// server (`make demo`). Run from web/: node scripts/capture-docs.mjs
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { chromium } from "@playwright/test";

const BASE = process.env.DEMO_BASE ?? "http://127.0.0.1:8080";
const OUT = path.resolve("..", "docs", "screenshots");
mkdirSync(OUT, { recursive: true });

async function waitForServer() {
  for (let i = 0; i < 120; i++) {
    try {
      const res = await fetch(`${BASE}/healthz`);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`no server at ${BASE}; run make demo first`);
}

async function getJSON(p, init) {
  const res = await fetch(`${BASE}${p}`, init);
  if (!res.ok) throw new Error(`${init?.method ?? "GET"} ${p} -> ${res.status}`);
  return res.json();
}

async function main() {
  await waitForServer();
  // make demo seeds its runs after the server answers, so wait for them too.
  let runs = [];
  for (let i = 0; i < 120 && runs.length === 0; i++) {
    runs = await getJSON("/runs?limit=50&status=completed");
    if (runs.length === 0) await new Promise((r) => setTimeout(r, 500));
  }
  const withRetries = runs.find((r) => r.retries > 0) ?? runs[0];
  if (!withRetries) throw new Error("no completed runs; run make demo first");

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1200, height: 760 },
    deviceScaleFactor: 1,
  });
  const page = await context.newPage();

  await page.goto(`${BASE}/ui/runs`);
  await page.getByRole("heading", { name: "Runs" }).waitFor();
  await page.waitForTimeout(400);
  await page.screenshot({ path: path.join(OUT, "runs-list.png") });

  await page.goto(`${BASE}/ui/runs/${withRetries.id}`);
  const retry = page.locator(".event-retry .event-head").first();
  await retry.waitFor();
  await retry.click();
  await page.waitForTimeout(300);
  await page.screenshot({ path: path.join(OUT, "timeline-retry.png") });

  await page.goto(`${BASE}/ui/benchmarks`);
  await page.locator("figure.chart svg").first().waitFor();
  // recharts animates series in on mount; wait for the animation to finish.
  await page.waitForTimeout(2500);
  await page.screenshot({ path: path.join(OUT, "benchmarks.png"), fullPage: true });

  // Stream a fresh run: frames of the timeline filling in while it executes.
  const started = await getJSON("/runs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ prompt: "stream this run into the timeline" }),
  });
  await page.goto(`${BASE}/ui/runs/${started.id}`);
  const framesDir = mkdtempSync(path.join(tmpdir(), "af-frames-"));
  let frame = 0;
  for (let i = 0; i < 80; i++) {
    await page.screenshot({
      path: path.join(framesDir, `frame-${String(frame).padStart(3, "0")}.png`),
    });
    frame += 1;
    const status = await page.locator(".run-title .badge").textContent();
    if (status === "completed" && i > 3) break;
    await page.waitForTimeout(250);
  }
  // Hold on the finished state so the GIF ends on it.
  for (let i = 0; i < 4; i++) {
    await page.screenshot({
      path: path.join(framesDir, `frame-${String(frame).padStart(3, "0")}.png`),
    });
    frame += 1;
  }
  await browser.close();

  execFileSync(
    "python3",
    [
      path.resolve("..", "scripts", "frames_to_gif.py"),
      framesDir,
      path.join(OUT, "run-streaming.gif"),
      "300",
      "860",
    ],
    { stdio: "inherit" },
  );
  rmSync(framesDir, { recursive: true, force: true });
  writeFileSync(
    path.join(OUT, "README.txt"),
    "Captured by web/scripts/capture-docs.mjs against make demo.\n",
  );
  console.log(`captured ${String(frame)} frames and 3 screenshots into ${OUT}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
