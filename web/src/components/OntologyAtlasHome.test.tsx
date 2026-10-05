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
    expect(await screen.findByRole("button", { name: "Focus Spec" })).toBeVisible();
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

    fireEvent.click(await screen.findByRole("button", { name: "Focus OldType" }));
    rerender(
      <OntologyAtlasHome
        atlas={{
          schemaPresent: true,
          types: [{ type: { name: "NewType", fields: [] }, count: 2 }],
        }}
        error={null}
      />,
    );
    expect(screen.queryByRole("button", { name: "Focus OldType" })).toBeNull();
    expect(await screen.findByRole("button", { name: "Focus NewType" })).toBeVisible();
    expect(screen.getByRole("button", { name: /All types/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(container.querySelector(".atlas-node--dimmed")).toBeNull();
  });
});
