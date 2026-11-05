import { expect, test } from "./fixtures";
import { chooseView, expectViewSettled } from "./workspaceView";

test("cards keep short and long titles on one line and align grid row heights", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/notes/tv-work-item");
  await chooseView(page, "Cards");
  await expectViewSettled(page);
  const short = page.locator(".configured-card", { hasText: "Anchor chain inspection" });

  const long = page.locator(".configured-card", {
    hasText: "Crane rail alignment along the northern harbor",
  });

  for (const [card, lines] of [
    [short, 1],
    [long, 1],
  ] as const) {
    await expect(card).toBeVisible();

    const layout = await card.evaluate((element) => {
      const title = element.querySelector(".configured-card__title");
      const text = element.querySelector(".configured-card__title-text");
      const meta = element.querySelector(".configured-brief__header");

      if (!title || !text || !meta) throw new Error("Missing card content");

      return {
        titleHeight: title.getBoundingClientRect().height,
        textHeight: text.getBoundingClientRect().height,
        lineHeight: Number.parseFloat(getComputedStyle(title).lineHeight),
        gap: meta.getBoundingClientRect().top - title.getBoundingClientRect().bottom,
      };
    });

    expect(layout.textHeight).toBeCloseTo(layout.lineHeight * lines, 0);
    expect(layout.titleHeight).toBeCloseTo(layout.textHeight, 0);
    expect(layout.gap).toBeLessThanOrEqual(8);
  }

  expect(await short.evaluate((el) => el.getBoundingClientRect().height)).toBeCloseTo(
    await long.evaluate((el) => el.getBoundingClientRect().height),
    0,
  );

  if (process.env.RHIZOME_CARDS_SCREENSHOT) {
    await page.screenshot({ path: process.env.RHIZOME_CARDS_SCREENSHOT, fullPage: true });
  }
});
