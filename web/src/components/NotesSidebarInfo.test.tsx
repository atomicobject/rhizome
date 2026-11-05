import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NotesSidebarInfo } from "./NotesSidebarInfo";
import {
  workspaceWithEmbeddedRelation,
  workspaceWithNavigation,
} from "./body/noteInfoFixtures.test-support";

function groupSection(title: string): HTMLElement {
  const section = screen
    .getByRole("heading", {
      name: (name) => name.replace(/\d+\s*(materials?)?$/, "").trim() === title,
    })
    .closest<HTMLElement>("section, details");

  if (!section) throw new Error(`no sidebar section rendered for ${title}`);

  return section;
}

describe("NotesSidebarInfo", () => {
  it("preserves the collection record template by default", () => {
    render(<NotesSidebarInfo workspace={workspaceWithEmbeddedRelation()} onOpen={vi.fn()} />);
    const relations = groupSection("Relations");
    expect(within(relations).getByRole("heading", { name: "Outgoing" })).toBeVisible();
    expect(within(relations).queryByRole("img", { name: "Links to" })).toBeNull();
  });

  it("keeps embedded relation targets with related notes and opens them at their anchor", () => {
    const onOpen = vi.fn();
    render(
      <NotesSidebarInfo noteReader workspace={workspaceWithEmbeddedRelation()} onOpen={onOpen} />,
    );

    const link = within(groupSection("Relations")).getByRole("button", {
      name: "Decide rain-date contingency by end of May",
    });

    expect(screen.queryByRole("heading", { name: "Code" })).toBeNull();

    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith("docs/playground/pizza-party-2026.md#item-1030", "stack");
  });

  it("shows each field relation direction and merges body links and backlinks per note", () => {
    const workspace = workspaceWithEmbeddedRelation();
    workspace.relations = [
      {
        key: "structural",
        label: "Structural relations",
        items: [
          {
            path: "ideas/a.md",
            title: "Idea A",
            kind: "note",
            resolvedType: "Idea",
            relationName: "opportunities",
            provenance: "field",
            direction: "incoming",
            structural: true,
          },
          {
            path: "impacts/b.md",
            title: "Impact B",
            kind: "note",
            resolvedType: "Impact",
            relationName: "impacts",
            provenance: "field",
            direction: "outgoing",
            structural: true,
          },
        ],
      },
      {
        key: "ambient",
        label: "Ambient relations",
        items: [
          { path: "ideas/a.md", title: "Idea A", kind: "note", relationName: "related" },
          {
            path: "people/c.md",
            title: "Person C",
            kind: "note",
            resolvedType: "Person",
            relationName: "related",
            provenance: "body_link",
            direction: "outgoing",
          },
          {
            path: "people/c.md",
            title: "Person C",
            kind: "note",
            resolvedType: "Person",
            relationName: "related",
            provenance: "backlink",
            direction: "incoming",
          },
        ],
      },
      {
        key: "backlinks",
        label: "Backlinks",
        items: [
          { path: "Log/2026-10-01.md", title: "2026-10-01", kind: "note", direction: "incoming" },
          {
            path: "Log/2026-10-02.md",
            title: "2026-10-02",
            kind: "note",
            provenance: "backlink",
            direction: "outgoing",
          },
        ],
      },
    ];
    render(<NotesSidebarInfo noteReader workspace={workspace} onOpen={vi.fn()} />);

    const relations = groupSection("Relations");
    expect(relations).toHaveTextContent(/Opportunities\s*1\s*←\s*Idea A\s*Idea/);
    expect(relations).toHaveTextContent(/Impacts\s*1\s*→\s*Impact B\s*Impact/);

    const linked = groupSection("Linked notes");
    expect(within(linked).queryByRole("button", { name: "Idea A" })).toBeNull();
    // An explicit direction wins over the legacy backlink-provenance fallback.
    expect(within(linked).getByRole("img", { name: "Links to" })).toBeTruthy();
    expect(within(linked).getAllByRole("button", { name: "Person C" })).toHaveLength(1);
    expect(within(linked).getByRole("img", { name: "Linked both ways" })).toBeTruthy();
    expect(linked).toHaveTextContent("Person");
    expect(linked).toHaveTextContent("Log/");
  });

  it("collapses long buckets behind a toggle", () => {
    const workspace = workspaceWithEmbeddedRelation();
    workspace.relations = [
      {
        key: "backlinks",
        label: "Backlinks",
        items: Array.from({ length: 8 }, (_, index) => ({
          path: `Log/day-${index}.md`,
          title: `Day ${index}`,
          kind: "note",
          direction: "incoming" as const,
        })),
      },
    ];
    render(<NotesSidebarInfo noteReader workspace={workspace} onOpen={vi.fn()} />);

    const linked = groupSection("Linked notes");
    expect(within(linked).getAllByRole("listitem")).toHaveLength(5);
    fireEvent.click(within(linked).getByRole("button", { name: "3 more" }));
    expect(within(linked).getAllByRole("listitem")).toHaveLength(8);
  });

  it("renders schema-declared workspace navigation separately and marks the current member", () => {
    const onOpen = vi.fn();
    render(<NotesSidebarInfo noteReader workspace={workspaceWithNavigation()} onOpen={onOpen} />);

    // An effort's materials start open.
    const navigation = groupSection("Effort materials");
    expect(navigation).toHaveAttribute("open");
    expect(navigation).toHaveTextContent("Schema-driven navigation");
    expect(within(navigation).getByRole("button", { name: "Implementation plan" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(within(navigation).getByRole("button", { name: "Implementation plan" })).toHaveAttribute(
      "title",
      "Plan details",
    );

    const related = groupSection("Relations");
    expect(
      within(related).getByRole("button", { name: "Governing product behavior" }),
    ).toBeTruthy();
    expect(related).toHaveTextContent("Governing specs");
    expect(within(related).queryByRole("button", { name: "Implementation plan" })).toBeNull();

    fireEvent.click(within(navigation).getByRole("button", { name: "Work log" }));
    expect(onOpen).toHaveBeenCalledWith("docs/efforts/example/work-log.md", "stack");
  });

  it("keeps an HTML page's effort materials closed, since the page links them", () => {
    const workspace = workspaceWithNavigation();
    workspace.content.format = "html";
    render(<NotesSidebarInfo noteReader workspace={workspace} onOpen={vi.fn()} />);

    const navigation = groupSection("Effort materials");
    expect(navigation).not.toHaveAttribute("open");
    expect(navigation.querySelector("summary")).toHaveTextContent(/^Effort materials\s*3$/);
    const heading = navigation.querySelector("summary h3");
    expect(heading?.firstChild?.textContent).toBe("Effort materials");
    expect(heading?.querySelector(".disclosure-toggle")?.nextElementSibling).toHaveClass(
      "ontology-sidebar-context__count",
    );
    expect(groupSection("Relations")).toBeVisible();
  });
});
