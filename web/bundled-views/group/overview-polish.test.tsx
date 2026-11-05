import { screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderGroupView, scopeRoutes } from "./__fixtures__/harness.tsx";

const loadOverview = () => import("./overview.tsx");

it("keeps Members with the map independently of the side sections", async () => {
  await renderGroupView(loadOverview, { routes: scopeRoutes() });
  const main = screen.getByRole("region", { name: "Map" }).closest(".gv-ov-main");

  expect(main).toContainElement(screen.getByRole("region", { name: "Members" }));
  expect(main).not.toContainElement(screen.getByRole("region", { name: "Outside the group" }));
});

it("nests a collapsed group's type swatches under its member families", async () => {
  await renderGroupView(loadOverview, { context: { kind: "workspace" }, routes: scopeRoutes() });
  const map = screen.getByRole("region", { name: "Map" });
  const planning = within(map).getByText("Planning", { selector: "dt" }).parentElement;

  expect(planning?.querySelectorAll(".gv-legend-family")).toHaveLength(4);
  expect(planning?.querySelector(".gv-legend-family-name")).toBeVisible();
  expect(map).toHaveTextContent("4 member types");
  expect(map).toHaveTextContent("Plain links");
  expect(map).not.toHaveTextContent("mostly plain links");
});
