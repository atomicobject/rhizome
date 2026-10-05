import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologyEditSessionResponse, ViewExecuteResponse } from "../api/types";
import { ConfiguredViewCards } from "./ConfiguredViewCards";
import { idea, ideaBoard } from "./ConfiguredView.profileFixtures";
import { rendererProps } from "./ConfiguredView.testFixtures";

function ideaBriefs(): ViewExecuteResponse {
  const alpha = idea("Alpha", "captured", { opportunities: ["Agents"], priority: "high" });
  const bravo = idea("Bravo", "pursuing", { opportunities: ["Agents", "Products"] });
  const opportunity = ideaBoard([bravo, alpha]);

  // Bravo's reverse field arrives as titled targets; Alpha's only as a count.
  bravo.fields = { ...bravo.fields, ideas: 6 };
  bravo.relationValues = {
    ...bravo.relationValues,
    ideas: ["One", "Two", "Three", "Four", "Five", "Six"].map((title) => ({
      value: `[[ideas/${title}.md|${title}]]`,
      ref: { notePath: `ideas/${title}.md`, kind: "NOTE" as const },
      title,
    })),
  };

  return {
    ...opportunity,
    variant: "card",
    board: undefined,
    groups: [
      {
        field: "status",
        key: "status=pursuing",
        label: "Pursuing",
        value: "pursuing",
        tone: "progress",
        count: 1,
        depth: 0,
        rowStart: 0,
        rowEnd: 1,
      },
      {
        field: "status",
        key: "status=captured",
        label: "Captured",
        value: "captured",
        tone: "neutral",
        count: 1,
        depth: 0,
        rowStart: 1,
        rowEnd: 2,
      },
    ],
  };
}

function brief(title: string) {
  const card = screen.getByRole("button", { name: title }).closest("article");

  if (!card) throw new Error(`No brief for ${title}`);

  return card;
}

