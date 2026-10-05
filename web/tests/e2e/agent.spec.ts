import { expect, test } from "./fixtures";

test("runs an approval and interrupt journey against the fake harness", async ({
  page,
}, testInfo) => {
  // A short desktop viewport makes the thread body the scroll container.
  await page.setViewportSize({ width: 1280, height: 560 });
  await page.goto("/agent");
  await expect(page.locator(".agent-thread__head")).toContainText("e2e-fake");

  await page.getByRole("button", { name: "New", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Rhizome agent chat" })).toBeVisible();

  await page.getByLabel("Message").fill("ping");
  await page.getByRole("button", { name: "Send" }).click();
  const firstApproval = page.getByRole("region", { name: "Approval requested" }).last();
  await expect(firstApproval).toContainText("printf pong");
  await expect(firstApproval).toContainText("fake harness needs approval");
  await firstApproval.getByRole("button", { name: "Allow", exact: true }).click();

  // Replay can show multiple replies; this journey checks completed reply persistence.
  const completedReply = page
    .locator(".agent-message--assistant:not(.is-working)")
    .getByRole("paragraph")
    .filter({ hasText: /^pong$/ })
    .first();

  await expect(completedReply).toBeVisible();
  await expect(page.getByText("Turn completed").last()).toBeVisible();
  await expect(page.getByLabel("Message")).toBeEnabled();

  // Applied layout, not stylesheet spelling: timeline rows keep their height
  // in the scrolling thread and event disclosures keep a 44px hit area.
  const thread = page.locator(".agent-thread__body");

  expect(
    await thread.evaluate((body) => body.scrollHeight > body.clientHeight),
    "thread content should overflow the short viewport",
  ).toBe(true);

  for (const selector of [".agent-message", ".agent-inline-event"]) {
    const flexShrink = await page
      .locator(selector)
      .evaluateAll((rows) => rows.map((row) => getComputedStyle(row).flexShrink));

    expect(flexShrink.length).toBeGreaterThan(0);
    expect(new Set(flexShrink)).toEqual(new Set(["0"]));
  }

  const summaryHeights = await page
    .locator(".agent-inline-event summary")
    .evaluateAll((summaries) => summaries.map((summary) => summary.getBoundingClientRect().height));

  expect(summaryHeights.length).toBeGreaterThan(0);

  for (const height of summaryHeights) expect(height).toBeGreaterThanOrEqual(44);

  await page.reload();
  await expect(completedReply).toBeVisible();
  await expect(firstApproval).toContainText("Decision: allow");

  await page.getByLabel("Message").fill("interrupt this turn");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByRole("button", { name: "Interrupt" })).toBeVisible();
  await page.getByRole("button", { name: "Interrupt" }).click();
  await expect(page.getByText("Interrupt requested")).toBeVisible();
  await expect(page.getByLabel("Message")).toBeEnabled();

  await page.screenshot({ path: testInfo.outputPath("agent-baseline.png"), fullPage: true });
});
