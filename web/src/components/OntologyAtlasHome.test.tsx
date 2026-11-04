import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { withFakeLayout } from "../test/fakeLayout";
import { OntologyAtlasHome } from "./OntologyAtlasHome";

describe("OntologyAtlasHome", () => {
  withFakeLayout();

  it("uses the visualization as the atlas survey surface", async () => {
    const { container } = render(
      <OntologyAtlasHome
        atlas={{
          schemaPresent: true,
          vaultName: "testvault",
          types: [
            {
              type: {
                name: "Spec",
                description: "Governing artifact.",
                fields: [],
              },
              count: 3,
            },
          ],
          interfaces: [{ name: "SpecLike", fields: [] }],
        }}
        error={null}
      />,
    );

    await waitFor(() => expect(container.querySelector(".react-flow")).not.toBeNull(), {
      timeout: 5000,
    });

    expect(screen.getByRole("heading", { name: "Ontology Atlas" })).toBeDefined();
    expect(screen.queryByRole("heading", { name: "Types" })).toBeNull();
    expect(screen.queryByRole("heading", { name: "Interfaces" })).toBeNull();
    // The header counts types and interfaces together, then names each.
    expect(screen.getByText("2")).toHaveClass("view-header__count");
    expect(screen.getByText("1 type and 1 interface · 3 typed notes")).toBeVisible();
    const find = await screen.findByRole("searchbox", { name: "Find a type" });
    fireEvent.change(find, { target: { value: "spec" } });
    expect(await screen.findByRole("button", { name: "Focus Spec" })).toBeVisible();
    // Enter focuses the first match and clears the query.
    fireEvent.keyDown(find, { key: "Enter" });
    expect(await screen.findByText("Tracing")).toHaveTextContent("Tracing Spec");
    expect(find).toHaveValue("");
  });
  it("clears obsolete cards during schema refresh and resets a removed focus", async () => {
    const { rerender, container } = render(
      <OntologyAtlasHome
        atlas={{
          schemaPresent: true,
          types: [{ type: { name: "OldType", fields: [] }, count: 1 }],
        }}
        error={null}
      />,
    );

    fireEvent.change(await screen.findByRole("searchbox", { name: "Find a type" }), {
      target: { value: "type" },
    });
    fireEvent.click(await screen.findByRole("button", { name: "Focus OldType" }));
    expect(screen.getByRole("button", { name: "Show all types" })).toBeVisible();
    rerender(
      <OntologyAtlasHome
        atlas={{
          schemaPresent: true,
          types: [{ type: { name: "NewType", fields: [] }, count: 2 }],
        }}
        error={null}
      />,
    );
    fireEvent.change(screen.getByRole("searchbox", { name: "Find a type" }), {
      target: { value: "type" },
    });
    expect(screen.queryByRole("button", { name: "Focus OldType" })).toBeNull();
    expect(await screen.findByRole("button", { name: "Focus NewType" })).toBeVisible();
    // The removed focus resets, so nothing is traced.
    expect(screen.queryByRole("button", { name: "Show all types" })).toBeNull();
    expect(container.querySelector(".atlas-node--dimmed")).toBeNull();
  });
});
