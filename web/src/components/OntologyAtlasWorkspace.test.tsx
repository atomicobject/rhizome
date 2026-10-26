import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { OntologySummaryResponse } from "../api/types";
import { jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeLayout } from "../test/fakeLayout";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { OntologyAtlasWorkspace } from "./OntologyAtlasWorkspace";

const http = withFakeFetch();

const summary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 3,
  typedNotes: 3,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 0,
  types: [
    { name: "Plan", label: "Plan", pluralLabel: "Plans", count: 1, role: "note" },
    { name: "Spec", label: "Spec", pluralLabel: "Specs", count: 2, role: "note" },
    { name: "Story", label: "Story", displayParent: "Spec", count: 0, role: "embedded" },
  ],
  interfaces: [
    {
      name: "Document",
      label: "Document",
      pluralLabel: "Documents",
      displayGroup: "Delivery",
      count: 3,
      implementors: ["Plan", "Spec"],
    },
  ],
};

describe("OntologyAtlasWorkspace sidebar", () => {
  withFakeLayout();

  beforeEach(() => {
    window.localStorage.clear();
    window.history.replaceState({}, "", "/ontology");
    http
      .json("GET", "/api/v1/ontology/atlas", { schemaPresent: true, types: [], interfaces: [] })
      .json("GET", "/api/v1/ontology/summary", summary)
      .json("GET", "/api/v2/validate", {
        status: "ok",
        generation: 1,
        result: { ok: true, issueCount: 0, errorCount: 0, durationMs: 1, checks: [] },
      });

    for (const name of ["Document", "Spec", "Story"])
      http.json("GET", `/api/v1/ontology/types/${name}`, {
        count: 0,
        issueCount: 0,
        type: { name, label: name, fields: [] },
        notes: [],
      });
  });

  it("selects Atlas on the atlas home and lists groups with their types and interfaces", async () => {
    render(<OntologyAtlasWorkspace sidebarTitle="testvault" />);
    const nav = await screen.findByRole("complementary", { name: "Ontology navigation" });

    expect(within(nav).getByRole("button", { name: "Atlas" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("heading", { name: "Ontology Atlas" })).toBeVisible();
    expect(screen.getByText("testvault")).toBeVisible();

    fireEvent.click(await within(nav).findByRole("button", { name: "Delivery" }));
    expect(within(nav).getByRole("button", { name: "Documents (3)" })).toBeVisible();
    fireEvent.click(within(nav).getByRole("button", { name: "Expand Documents" }));
    expect(within(nav).getByRole("button", { name: "Specs (2)" })).toBeVisible();
  });

  it("opens a type's detail, marks it current, and returns to the atlas", async () => {
    render(<OntologyAtlasWorkspace />);
    const nav = await screen.findByRole("complementary", { name: "Ontology navigation" });
    fireEvent.click(await within(nav).findByRole("button", { name: "Delivery" }));
    fireEvent.click(within(nav).getByRole("button", { name: "Documents (3)" }));

    expect(window.location.pathname).toBe("/ontology/type/Document");
    expect(await screen.findByRole("heading", { name: "Document", level: 1 })).toBeVisible();
    expect(within(nav).getByRole("button", { name: "Documents (3)" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(within(nav).getByRole("button", { name: "Atlas" })).not.toHaveAttribute("aria-current");

    fireEvent.click(within(nav).getByRole("button", { name: "Atlas" }));
    expect(window.location.pathname).toBe("/ontology");
    expect(await screen.findByRole("heading", { name: "Ontology Atlas" })).toBeVisible();
  });

  it("opens the current type's ancestors and lets embedded types open their detail", async () => {
    window.history.replaceState({}, "", "/ontology/type/Story");
    render(<OntologyAtlasWorkspace />);
    const nav = await screen.findByRole("complementary", { name: "Ontology navigation" });
    const story = await within(nav).findByRole("button", { name: "Story" });

    expect(story).toHaveAttribute("aria-current", "page");
    expect(story).toBeEnabled();
    fireEvent.click(within(nav).getByRole("button", { name: "Specs (2)" }));
    expect(window.location.pathname).toBe("/ontology/type/Spec");
  });

  it("says when the type tree could not load and retries it", async () => {
    let fail = true;
    http.on("GET", "/api/v1/ontology/summary", () =>
      fail ? jsonReply({ error: "unavailable" }, 500) : jsonReply(summary),
    );
    render(<OntologyAtlasWorkspace />);

    const sidebar = await screen.findByRole("complementary", { name: "Ontology navigation" });
    expect(await within(sidebar).findByRole("status")).toHaveTextContent("Types unavailable.");
    fail = false;
    fireEvent.click(within(sidebar).getByRole("button", { name: "Retry" }));
    expect(await within(sidebar).findByRole("button", { name: "Delivery" })).toBeVisible();
    expect(within(sidebar).queryByRole("status")).toBeNull();
  });
});