describe("ConfiguredViewCards record briefs", () => {
  it("groups briefs into lifecycle sections in the generated order", () => {
    const { container } = render(
      <ConfiguredViewCards {...rendererProps({ execution: ideaBriefs() })} />,
    );

    const sections = [...container.querySelectorAll(".configured-view__card-section")];
    expect(sections.map((section) => section.querySelector("h3")?.textContent)).toEqual([
      expect.stringContaining("Pursuing"),
      expect.stringContaining("Captured"),
    ]);
    expect(sections[0]).toContainElement(brief("Bravo"));
    expect(brief("Bravo")).toHaveClass("configured-card--brief");
  });

  it("shows the type, tags, people, Changed, summary, callouts, chips, and reverse lists", () => {
    render(<ConfiguredViewCards {...rendererProps({ execution: ideaBriefs() })} />);
    const bravo = brief("Bravo");

    expect(within(bravo).getByText("IP idea")).toBeVisible();
    expect(within(bravo).getByText("Pursuing")).toBeVisible();
    expect(within(bravo).getByText("Low")).toBeVisible();
    expect(within(bravo).getByTitle("Drew Colthorp")).toHaveTextContent("DC");
    expect(within(bravo).getByText("3d")).toBeVisible();
    expect(bravo.querySelector(".configured-brief__summary--brief")).toHaveTextContent(
      "Bravo summary",
    );
    expect(bravo.querySelector(".configured-brief__callout")).toHaveTextContent(
      "Next stepBravo next step",
    );

    const links = bravo.querySelector<HTMLElement>(".configured-brief__links");

    if (!links) throw new Error("Bravo has no link list");
    expect(within(links).getByText("Opportunities")).toBeVisible();
    expect(within(links).getByRole("link", { name: "Products" })).toBeVisible();
    // A reverse field lists four records and counts the rest.
    expect(
      within(links)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["One", "Two", "Three", "Four", "+2 more"]);
    // A reverse field known only by count reads as a labeled count.
    expect(brief("Alpha")).toHaveTextContent("3 ideas");
  });

  it("counts a reverse field from its total when the listed targets stop at the server's cap", () => {
    const result = ideaBriefs();
    const bravo = result.rows[0]!;

    const listed = ["A", "B", "C", "D", "E", "F", "G", "H"].map((title) => ({
      value: `[[ideas/${title}.md|${title}]]`,
      ref: { notePath: `ideas/${title}.md`, kind: "NOTE" as const },
      title,
    }));

    bravo.fields = { ...bravo.fields, ideas: 12 };
    bravo.relationValues = { ...bravo.relationValues, ideas: listed };

    render(<ConfiguredViewCards {...rendererProps({ execution: result })} />);
    const links = brief("Bravo").querySelector<HTMLElement>(".configured-brief__links")!;

    expect(within(links).getAllByRole("listitem").at(-1)).toHaveTextContent("+8 more");
  });

  it("marks linked and reverse records with their lifecycle status", () => {
    const result = ideaBriefs();
    const bravo = result.rows[0]!;
    const done = { value: "done", label: "Done", tone: "success", stage: "done" as const };
    const active = { value: "active", label: "Active", tone: "progress", stage: "active" as const };

    bravo.relationValues = {
      ...bravo.relationValues,
      opportunities: bravo.relationValues!.opportunities!.map((value) => ({
        ...value,
        status: active,
      })),
      ideas: bravo.relationValues!.ideas!.map((value, index) =>
        index === 0 ? { ...value, status: done } : value,
      ),
    };

    render(<ConfiguredViewCards {...rendererProps({ execution: result })} />);
    const links = brief("Bravo").querySelector<HTMLElement>(".configured-brief__links")!;
    const [first, second] = within(links).getAllByRole("listitem");

    expect(first!.querySelector(".status-mark--success")).not.toBeNull();
    expect(within(first!).getByText("Done")).toHaveClass("sr-only");
    expect(second!.querySelector(".status-mark")).toBeNull();
    expect(
      within(links)
        .getByRole("link", { name: "Products" })
        .closest(".configured-brief__chip")!
        .querySelector(".status-mark--progress"),
    ).not.toBeNull();
  });

  it("stages a tag change through the edit session like a table cell edit", async () => {
    const onStageOps = vi.fn(async () => {});
    render(<ConfiguredViewCards {...rendererProps({ execution: ideaBriefs(), onStageOps })} />);
    const bravo = brief("Bravo");

    fireEvent.click(within(bravo).getByRole("button", { name: "Edit Priority" }));
    fireEvent.click(within(bravo).getByRole("menuitemradio", { name: "High" }));

    await waitFor(() => expect(onStageOps).toHaveBeenCalledTimes(1));
    expect(onStageOps).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: "setField",
        path: "ideas/Bravo.md",
        field: "priority",
        value: "high",
        fieldValue: { kind: "scalar", scalar: "high" },
      }),
    ]);
    expect(within(bravo).queryByRole("menu")).toBeNull();
  });

  it("moves through a tag menu with arrow keys and closes it with Escape", () => {
    render(
      <ConfiguredViewCards
        {...rendererProps({ execution: ideaBriefs(), onStageOps: async () => {} })}
      />,
    );
    const bravo = brief("Bravo");
    const trigger = within(bravo).getByRole("button", { name: "Edit Priority" });

    fireEvent.click(trigger);
    const menu = within(bravo).getByRole("menu");
    // The menu opens on the current value.
    expect(within(menu).getByRole("menuitemradio", { name: "Low" })).toHaveFocus();

    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(within(menu).getByRole("menuitemradio", { name: "High" })).toHaveFocus();
    fireEvent.keyDown(menu, { key: "ArrowUp" });
    expect(within(menu).getByRole("menuitemradio", { name: "Low" })).toHaveFocus();

    fireEvent.keyDown(menu, { key: "Escape" });
    expect(within(bravo).queryByRole("menu")).toBeNull();
    expect(trigger).toHaveFocus();
  });

  it("marks a staged tag and keeps read-only tags as plain marks", () => {
    const result = ideaBriefs();

    const editSession: OntologyEditSessionResponse = {
      sessionId: "edit",
      status: "dirty",
      hasUncommittedChanges: true,
      createdAt: "2026-10-03T00:00:00Z",
      updatedAt: "2026-10-03T00:00:00Z",
      ops: [{ kind: "setField", path: "ideas/Bravo.md", field: "priority", value: "high" }],
    };

    const { rerender } = render(
      <ConfiguredViewCards
        {...rendererProps({ execution: result, editSession, onStageOps: async () => {} })}
      />,
    );

    expect(within(brief("Bravo")).getByRole("button", { name: "Edit Priority" })).toHaveClass(
      "is-dirty",
    );

    rerender(<ConfiguredViewCards {...rendererProps({ execution: result })} />);
    expect(within(brief("Bravo")).queryByRole("button", { name: "Edit Priority" })).toBeNull();
    expect(within(brief("Bravo")).getByText("Low")).toBeVisible();
  });
});
