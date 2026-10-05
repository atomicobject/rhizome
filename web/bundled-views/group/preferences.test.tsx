import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it } from "vitest";

import { FakeFetch, jsonReply } from "../../src/test/fakeFetch";
import { preferenceServer } from "../../src/viewPreferences/testServer";
import { DISPLAY_GROUPS, NOW, PATHS, RECORDS_DATA } from "./__fixtures__/groups.ts";
import { groupRoutes, renderGroupView } from "./__fixtures__/harness.tsx";
import { isJsonObject } from "./api.ts";
import { withChoice } from "./remembered.ts";

const loadTrace = () => import("./trace.tsx");

const loadBriefing = () => import("./briefing.tsx");

const PLANNING = { kind: "group", group: "Planning" } as const;

const SCOPE = { viewId: "group.test", context: PLANNING };

let http: FakeFetch;

let server: ReturnType<typeof preferenceServer>;

beforeEach(() => {
  http = new FakeFetch().install();
  server = preferenceServer(http);
});

afterEach(() => http.restore());

const checked = (name: RegExp) => screen.getByRole("radio", { name });

it("remembers Trace's row type and row tree for each group", async () => {
  const planning = DISPLAY_GROUPS.groups.find((group) => group.name === "Planning");

  if (!planning) throw new Error("fixture group missing");

  const routes = groupRoutes({
    "GET /api/v1/display-groups": () =>
      jsonReply({ groups: [...DISPLAY_GROUPS.groups, { ...planning, name: "Roadmap" }] }),
  });

  const first = await renderGroupView(loadTrace, { routes, http });
  fireEvent.click(await screen.findByRole("button", { name: "Rows under Commerce" }));
  await waitFor(() => expect(server.read(SCOPE).values).toHaveProperty("trace.rows"));
  fireEvent.click(screen.getByRole("radio", { name: /^Releases/ }));
  await waitFor(() => expect(server.read(SCOPE).values["trace.spine"]).toBe("Release"));
  first.unmount();

  const second = await renderGroupView(loadTrace, { routes, http });
  await waitFor(() => expect(checked(/^Releases/)).toBeChecked());
  fireEvent.click(screen.getByRole("radio", { name: /^Areas/ }));
  // Areas is the default, so choosing it forgets the choice; its row tree stays.
  await waitFor(() => expect(server.read(SCOPE).values).not.toHaveProperty("trace.spine"));
  expect(await screen.findByRole("button", { name: "Rows under Commerce" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  second.unmount();

  const roadmap = await renderGroupView(loadTrace, { group: "Roadmap", routes, http });
  expect(await screen.findByRole("button", { name: "Rows under Commerce" })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  roadmap.unmount();
});

it("keeps a band opened from its default fold", async () => {
  const payments = { path: PATHS.payments, title: "Payments", resolvedType: "Area" };

  const bugs = Array.from({ length: 4 }, (_, index) => ({
    ref: { notePath: `work/bug-${index}.md`, kind: "NOTE", typeName: "Bug" },
    path: `work/bug-${index}.md`,
    title: `Bug ${index}`,
    updatedAt: new Date(NOW - index * 60_000).toISOString(),
    stage: "dropped",
    area: payments,
  }));

  const routes = groupRoutes({
    "POST /api/v1/graphql": () =>
      jsonReply({
        data: {
          ...RECORDS_DATA,
          Bug: [...(Array.isArray(RECORDS_DATA.Bug) ? RECORDS_DATA.Bug : []), ...bugs],
        },
      }),
  });

  const first = await renderGroupView(loadTrace, { routes, http });
  fireEvent.click(await screen.findByRole("radio", { name: /^Work items/ }));
  const dropped = await screen.findByRole("button", { name: /^Dropped/ });
  expect(dropped).toHaveAttribute("aria-expanded", "false");
  fireEvent.click(dropped);
  await waitFor(() => expect(server.read(SCOPE).values).toHaveProperty("trace.bands"));
  first.unmount();

  await renderGroupView(loadTrace, { routes, http });
  expect(await screen.findByRole("button", { name: /^Dropped/ })).toHaveAttribute(
    "aria-expanded",
    "true",
  );
});

it("ignores a remembered row type the group no longer has", async () => {
  server.change(SCOPE, { "trace.spine": "Gone" });
  await renderGroupView(loadTrace, { http });

  await screen.findByRole("table");
  expect(checked(/^Areas/)).toBeChecked();
});

it("keeps a Briefing signal's records revealed after a reload", async () => {
  // Areas without parents link with nothing else, so Not linked has more than it names.
  const areas = (Array.isArray(RECORDS_DATA.Area) ? RECORDS_DATA.Area : []).map((area) =>
    isJsonObject(area) ? { ...area, parent: null } : area,
  );

  const routes = groupRoutes({
    "POST /api/v1/graphql": () => jsonReply({ data: { ...RECORDS_DATA, Area: areas } }),
  });

  const unlinked = async () => {
    const context = await screen.findByRole("region", { name: "Context" });

    const found = [...context.querySelectorAll("li")].find((entry) =>
      entry.textContent?.includes("Not linked"),
    );

    if (!found) throw new Error("no Not linked signal");

    return within(found);
  };

  const first = await renderGroupView(loadBriefing, { routes, http });
  fireEvent.click((await unlinked()).getByRole("button", { name: "+2 more" }));
  await waitFor(() => expect(server.read(SCOPE).values).toHaveProperty("briefing.expanded"));
  first.unmount();

  await renderGroupView(loadBriefing, { routes, http });
  expect(await (await unlinked()).findByRole("button", { name: "Show fewer" })).toBeVisible();
});

it("keeps Trace's remembered rows storable when row keys are long multibyte text", () => {
  // Trace and Briefing keys stay within the server's 64 KiB value and 128 KiB instance limits.
  const longKey = (index: number) => `${"界".repeat(200)}-${index}`;
  let rows: Record<string, Record<string, boolean>> = {};

  for (let spine = 0; spine < 8; spine++)
    for (let index = 0; index < 400; index++)
      rows = withChoice(rows, `Spine ${spine}`, longKey(index), true);

  const next = withChoice(rows, "Spine 0", "newest", false);

  expect(new TextEncoder().encode(JSON.stringify(next)).length * 3).toBeLessThanOrEqual(128 * 1024);
  expect(next["Spine 0"]?.newest).toBe(false);
  expect(next["Spine 7"]?.[longKey(399)]).toBe(true);
  expect(next["Spine 1"]).toBeUndefined();
});
