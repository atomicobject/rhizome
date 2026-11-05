import { fireEvent, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { fixtureModel, renderGroupView, scopeRoutes } from "./__fixtures__/harness.tsx";

const loadOverview = () => import("./overview.tsx");

it("places group Members across the columns before outside previews and guide", async () => {
  await renderGroupView(loadOverview, { routes: scopeRoutes() });
  const main = screen.getByRole("region", { name: "Map" }).closest(".gv-ov-main");

  const members = screen.getByRole("region", { name: "Members" });
  expect(main).not.toContainElement(members);
  expect(members.closest(".gv-ov-members")?.parentElement).toHaveClass("gv-ov--group");
  const below = screen.getByRole("region", { name: "Guide and views" }).closest(".gv-ov-below");
  expect(below).toContainElement(screen.getByRole("region", { name: "Guide and views" }));
  expect(
    screen.getByRole("region", { name: "Outside the group" }).querySelectorAll("li"),
  ).toHaveLength(3);
  expect(screen.queryByRole("region", { name: "Outside notes" })).toBeNull();
  expect(main).not.toContainElement(screen.getByRole("region", { name: "Outside the group" }));
});

it("nests a collapsed group's type swatches under its member families", async () => {
  await renderGroupView(loadOverview, { context: { kind: "workspace" }, routes: scopeRoutes() });
  const map = screen.getByRole("region", { name: "Map" });
  const planning = within(map).getByText("Planning", { selector: "dt" }).parentElement;

  expect(planning).toHaveTextContent("Story 4");
  expect(planning?.querySelectorAll("dd > span").length).toBeGreaterThan(4);
  expect(map).toHaveTextContent("4 member types");
  expect(map).toHaveTextContent("Plain links");
  expect(map).not.toHaveTextContent("mostly plain links");
});

it("keeps tiny nonempty ring segments at least four pixels while preserving the full circle", async () => {
  const { compositionArcs } = await import("./overview-map-parts.tsx");
  const { AGGREGATE, SCOPE_GROUPS, SUMMARIES } = await import("./__fixtures__/aggregate.ts");
  const { scopeModel } = await import("./scope.ts");

  if (!AGGREGATE.members) throw new Error("missing fixture members");

  const model = scopeModel({
    scope: { kind: "workspace" },
    groups: SCOPE_GROUPS.groups,
    members: AGGREGATE.members,
    summaries: SUMMARIES,
    expanded: new Set(),
  });

  const node = model?.nodes.find((item) => item.kind === "group" && item.types.length > 2);

  if (!node) throw new Error("missing composite fixture");
  const summaries = new Map(SUMMARIES);
  node.types.forEach((name, index) => {
    const summary = summaries.get(name);

    if (summary) summaries.set(name, { ...summary, count: index ? 1 : 10000 });
  });
  const radius = 30;
  const circumference = 2 * Math.PI * (radius - (radius * 0.32) / 2);
  const arcs = compositionArcs(node, summaries, (name) => name, radius);
  expect(arcs.reduce((sum, arc) => sum + arc.share, 0)).toBeCloseTo(1);

  for (const arc of arcs) expect(arc.share * circumference).toBeGreaterThanOrEqual(4 - 1e-9);
  expect(arcs[0].share).toBeGreaterThan(arcs[1].share);
});

it("names line keys separately and reconciles the combined untyped bucket", async () => {
  await renderGroupView(loadOverview, { context: { kind: "workspace" }, routes: scopeRoutes() });
  const members = screen.getByRole("region", { name: "Members" });

  expect(members).toHaveTextContent("29 typed · 17 untyped + 3 ambiguous");
  const row = within(members).getByRole("row", { name: /Untyped \+ ambiguous/ });

  expect(within(row).getAllByRole("cell")[1]).toHaveTextContent("20");

  const lines = within(screen.getByRole("region", { name: "Map" })).getByText("Lines", {
    selector: "dt",
  }).parentElement;

  expect(lines).toHaveTextContent("Plain links");
  expect(lines).not.toHaveTextContent("Untyped");
});

it("names a zero-row disclosure and puts its caveat in heading help", async () => {
  const model = fixtureModel("Planning");

  const notes = [
    { key: "outside", path: "outside.md", title: "Outside", type: null, touches: ["record"] },
  ];

  await renderGroupView(async () => {
    const { OutsideNotes } = await import("./overview-side-parts.tsx");

    return {
      default: () => (
        <OutsideNotes
          model={model}
          outside={{ notes, byType: [], incomplete: true }}
          title="Outside the group"
          info="Linked notes"
          empty="None"
          limit={0}
        />
      ),
    };
  });
  const outside = screen.getByRole("region", { name: "Outside the group" });

  expect(outside.querySelectorAll("li")).toHaveLength(0);
  expect(within(outside).getByRole("heading")).toHaveAttribute(
    "title",
    expect.stringContaining("More notes may link"),
  );
  expect(within(outside).getByRole("button", { name: "Search all notes" })).toBeVisible();
  fireEvent.click(within(outside).getByRole("button", { name: "Show 1 note" }));
  expect(outside.querySelectorAll("li")).toHaveLength(1);
  fireEvent.click(within(outside).getByRole("button", { name: "Show fewer" }));
  expect(outside.querySelectorAll("li")).toHaveLength(0);
});
