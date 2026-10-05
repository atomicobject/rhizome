import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  HighlightProvider,
  RecordChip,
  RelativeTime,
  StatusMark,
  sharedLabelPrefix,
  statusPosition,
  typeLabel,
} from "./index";
import type { EnumValueDoc } from "./types";

// Declared out of order: `order` decides position, and a value without one comes last.
const LIFECYCLE: EnumValueDoc[] = [
  { name: "review", label: "In review", order: 3, tone: "progress", stage: "active" },
  { name: "draft", label: "Draft", order: 1, tone: "neutral", stage: "open" },
  { name: "abandoned", label: "Abandoned", tone: "muted", stage: "dropped" },
  { name: "building", label: "Building", order: 2, tone: "progress", stage: "active" },
  { name: "shipped", label: "Shipped", order: 4, tone: "success", stage: "done" },
  { name: "parked", label: "Parked", order: 5, tone: "info", stage: "dropped" },
  { name: "blocked", label: "Blocked", order: 6, tone: "risk", stage: "active" },
];

describe("status marks", () => {
  it("fill by position among non-terminal values in @view order", () => {
    const fills = ["draft", "building", "review", "blocked"].map((name) => {
      const position = statusPosition(LIFECYCLE, name);

      return position.kind === "active" ? position.fill : position.kind;
    });

    expect(fills).toEqual([0, 1 / 3, 2 / 3, 1]);
  });

  it("treat done and dropped stages as terminal, whatever the tone", () => {
    expect(statusPosition(LIFECYCLE, "shipped").kind).toBe("done");
    expect(statusPosition(LIFECYCLE, "abandoned").kind).toBe("closed");
    expect(statusPosition(LIFECYCLE, "parked").kind).toBe("closed");
    // Tone only colors the mark: a success-toned active value is still moving.
    expect(
      statusPosition([{ name: "accepted", tone: "success", stage: "active" }], "accepted").kind,
    ).toBe("active");
    // An enum without stages has no terminal values.
    expect(statusPosition([{ name: "closed", collapsed: true }], "closed").kind).toBe("active");
    expect(statusPosition(LIFECYCLE, "renamed").kind).toBe("unknown");
    expect(statusPosition([{ name: "only", stage: "active" }], "only")).toMatchObject({
      kind: "active",
      fill: 1,
    });
  });

  it("always pair the mark with the value's label, or its name when unlabeled", () => {
    const { container, rerender } = render(<StatusMark value="review" values={LIFECYCLE} />);

    expect(screen.getByText("In review")).toBeVisible();
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");

    rerender(<StatusMark value="renamed" values={LIFECYCLE} />);
    expect(screen.getByText("renamed")).toBeVisible();

    rerender(<StatusMark value="shipped" values={LIFECYCLE} hideLabel />);
    expect(screen.getByText("Shipped")).toHaveClass("sr-only");
    expect(container.firstElementChild).toHaveAttribute("title", "Shipped");

    rerender(<StatusMark value={null} values={LIFECYCLE} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("record chips", () => {
  it("light every occurrence of the pointed or focused record and nothing else", () => {
    const onOpen = vi.fn();
    render(
      <HighlightProvider>
        <RecordChip recordKey="specs/a.md" onOpen={onOpen}>
          Spec A
        </RecordChip>
        <RecordChip recordKey="specs/b.md">Spec B</RecordChip>
        <RecordChip recordKey="specs/a.md" indirect title="Spec A">
          Spec A again
        </RecordChip>
      </HighlightProvider>,
    );

    const first = screen.getByRole("button", { name: "Spec A" });
    const other = screen.getByText("Spec B").parentElement!;
    const again = screen.getByText("Spec A again").parentElement!;

    fireEvent.pointerEnter(first);
    expect(first).toHaveAttribute("data-highlighted", "true");
    expect(again).toHaveAttribute("data-highlighted", "true");
    expect(other).not.toHaveAttribute("data-highlighted");

    fireEvent.pointerEnter(other);
    fireEvent.pointerLeave(first);
    expect(other).toHaveAttribute("data-highlighted", "true");
    expect(first).not.toHaveAttribute("data-highlighted");

    fireEvent.pointerLeave(other);
    fireEvent.focus(again);
    expect(first).toHaveAttribute("data-highlighted", "true");

    fireEvent.click(first);
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(again).toHaveAttribute("title", "Spec A (reached through another record)");
    expect(screen.getByText("(indirect)")).toHaveClass("sr-only");
    // Indirect is more than a fainter color: it is italic, and styleable.
    expect(again).toHaveAttribute("data-indirect", "true");
    expect(again).toHaveClass("italic");
    expect(other).not.toHaveAttribute("data-indirect");
    expect(other).not.toHaveClass("italic");
  });

  it("truncate their label to one line unless asked to wrap it", () => {
    const { rerender } = render(<RecordChip recordKey="specs/a.md">Spec A</RecordChip>);

    expect(screen.getByText("Spec A")).toHaveClass("truncate");

    rerender(
      <RecordChip recordKey="specs/a.md" wrap>
        Spec A
      </RecordChip>,
    );
    expect(screen.getByText("Spec A")).toHaveClass("line-clamp-2");
    expect(screen.getByText("Spec A")).not.toHaveClass("truncate");
  });

  it("render without highlighting outside a provider", () => {
    render(<RecordChip recordKey="specs/a.md">Spec A</RecordChip>);
    const chip = screen.getByText("Spec A").parentElement!;

    fireEvent.pointerEnter(chip);
    expect(chip).not.toHaveAttribute("data-highlighted");
  });
});

describe("relative time", () => {
  const now = new Date("2026-10-03T12:00:00Z");

  it.each([
    ["2026-10-03T11:59:30Z", "now"],
    ["2026-10-03T11:55:00Z", "5m"],
    ["2026-10-02T09:00:00Z", "27h"],
    ["2026-09-21T12:00:00Z", "12d"],
  ])("shows %s compactly as %s with the exact time attached", (value, text) => {
    render(<RelativeTime value={value} now={now} />);
    const time = screen.getByText(text).closest("time");

    expect(time).toHaveAttribute("dateTime", new Date(value).toISOString());
    // A title alone reaches neither touch nor most screen readers.
    const exact = time!.getAttribute("title")!;
    expect(exact).not.toBe("");
    expect(time!.querySelector(".sr-only")?.textContent).toContain(exact);
  });

  it("falls back to a date for old times and renders nothing for invalid ones", () => {
    const { container, rerender } = render(<RelativeTime value="2025-03-04T12:00:00Z" now={now} />);

    expect(container.querySelector("time [aria-hidden]")?.textContent).toMatch(/2025/);
    rerender(<RelativeTime value="not a time" now={now} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("labels", () => {
  it("find a first word every label shares and leaves something after it", () => {
    expect(sharedLabelPrefix(["Delivery specs", "Delivery efforts"])).toBe("Delivery ");
    expect(sharedLabelPrefix(["Delivery specs", "Efforts"])).toBe("");
    expect(sharedLabelPrefix(["Delivery", "Delivery efforts"])).toBe("");
    expect(sharedLabelPrefix(["Deliveryish specs", "Delivery efforts"])).toBe("");
    expect(sharedLabelPrefix(["Delivery specs"])).toBe("");
  });

  it("choose singular or plural and drop a shared prefix", () => {
    const labels = { label: "Delivery spec", pluralLabel: "Delivery specs" };

    expect(typeLabel(labels)).toBe("Delivery spec");
    expect(typeLabel(labels, { count: 1, prefix: "Delivery " })).toBe("Spec");
    expect(typeLabel(labels, { count: 0, prefix: "Delivery " })).toBe("Specs");
    expect(typeLabel(labels, { plural: true })).toBe("Delivery specs");
    expect(typeLabel({ label: "Effort", pluralLabel: "Efforts" }, { prefix: "Delivery " })).toBe(
      "Effort",
    );
  });
});
