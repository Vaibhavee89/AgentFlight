import { expect, test } from "@playwright/test";

test("a live run does not shift the layout as events arrive", async ({ page, request }) => {
  // Start the run through the API so the page opens while it is still running.
  const started = await request.post("/runs", { data: { prompt: "live layout check" } });
  expect(started.ok()).toBe(true);
  const { id } = (await started.json()) as { id: string };

  // Record layout shifts that happen without user input.
  await page.addInitScript(() => {
    (window as unknown as { __cls: number }).__cls = 0;
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries() as (PerformanceEntry & {
        value: number;
        hadRecentInput: boolean;
      })[]) {
        if (!entry.hadRecentInput) (window as unknown as { __cls: number }).__cls += entry.value;
      }
    }).observe({ type: "layout-shift", buffered: true });
  });

  await page.goto(`/ui/runs/${id}`);
  const timeline = page.getByRole("region", { name: "Event timeline" });
  await expect(timeline.getByText("model request").first()).toBeVisible();
  await expect(page.locator(".run-title .badge")).toHaveText("completed", { timeout: 30_000 });

  const cls = await page.evaluate(() => (window as unknown as { __cls: number }).__cls);
  // Live updates append at the bottom and reserve their space, so nothing above
  // them should move. The threshold is the "good" bound for web vitals.
  expect(cls).toBeLessThan(0.1);
});

test("arrow keys move focus between timeline events", async ({ page, request }) => {
  const started = await request.post("/runs", { data: { prompt: "keyboard check" } });
  const { id } = (await started.json()) as { id: string };
  await page.goto(`/ui/runs/${id}`);
  await expect(page.locator(".run-title .badge")).toHaveText("completed", { timeout: 30_000 });

  const heads = page.locator(".event-head");
  await expect(heads.first()).toBeVisible();
  await heads.first().focus();
  await page.keyboard.press("ArrowDown");
  await expect(heads.nth(1)).toBeFocused();
  await page.keyboard.press("End");
  await expect(heads.last()).toBeFocused();
  await page.keyboard.press("Home");
  await expect(heads.first()).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(heads.first()).toHaveAttribute("aria-expanded", "true");
});
