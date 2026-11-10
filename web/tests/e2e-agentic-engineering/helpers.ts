import { appendFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { expect, type Page, type TestInfo } from "@playwright/test";

export { customFrame as content, openGroup } from "../e2e/groupViewHelpers";

export const home = "/notes/group/Delivery";

export const stage = (value: string) => `/notes?view=agentic-engineering.stage&stage=${value}`;

export const specPath = "docs/specs/experience/harbor-booking.md";

export const specTitle = "Harbor booking and recovery for passengers with long itineraries";

export function record(note: string, story?: string) {
  const params = new URLSearchParams({ note });

  if (story) params.set("story", story);

  return `/notes?${params}`;
}

export const workspace = (variant: string, minute: string) =>
  `docs/efforts/harbor-${variant}/2026-11-09-11-${minute}-effort.html`;

export const groups = [
  "Shaping specs",
  "Ready stories",
  "Planned efforts",
  "Active efforts",
  "Delivery history",
];

export async function capture(page: Page, info: TestInfo, scenario: string) {
  const file = info.outputPath(`${scenario}.png`);
  await page.screenshot({ path: file, fullPage: true });
  await info.attach(scenario, { path: file, contentType: "image/png" });
  const directory = path.resolve("test-results/agentic-engineering");
  await mkdir(directory, { recursive: true });
  await appendFile(
    path.join(directory, "screenshots.jsonl"),
    JSON.stringify({
      scenario,
      project: info.project.name,
      theme: info.titlePath.find((part) => /^(light|dark)$/.test(part)),
      viewport: page.viewportSize(),
      url: page.url(),
      file,
    }) + "\n",
  );
}

export async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );

  for (const frame of page.frames().filter((entry) => entry.parentFrame())) {
    expect(
      await frame.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
  }
}
