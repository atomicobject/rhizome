import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, it } from "vitest";

import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NODE_MESSAGE,
} from "../../src/lib/customViewMessages";
import { jsonReply, type FakeFetchRequest } from "../../src/test/fakeFetch";
import { AGGREGATE, AGGREGATE_PARTS, VALIDATION_ENVELOPE } from "./__fixtures__/aggregate.ts";
import { NOW, TYPE_DOCS } from "./__fixtures__/groups.ts";
import { renderGroupView, scopeRoutes } from "./__fixtures__/harness.tsx";
import type { JsonObject } from "./api.ts";
import {
  MOTION_PER_TYPE,
  activeRecordsQuery,
  motionTypes,
  parseIssueGroups,
  parseRecentNotes,
} from "./workspace-activity.ts";

const loadBriefing = () => import("./workspace-briefing.tsx");

const WORKSPACE = { kind: "workspace" } as const;

const region = (name: string) => screen.getByRole("region", { name });

const ISSUE_GROUPS: JsonObject = {
  generation: 3,
  groups: [
    {
      check: "types",
      code: "type_ambiguous",
      variant: { key: "Meeting|Person", label: "Matches Meeting and Person" },
      issueCount: 3,
      affectedFileCount: 3,
      applicableRepairCount: 0,
    },
    {
      check: "schema",
      code: "missing_required",
      issueCount: 2,
      affectedFileCount: 1,
      applicableRepairCount: 0,
    },
  ],
};

const seconds = (minutesAgo: number) => Math.floor((NOW - minutesAgo * 60_000) / 1000);

const RECENT: JsonObject = {
  count: 49,
  notes: [
    {
      ref: { notePath: "inbox/loose-thought.md", kind: "NOTE" },
      path: "inbox/loose-thought.md",
      title: "Loose thought",
      hasIssues: false,
      updatedAt: seconds(5),
    },
    {
      ref: { notePath: "work/checkout-redesign.md", kind: "NOTE", typeName: "Story" },
      path: "work/checkout-redesign.md",
      title: "Checkout redesign",
      resolvedType: "Story",
      hasIssues: true,
      updatedAt: seconds(30),
    },
    {
      ref: { notePath: "Top.md", kind: "NOTE" },
      path: "Top.md",
      title: "Top",
      resolvedType: "_untyped",
      hasIssues: false,
    },
  ],
};

const record = (type: string, index: number, fields: JsonObject): JsonObject => ({
  ref: { notePath: `${type.toLowerCase()}/${index}.md`, kind: "NOTE", typeName: type },
  path: `${type.toLowerCase()}/${index}.md`,
  title: `${type} ${index}`,
  updatedAt: new Date(NOW - index * 3_600_000).toISOString(),
  issueCount: 0,
  ...fields,
});

/** Active records as the server filters them: six stories (one past the cap) and one release. */
function activeRecords(request: FakeFetchRequest) {
  const query = request.body ?? "";

  if (!query.includes("op: in"))
    return jsonReply({ errors: [{ message: "expected a filtered query" }] });

  return jsonReply({
    data: {
      Story: Array.from({ length: MOTION_PER_TYPE + 1 }, (_, index) =>
        record("Story", index + 1, { stage: "doing" }),
      ),
      Bug: [],
      Release: [record("Release", 1, { releaseStatus: "shipping" })],
    },
  });
}

function routes(overrides = {}) {
  return scopeRoutes({
    "POST /api/v1/validation/groups": () => jsonReply(ISSUE_GROUPS),
    "POST /api/v1/graphql": activeRecords,
    "GET /api/v1/ontology/types/__all__": (request) =>
      request.query.get("limit") === "12"
        ? jsonReply(RECENT)
        : jsonReply({ error: "no limit" }, 400),
    ...overrides,
  });
}

it("decodes issue groups by variant and recent notes, untyped included", () => {
  expect(parseIssueGroups(ISSUE_GROUPS).map((group) => [group.label, group.issueCount])).toEqual([
    ["Matches Meeting and Person", 3],
    ["missing_required", 2],
  ]);
  expect(
    parseRecentNotes(RECENT).map((note) => [note.title, note.type, note.folder, note.updatedAt]),
  ).toEqual([
    ["Loose thought", null, "inbox", seconds(5) * 1000],
    ["Checkout redesign", "Story", "work", seconds(30) * 1000],
    ["Top", null, "", null],
  ]);
});

it("reads only active-stage records, capped per type", () => {
  const types = motionTypes(AGGREGATE.members!, TYPE_DOCS);

  expect(types.map((entry) => [entry.type, entry.field, entry.active])).toEqual([
    ["Story", "stage", ["doing", "blocked"]],
    ["Bug", "stage", ["doing", "blocked"]],
    ["Release", "releaseStatus", ["shipping"]],
  ]);

  const query = activeRecordsQuery(types);
  expect(query).toContain(
    `Story: story(first: ${MOTION_PER_TYPE + 1}, filters: [{ field: "stage", op: in, values: ["doing","blocked"] }]`,
  );
  expect(query).toContain("path title updatedAt issueCount releaseStatus");
  expect(activeRecordsQuery([])).toBe("");
});

