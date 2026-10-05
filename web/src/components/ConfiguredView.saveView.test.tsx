import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { isJsonObject } from "../api/parse";
import type { ViewSaveRequest } from "../api/types";
import { decodeJSONBody, withFakeFetch } from "../test/fakeFetch";
import { ideaBoard, ideaView } from "./ConfiguredView.profileFixtures";
import { renderView } from "./ConfiguredView.testFixtures";

const SAVE_PATH = `/api/v1/views/${encodeURIComponent(ideaView.id)}/save`;

const view = {
  ...ideaView,
  availableVariants: ["table", "kanban"],
  defaults: { variant: "kanban" },
  variants: { ...ideaView.variants, kanban: { columnField: "status" } },
};

function isSaveRequest(value: unknown): value is ViewSaveRequest {
  return isJsonObject(value) && isJsonObject(value.state);
}

function renderBoard(onViewSaved = vi.fn(async () => {}), state = {}) {
  renderView({
    view,
    execution: { ...ideaBoard(), view, definitionFingerprint: "fp-1" },
    state: { variant: "kanban", ...state },
    onViewSaved,
  });

  return onViewSaved;
}

describe("Save view", () => {
  const http = withFakeFetch();

  it("posts the current presentation with the loaded fingerprint and shows the written path", async () => {
    http.json("POST", SAVE_PATH, {
      id: "ip-ideas",
      path: ".rhizome/views/ip-ideas.yaml",
      created: true,
    });

    const onViewSaved = renderBoard(undefined, { laneField: "priority", search: "agents" });

    // A lane choice the definition does not hold is an unsaved change.
    fireEvent.click(screen.getByRole("button", { name: "Save view, unsaved changes" }));
    // A typed search is temporary; it saves only when the reader includes it.
    fireEvent.click(screen.getByRole("checkbox", { name: "Include search “agents”" }));
    fireEvent.click(screen.getByRole("button", { name: "Write view YAML" }));

    expect(await screen.findByRole("status")).toHaveTextContent(
      "Saved .rhizome/views/ip-ideas.yaml",
    );
    expect(onViewSaved).toHaveBeenCalledWith(
      expect.objectContaining({ id: "ip-ideas", created: true }),
    );

    const body = decodeJSONBody(http.requests("POST", SAVE_PATH)[0], isSaveRequest);
    expect(body.definitionFingerprint).toBe("fp-1");
    expect(body.state).toEqual(
      expect.objectContaining({ variant: "kanban", laneField: "priority", search: "agents" }),
    );
    // Settings the reader left alone stay as the file has them.
    expect(body.state).not.toHaveProperty("columnField");
    expect(body.state).not.toHaveProperty("columns");
  });

  it("toggles a header sort away from the view's own sort", () => {
    const result = { ...ideaBoard(), view: { ...view, defaults: { variant: "table" } } };
    // SAFETY: the generated execution state has no assignable concrete shape.
    result.state = { sort: [{ field: "title", direction: "asc" }] } as typeof result.state;
    const onStateChange = vi.fn();
    renderView({
      view: result.view,
      execution: result,
      state: { variant: "table" },
      onStateChange,
    });

    fireEvent.click(screen.getByRole("button", { name: /^Sort Title/ }));
    expect(onStateChange).toHaveBeenCalledWith(
      expect.objectContaining({ sort: [{ field: "title", direction: "desc" }] }),
    );
  });

  it("shows no unsaved marker when nothing differs from the definition", () => {
    renderBoard();

    expect(screen.getByRole("button", { name: "Save view" })).toBeVisible();
  });

  it("shows an invalid result inline", async () => {
    http.json(
      "POST",
      SAVE_PATH,
      { error: "variants.kanban.laneField: unknown field", code: "invalid_view" },
      400,
    );

    const onViewSaved = renderBoard();
    fireEvent.click(screen.getByRole("button", { name: "Save view" }));
    fireEvent.click(screen.getByRole("button", { name: "Write view YAML" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Could not save view: .*unknown field/,
    );
    expect(screen.queryByRole("button", { name: "Save anyway" })).toBeNull();
    expect(onViewSaved).not.toHaveBeenCalled();
  });

  it("offers to save anyway after a fingerprint conflict, without the stale fingerprint", async () => {
    http.json("POST", SAVE_PATH, { error: "view changed", code: "conflict" }, 409);

    renderBoard();
    fireEvent.click(screen.getByRole("button", { name: "Save view" }));
    fireEvent.click(screen.getByRole("button", { name: "Write view YAML" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("The view file changed since this view loaded.");

    http.json("POST", SAVE_PATH, {
      id: ideaView.id,
      path: ".rhizome/views/ip-ideas.yaml",
      created: false,
    });
    fireEvent.click(screen.getByRole("button", { name: "Save anyway" }));

    await waitFor(() => expect(http.requests("POST", SAVE_PATH)).toHaveLength(2));
    const retry = decodeJSONBody(http.requests("POST", SAVE_PATH)[1], isSaveRequest);
    expect(retry.definitionFingerprint).toBeUndefined();
    expect(await screen.findByRole("status")).toHaveTextContent("Saved");
  });
});
