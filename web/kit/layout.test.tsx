import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { BarList, Count, Fact, Facts, Section, Segmented, sectionStatus } from "./index";

describe("Section", () => {
  it("names its region by the heading and shows a count beside it", () => {
    render(
      <Section title="Opportunities" count={12} caption="Ranked by signal.">
        <p>rows</p>
      </Section>,
    );

    const region = screen.getByRole("region", { name: "Opportunities" });

    expect(within(region).getByRole("heading", { level: 2 })).toHaveTextContent("Opportunities12");
    expect(region).toHaveTextContent("Ranked by signal.");
    expect(within(region).getByText("rows")).toBeVisible();
  });

  it("keeps its heading while the body loads, fails with a retry, or reads empty", () => {
    const retry = vi.fn();
    const build = vi.fn(() => <p>rows</p>);

    const { rerender } = render(
      <Section title="Ideas" status={{ status: "loading" }} what="ideas">
        {build}
      </Section>,
    );

    expect(screen.getByRole("heading", { name: "Ideas" })).toBeVisible();
    expect(screen.getByRole("status")).toHaveTextContent("Loading ideas…");
    expect(build).not.toHaveBeenCalled();

    rerender(
      <Section
        title="Ideas"
        status={{ status: "error", error: new Error("offline"), retry }}
        what="ideas"
      >
        {build}
      </Section>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Could not load ideas: offline");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(retry).toHaveBeenCalledOnce();

    rerender(
      <Section title="Ideas" status={{ status: "ready" }} empty="No ideas yet.">
        {build}
      </Section>,
    );
    expect(screen.getByText("No ideas yet.")).toBeVisible();
    expect(build).not.toHaveBeenCalled();

    rerender(
      <Section title="Ideas" status={{ status: "ready" }}>
        {build}
      </Section>,
    );
    expect(screen.getByText("rows")).toBeVisible();
  });

  it("derives one status from several queries, retrying only the failed ones", () => {
    const ok = { isPending: false, error: null, refetch: vi.fn() };
    const pending = { isPending: true, error: null, refetch: vi.fn() };
    const failed = { isPending: false, error: new Error("boom"), refetch: vi.fn() };

    expect(sectionStatus(ok)).toEqual({ status: "ready" });
    expect(sectionStatus(ok, pending)).toEqual({ status: "loading" });

    const status = sectionStatus(pending, failed);

    expect(status.status).toBe("error");

    if (status.status === "error") status.retry();
    expect(failed.refetch).toHaveBeenCalledOnce();
    expect(pending.refetch).not.toHaveBeenCalled();
  });
});

describe("Facts, Count, and Segmented", () => {
  it("lists facts with their values and marks risk", () => {
    render(
      <Facts note="Guide: tracking.">
        <Fact value={12}>records</Fact>
        <Fact value={2} tone="risk">
          with issues
        </Fact>
      </Facts>,
    );

    expect(screen.getByText("Guide: tracking.")).toBeVisible();
    expect(
      screen.getByText("with issues", { exact: false }).closest("[data-tone]"),
    ).toHaveAttribute("data-tone", "risk");
  });

  it("switches modes with radios that carry the label and count", () => {
    const onChange = vi.fn();
    render(
      <Segmented
        label="Lens"
        value="rank"
        onChange={onChange}
        options={[
          { value: "rank", label: "Rank" },
          { value: "impact", label: "By impact", count: 5, description: "(default)" },
        ]}
      />,
    );

    const group = screen.getByRole("radiogroup", { name: "Lens" });

    expect(within(group).getByRole("radio", { name: "Rank" })).toBeChecked();
    fireEvent.click(within(group).getByRole("radio", { name: "By impact 5 (default)" }));
    expect(onChange).toHaveBeenCalledWith("impact");
  });

  it("renders counts as unboxed numerals", () => {
    render(<Count tone="risk">3</Count>);

    expect(screen.getByText("3")).toHaveAttribute("data-tone", "risk");
  });
});

describe("BarList", () => {
  it("scales bars to the largest value and opens labels", () => {
    const onOpen = vi.fn();

    const { container } = render(
      <BarList
        label="Time by office"
        items={[
          { label: "Ann Arbor", value: 30, display: "30 h", onOpen },
          { label: "Chicago", value: 15, series: "link" },
        ]}
      />,
    );

    const bars = [...container.querySelectorAll<HTMLElement>(".rz-bars-track > i")];

    expect(screen.getByRole("list", { name: "Time by office" })).toBeVisible();
    expect(bars.map((bar) => bar.style.inlineSize)).toEqual(["100%", "50%"]);
    expect(bars[1]).toHaveAttribute("data-series", "link");
    expect(screen.getByText("30 h")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Ann Arbor" }));
    expect(onOpen).toHaveBeenCalledOnce();
  });
});