it("briefs All notes: issues by variant and work in motion, beside recent changes", async () => {
  const { posted } = await renderGroupView(loadBriefing, {
    context: WORKSPACE,
    routes: routes(),
    embedded: true,
  });

  const attention = region("Needs attention");

  expect(within(attention).getByText("5")).toBeVisible();
  expect([...attention.querySelectorAll(".gv-sig-text")].map((entry) => entry.textContent)).toEqual(
    ["Matches Meeting and Person in 3 files", "missing_required in 1 file"],
  );

  const motion = region("In motion");
  expect([...motion.querySelectorAll("h3")].map((heading) => heading.textContent)).toEqual([
    "Stories 5+",
    "Releases 1",
  ]);
  expect(within(motion).queryByText("Story 6")).toBeNull();

  const recent = region("Recent changes");
  expect(
    [...recent.querySelectorAll("li")].map((entry) => entry.querySelector("span")?.textContent),
  ).toEqual(["Loose thought untyped · inbox/", "Checkout redesign Story · work/", "Top untyped"]);

  fireEvent.click(within(attention).getAllByRole("button", { name: "Open issues" })[0]);
  fireEvent.click(within(motion).getByRole("button", { name: "More in stories" }));
  fireEvent.click(within(recent).getByRole("button", { name: "Loose thought" }));

  expect(posted.filter((message) => message.type.startsWith("rhizome:open-"))).toEqual([
    { type: OPEN_ISSUES_MESSAGE },
    { type: OPEN_COLLECTION_MESSAGE, name: "Story" },
    { type: OPEN_NODE_MESSAGE, ref: { notePath: "inbox/loose-thought.md", kind: "NOTE" } },
  ]);
});

it("fails one block without the others", async () => {
  let fail = true;

  await renderGroupView(loadBriefing, {
    context: WORKSPACE,
    settle: false,
    routes: routes({
      "GET /api/v1/ontology/types/__all__": () =>
        fail ? jsonReply({ error: "down" }, 500) : jsonReply(RECENT),
    }),
  });

  const alert = await within(region("Recent changes")).findByRole("alert");
  expect(alert).toHaveTextContent("Could not load recent changes");
  await waitFor(() => expect(region("In motion").querySelectorAll("h3")).toHaveLength(2));
  expect(within(region("Needs attention")).queryByRole("alert")).toBeNull();

  fail = false;
  fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));

  await waitFor(() =>
    expect(within(region("Recent changes")).getByText("Loose thought")).toBeVisible(),
  );
});

const envelopeAt = (generation: number) =>
  jsonReply({ ...VALIDATION_ENVELOPE, generation: generation + 1, snapshot: { generation } });

const groupsAt = (current: () => number) => (request: FakeFetchRequest) =>
  // SAFETY: the view posts a ValidationIssueGroupRequest.
  (JSON.parse(request.body ?? "{}") as { generation: number }).generation === current()
    ? jsonReply(ISSUE_GROUPS)
    : jsonReply({ error: "validation generation expired" }, 410);

it("reads issues at the published generation and rereads it when a read expires", async () => {
  // The envelope's own generation is a run in progress; only the snapshot's
  // generation is readable. Generation 2 expires as the server publishes 3.
  let published = 2;
  const groups = groupsAt(() => 3);

  const view = await renderGroupView(loadBriefing, {
    context: WORKSPACE,
    settle: false,
    routes: routes({
      "GET /api/v2/validate": () => envelopeAt(published),
      "POST /api/v1/validation/groups": (request: FakeFetchRequest) => {
        published = 3;

        return groups(request);
      },
    }),
  });

  await waitFor(() => expect(within(region("Needs attention")).getByText("5")).toBeVisible());
  expect(view.http.count("POST", "/api/v1/validation/groups")).toBe(2);
  expect(within(region("Needs attention")).queryByRole("alert")).toBeNull();
});

it("shows a Retry when the published generation stays unreadable", async () => {
  let served = 4;

  await renderGroupView(loadBriefing, {
    context: WORKSPACE,
    settle: false,
    routes: routes({
      "GET /api/v2/validate": () => envelopeAt(3),
      "POST /api/v1/validation/groups": groupsAt(() => served),
    }),
  });

  const alert = await within(region("Needs attention")).findByRole("alert");
  expect(alert).toHaveTextContent("Could not load what needs attention");

  served = 3;
  fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));

  await waitFor(() => expect(within(region("Needs attention")).getByText("5")).toBeVisible());
});

it("says when validation has not published results and nothing is in motion", async () => {
  await renderGroupView(loadBriefing, {
    context: WORKSPACE,
    routes: routes({
      "GET /api/v2/validate": () =>
        jsonReply({ status: "running", health: "running", generation: 1, publishedGeneration: 0 }),
      "POST /api/v1/graphql": () => jsonReply({ data: { Story: [], Bug: [], Release: [] } }),
    }),
  });

  expect(
    within(region("Needs attention")).getByText("Validation has not published results yet."),
  ).toBeVisible();
  expect(within(region("In motion")).getByText("Nothing in an active stage.")).toBeVisible();
});

it("retries the type documentation In motion failed to read", async () => {
  let fail = true;

  await renderGroupView(loadBriefing, {
    context: WORKSPACE,
    settle: false,
    routes: routes({
      "GET /api/v1/ontology/types/Release": () =>
        fail ? jsonReply({ error: "down" }, 500) : jsonReply({ type: TYPE_DOCS.Release, count: 0 }),
    }),
  });

  const alert = await within(region("In motion")).findByRole("alert");

  fail = false;
  fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));

  await waitFor(() => expect(region("In motion").querySelectorAll("h3")).toHaveLength(2));
});

it("says In motion waits while the index rebuilds, reading no records", async () => {
  const view = await renderGroupView(loadBriefing, {
    context: WORKSPACE,
    routes: routes({
      "GET /api/v1/ontology/shape": () =>
        jsonReply({ ...AGGREGATE_PARTS.members, rebuilding: true }),
    }),
  });

  expect(within(region("In motion")).getByText(/The index is rebuilding/)).toBeVisible();
  expect(view.http.count("POST", "/api/v1/graphql")).toBe(0);
  expect(view.http.count("GET", "/api/v1/ontology/types/Story")).toBe(0);
});
