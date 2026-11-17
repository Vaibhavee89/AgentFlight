import { expect, test } from "@playwright/test";

// These run against the second server, which has an API token set.
const BASE = "http://127.0.0.1:18081";
const TOKEN = "e2e-token-0123456789abcdef";

test.use({ baseURL: BASE });

test("the API refuses a request with no credentials, and the health probe stays open", async ({
  request,
}) => {
  expect((await request.get("/runs")).status()).toBe(401);
  expect((await request.post("/runs", { data: { prompt: "x" } })).status()).toBe(401);
  expect((await request.get("/healthz")).status()).toBe(200);

  const authed = await request.get("/runs", { headers: { Authorization: `Bearer ${TOKEN}` } });
  expect(authed.status()).toBe(200);
});

test("sign in, watch a run stream live, sign out", async ({ page }) => {
  await page.goto("/ui/runs");

  // Signed out: only the login form, no run data.
  await expect(page.getByLabel("API token")).toBeVisible();
  await expect(page.getByRole("heading", { level: 1, name: "Runs" })).toHaveCount(0);

  // A wrong token is refused and the form stays.
  await page.getByLabel("API token").fill("not-the-right-token-at-all");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert")).toHaveText("invalid token");

  // The right token opens the dashboard.
  await page.getByLabel("API token").fill(TOKEN);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Runs" })).toBeVisible();

  // The session cookie is out of reach of page scripts.
  expect(await page.evaluate(() => document.cookie)).toBe("");

  // A run started in the browser streams over EventSource, which can only
  // authenticate with the cookie.
  await page.getByRole("button", { name: "New run" }).click();
  await page.getByLabel("Prompt").fill("auth e2e: summarise the disk report");
  await page.getByRole("button", { name: "Start run" }).click();
  await expect(page).toHaveURL(/\/ui\/runs\/[0-9a-f]+$/);
  await expect(page.locator(".run-title .badge")).toHaveText("completed", { timeout: 30_000 });
  const timeline = page.getByRole("region", { name: "Event timeline" });
  await expect(timeline.getByText("finished").first()).toBeVisible();

  // The session survives a reload.
  await page.reload();
  await expect(page.locator(".run-title .badge")).toHaveText("completed");

  // Signing out brings the login form back and the data is out of reach again.
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByLabel("API token")).toBeVisible();
  const status = await page.evaluate(async () => (await fetch("/runs")).status);
  expect(status).toBe(401);
});
