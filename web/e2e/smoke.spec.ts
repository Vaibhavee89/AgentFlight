import { expect, test } from "@playwright/test";

test("create a run, watch it finish, and verify its replay", async ({ page }) => {
  await page.goto("/ui/runs");
  await expect(page.getByRole("heading", { level: 1, name: "Runs" })).toBeVisible();

  // Start a run from the dialog. It should open the run's detail page.
  await page.getByRole("button", { name: "New run" }).click();
  await page.getByLabel("Prompt").fill("smoke test: summarise the disk report");
  await page.getByRole("button", { name: "Start run" }).click();
  await expect(page).toHaveURL(/\/ui\/runs\/[0-9a-f]+$/);
  const runUrl = page.url();

  // The run finishes; its status badge in the header shows it.
  const header = page.locator(".run-title");
  await expect(header.locator(".badge")).toHaveText("completed", { timeout: 30_000 });

  // The timeline shows the recorded events in order.
  const timeline = page.getByRole("region", { name: "Event timeline" });
  await expect(timeline.getByText("model request").first()).toBeVisible();
  await expect(timeline.getByText("tool call").first()).toBeVisible();
  await expect(timeline.getByText("finished").first()).toBeVisible();

  // Verify the replay: the recording reproduces, so the panel reads OK.
  await page.getByRole("button", { name: "Verify replay" }).click();
  await expect(page.getByText("Replay verified.")).toBeVisible({ timeout: 15_000 });

  // The run is listed on the runs page too.
  await page.goto("/ui/runs");
  await expect(
    page.getByRole("link", { name: new URL(runUrl).pathname.split("/").pop() ?? "" }),
  ).toBeVisible();
});
