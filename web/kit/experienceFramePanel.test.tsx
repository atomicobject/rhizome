// The panel frame's one page (SPEC-0136 R6): it stays mounted across subjects.
import { act, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { PANEL_TAB_ID, PHASE_MESSAGE } from "../src/lib/customViewMessages";
import { jsonReply } from "../src/test/fakeFetch";
import { useActionQuery } from "./actions";
import { record, withExperienceFrame } from "./experienceFrameHarness";
import { useViewPreference } from "./preferences";
import { useViewContext } from "./viewContext";

const { http, start, locate, sent } = withExperienceFrame();

describe("the panel page", () => {
  it("keeps one component across subjects with a new navigation each time", async () => {
    function Panel() {
      const [notes, setNotes] = useState("");
      const context = useViewContext();

      return (
        <label>
          {context.kind === "node" ? context.ref.notePath : context.kind}
          <input value={notes} onChange={(event) => setNotes(event.target.value)} />
        </label>
      );
    }

    const { frame } = start({ panel: Panel });
    await locate(PANEL_TAB_ID, "p-1", { view: "panel", context: record("a.md") });
    await act(async () => {
      const input = screen.getByRole("textbox");
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
      setter?.call(input, "kept");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });

    await locate(PANEL_TAB_ID, "p-2", { view: "panel", context: record("b.md") });
    expect(screen.getByRole("textbox")).toHaveProperty("value", "kept");
    expect(screen.getByText("b.md")).toBeTruthy();
    expect(frame.state().pages[0]?.binding.navigationId).toBe("p-2");
    // The kept page's module is already loaded, so each navigation marks it at once.
    expect(
      sent(PHASE_MESSAGE).filter(
        (message) => message.type === PHASE_MESSAGE && message.phase === "module-loaded",
      ),
    ).toEqual([
      expect.objectContaining({ tabId: PANEL_TAB_ID, navigationId: "p-1" }),
      expect.objectContaining({ tabId: PANEL_TAB_ID, navigationId: "p-2" }),
    ]);
  });

  it("renders a subject-independent read that was in flight across a subject change", async () => {
    let answer: (response: Response) => void = () => undefined;
    http.on(
      "POST",
      "/api/v1/actions/module/tree",
      () => new Promise((resolve) => (answer = resolve)),
    );

    function Panel() {
      const { data } = useActionQuery<string>("module/tree");
      const context = useViewContext();

      return (
        <p>
          {context.kind === "node" ? context.ref.notePath : ""} {data ?? "loading"}
        </p>
      );
    }

    start({ panel: Panel });
    await locate(PANEL_TAB_ID, "p-1", { view: "panel", context: record("a.md") });
    await locate(PANEL_TAB_ID, "p-2", { view: "panel", context: record("b.md") });
    await act(async () => answer(jsonReply({ status: "ok", result: "tree" })));

    expect((await screen.findByText(/tree/)).textContent).toContain("b.md");
  });

  it("scopes preferences to the panel's experience, not to the subject or the panel's view ID", async () => {
    function Panel() {
      const height = useViewPreference("height", {
        defaultValue: 200,
        validate: (value): value is number => Number.isFinite(value),
      });

      return <p>{String(height.value)}</p>;
    }

    start({ panel: Panel, renamed: Panel });
    await locate(PANEL_TAB_ID, "p-1", { view: "panel", context: record("a.md") });
    await locate(PANEL_TAB_ID, "p-2", { view: "panel", context: record("b.md") });
    await screen.findByText("200");
    await locate(PANEL_TAB_ID, "p-3", { view: "renamed", context: record("b.md") });
    await screen.findByText("200");

    const scopes = http
      .requests("GET", "/api/v1/view-preferences")
      .map((request) => JSON.parse(request.query.get("scope") ?? "{}"));

    expect(scopes.length).toBeGreaterThan(0);
    expect(new Set(scopes.map((scope) => JSON.stringify(scope)))).toEqual(
      new Set([JSON.stringify({ viewId: "experience:x", context: { kind: "standalone" } })]),
    );
  });
});
