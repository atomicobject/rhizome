import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Fact, Facts } from "./layout";
import { RecordPage } from "./record-page";

describe("RecordPage", () => {
  it("shows the title, kind, facts, and actions while the body is still loading", () => {
    render(
      <RecordPage
        kind={<span>Assertion</span>}
        title="Caching is per generation"
        facts={
          <Facts>
            <Fact value={3}>sources</Fact>
          </Facts>
        }
        actions={<button type="button">Open note</button>}
        status={{ status: "loading" }}
        what="citations"
      >
        <p>body</p>
      </RecordPage>,
    );

    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Caching is per generation");
    expect(screen.getByRole("article", { name: "Caching is per generation" })).toBeTruthy();
    expect(screen.getByText("Assertion")).toBeTruthy();
    expect(screen.getByText("sources")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Open note" })).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe("Loading citations…");
    expect(screen.queryByText("body")).toBeNull();
  });

  it("shows a failed read with a retry in place of the body", () => {
    const retry = vi.fn();
    render(
      <RecordPage title="A" status={{ status: "error", error: new Error("offline"), retry }}>
        <p>body</p>
      </RecordPage>,
    );

    expect(screen.getByRole("alert").textContent).toContain("Could not load this record: offline");
    screen.getByRole("button", { name: "Retry" }).click();
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("says what is missing above a partial body, and places the aside beside it", () => {
    render(
      <RecordPage
        title="A"
        status={{ status: "ready" }}
        partial="Showing 200 of 1,195 linked records."
        aside={<p>related</p>}
      >
        {() => <p>body</p>}
      </RecordPage>,
    );

    expect(screen.getByRole("status").textContent).toBe("Showing 200 of 1,195 linked records.");
    expect(screen.getByText("body")).toBeTruthy();
    expect(screen.getByRole("complementary").textContent).toBe("related");
  });
});
