import { expect, test } from "./fixtures";

test("runs an approval and interrupt journey against the fake harness", async ({
  page,
}, testInfo) => {
  // A short desktop viewport makes the thread body the scroll container.
  await page.setViewportSize({ width: 1280, height: 560 });
  await page.goto("/agent");
  await expect(page.locator(".agent-thread__head")).toContainText("e2e-fake");

  // Nothing is chosen, so chat uses the first ready agent: the fake Claude Code.
  const picker = page.getByRole("button", { name: "Fake Opus · Default" });
  await picker.click();
  const dialog = page.getByRole("dialog", { name: "Choose a model" });
  await expect(dialog.getByRole("group")).toHaveText([/^Claude Code/, /^Codex· signed out/]);
  expect((await dialog.boundingBox())?.width).toBe(430);

  // Choosing the signed-out agent's model explains it in the conversation and holds Send.
  await page.keyboard.press("ControlOrMeta+3");
  await expect(dialog).toBeHidden();
  await expect(page.getByText(/Codex is signed out/)).toHaveText(
    "Codex is signed out. Run codex login in Terminal.",
  );
  await page.getByLabel("Message").fill("draft kept");
  await expect(page.getByRole("button", { name: "Send" })).toBeDisabled();
  await page.getByRole("button", { name: "Use Fake Opus instead" }).click();
  await expect(page.getByText(/signed out/)).toBeHidden();
  await expect(page.getByLabel("Message")).toHaveValue("draft kept");
  await page.getByLabel("Message").fill("");

  // ⇧⌘E opens on Reasoning; the choice saves for the next chats.
  await page.keyboard.press("ControlOrMeta+Shift+E");
  const reasoning = page.getByRole("radiogroup", { name: "Reasoning" });
  await expect(reasoning.getByRole("radio", { name: "Default" })).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("button", { name: "Fake Opus · High" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();

  await page.keyboard.press("ControlOrMeta+Shift+A");
  const modes = page.getByRole("menu", { name: "Permission mode" });
  await expect(modes.locator(".chat-mode-menu__label")).toHaveText(["Ask first", "Auto"]);
  await page.keyboard.press("Escape");
  await expect(modes).toBeHidden();
  await page.reload();
  await expect(page.getByRole("button", { name: "Fake Opus · High" })).toBeVisible();

  await expect(page.getByText("No sessions yet.", { exact: true })).toHaveCSS("font-size", "13px");
  const rows = page.locator(".agent-examples button");
  await expect(rows).toHaveCount(3);

  // The arrow follows its question by 8px, so it reads as the row's action.
  const gaps = await page.locator(".agent-examples button").evaluateAll((buttons) =>
    buttons.map((button) => {
      const text = button.querySelector("span")!.getBoundingClientRect();

      const arrow = button.querySelector(".agent-examples__arrow")!.getBoundingClientRect();

      return Math.round(arrow.left - text.right);
    }),
  );

  expect(gaps).toEqual([8, 8, 8]);

  await rows.first().focus();
  await page.keyboard.press("Enter");
  await expect(page.getByLabel("Message")).toHaveValue(
    "What changed in this repository this week?",
  );
  await expect(page.getByLabel("Message")).toBeFocused();

  await page.getByRole("button", { name: "New session", exact: true }).click();
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
