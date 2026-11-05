import type { Route } from "@playwright/test";
import type { TaskRunDetail, TaskRunView, TasksLiveState } from "../../src/api/tasks";
import {
  localDraftsReportFixture,
  SOURCE_CHECKOUT,
  stateFixture,
} from "../../src/components/tasks/taskFixtures";
import { expect, test, type Page } from "./fixtures";

// The fixture vault is not a Git repository with tasks, so the Tasks routes
// are served from the synthetic task fixtures, as the runtime would serve a
// feature checkout holding a local-only draft.

const TEST_ID = "20261031T170001Z-7e570001";

async function serveTasks(page: Page) {
  const now = Date.now();
  let launchedAt = "";
  let finished = false;

  const testRun = (): TaskRunView => ({
    id: TEST_ID,
    task: "session-mining",
    test: true,
    sourceCheckout: SOURCE_CHECKOUT,
    startedAt: launchedAt,
    at: finished ? launchedAt : "",
    outcome: finished ? "not-needed" : "",
    stage: finished ? undefined : "check",
    commit: "8c41f0e2b7d9a3c1e5f6",
    hasTranscript: false,
    hasCheckLog: true,
  });

  const state = (): TasksLiveState => {
    const base = stateFixture(now);

    if (!launchedAt) return base;

    // The test finishes on the read after it first shows as running.
    if (!finished) {
      finished = true;

      return { ...base, running: testRun() };
    }

    return { ...base, recent: [testRun(), ...base.recent] };
  };

  await page.route(
    (url) => url.pathname.startsWith("/api/tasks"),
    (route: Route) => {
      const request = route.request();
      const { pathname, searchParams } = new URL(request.url());

      if (request.method() === "POST" && pathname === "/api/tasks/tests/session-mining") {
        launchedAt = new Date().toISOString();

        return route.fulfill({ json: { launchedAt, sourceCheckout: SOURCE_CHECKOUT } });
      }

      if (pathname === "/api/tasks") return route.fulfill({ json: localDraftsReportFixture(now) });

      if (pathname === "/api/tasks/state") return route.fulfill({ json: state() });

      if (pathname === "/api/tasks/runs") {
        const runs = launchedAt && searchParams.get("task") === "session-mining" ? [testRun()] : [];

        return route.fulfill({ json: { runs } });
      }

      if (pathname === `/api/tasks/runs/${TEST_ID}`) {
        const detail: TaskRunDetail = {
          run: testRun(),
          events: [],
          next: 0,
          checkLog: "no sessions to mine",
        };

        return route.fulfill({ json: detail });
      }

      return route.fulfill({ status: 404, json: { error: `no fixture for ${pathname}` } });
    },
  );
}

test("tests a local-only task draft and reads its result", async ({ page }) => {
  await serveTasks(page);
  await page.goto("/tasks");
  await expect(page.getByRole("columnheader", { name: "Action", exact: true })).toBeVisible();
  const panel = await page.locator(".tasks-overview").boundingBox();
  const pause = await page.getByRole("button", { name: "Pause all" }).boundingBox();
  const table = await page.locator(".task-table").boundingBox();

  expect(panel && pause && panel.x + panel.width - pause.x - pause.width).toBe(22);
  expect(panel && table && panel.x + panel.width - table.x - table.width).toBe(22);

  const row = page.getByRole("row", { name: /Session mining/ });
  await expect(row.getByText("Local only")).toBeVisible();
  await expect(row.getByText("Not scheduled")).toBeVisible();
  await expect(
    page.getByRole("row", { name: /Docs drift check/ }).getByText("Local changes"),
  ).toBeVisible();

  await row.getByRole("button", { name: /Session mining/ }).click();
  await expect(page).toHaveURL(/\/tasks\/session-mining$/);
  await expect(page.getByRole("group", { name: "Mode" })).toHaveCount(0);

  const strip = page.getByRole("region", { name: "Local version" });
  await expect(strip.getByText(".rhizome/tasks/session-mining.yaml")).toBeVisible();
  await strip.getByRole("button", { name: "Test once" }).click();

  await expect(page).toHaveURL(new RegExp(`/tasks/session-mining/runs/${TEST_ID}$`));
  const run = page.getByRole("article", { name: `Run ${TEST_ID}` });
  await expect(run.getByText("Not needed", { exact: true })).toBeVisible();
  await expect(run.getByText("Test", { exact: true })).toBeVisible();
  await expect(run.getByText("Snapshot")).toBeVisible();
  await expect(run.getByText(SOURCE_CHECKOUT)).toBeVisible();

  const history = page.getByRole("region", { name: "Run history" });
  await expect(history.getByText("Test", { exact: true })).toBeVisible();
  await expect(strip.getByRole("button", { name: "Test once" })).toBeEnabled();
});
