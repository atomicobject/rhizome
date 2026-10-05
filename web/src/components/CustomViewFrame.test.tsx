import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { OntologyEditOp, OntologyEditSessionResponse, ViewCatalogEntry } from "../api/types";
import {
  EDIT_SESSION_HELLO_MESSAGE,
  EDIT_SESSION_MESSAGE,
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  OPEN_NOTE_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_VIEW_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  VAULT_EVENT_MESSAGE,
  type ViewMessage,
} from "../lib/customViewMessages";
import { publishVaultEvent } from "../query/vaultEvents";
import { CustomViewFrame } from "./CustomViewFrame";

const view: ViewCatalogEntry = {
  id: "poc.action-board",
  name: "Action Board",
  source: { kind: "custom", entry: "action-board.tsx" },
  mount: { kind: "standalone" },
  defaults: {},
  variants: {},
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "poc.action-board",
    name: "Action Board",
    source: { kind: "custom", entry: "action-board.tsx" },
    mount: { kind: "standalone" },
    variants: {},
  },
};

type MalformedMessage = {
  type: string;
  path?: number;
  requestId?: string;
  ops?: string;
  name?: string | number;
  scope?: { kind: string; key?: string };
};

function post(
  source: MessageEventSource | null,
  origin: string,
  data: ViewMessage | MalformedMessage,
) {
  window.dispatchEvent(new MessageEvent("message", { source, origin, data }));
}

describe("CustomViewFrame", () => {
  it("frames the view's standalone URL and opens notes the view asks for", () => {
    const onOpenNote = vi.fn();
    render(
      <CustomViewFrame view={view} session={null} onOpenNote={onOpenNote} onStageOps={vi.fn()} />,
    );
    const frame = screen.getByTitle<HTMLIFrameElement>("Action Board");

    expect(frame.getAttribute("src")).toBe("/views/poc.action-board?hosted=1");
    expect(frame.hasAttribute("sandbox")).toBe(false);

    const message: ViewMessage = { type: OPEN_NOTE_MESSAGE, path: "docs/a.md" };
    post(window, window.location.origin, message);
    post(frame.contentWindow, "https://elsewhere.example", message);
    post(frame.contentWindow, window.location.origin, { type: OPEN_NOTE_MESSAGE, path: 7 });
    expect(onOpenNote).not.toHaveBeenCalled();

    post(frame.contentWindow, window.location.origin, message);
    expect(onOpenNote).toHaveBeenCalledExactlyOnceWith("docs/a.md", "activate");

    post(frame.contentWindow, window.location.origin, { ...message, beside: true });
    expect(onOpenNote).toHaveBeenLastCalledWith("docs/a.md", "beside");
  });

  it("bridges canonical navigation only from the active frame and remounts for a new subject", () => {
    const onOpenNode = vi.fn();
    const onOpenView = vi.fn();

    const props = {
      view,
      session: null,
      onOpenNote: vi.fn(),
      onStageOps: vi.fn(),
      onOpenNode,
      onOpenView,
    };

    const { rerender } = render(
      <CustomViewFrame {...props} context={{ kind: "group", group: "Delivery" }} />,
    );

    const frame = screen.getByTitle<HTMLIFrameElement>("Action Board");
    const source = frame.contentWindow;
    const ref = { notePath: "docs/a.md", fragment: "^story", nodeId: "story", kind: "EMBEDDED" };

    const message: ViewMessage = {
      type: OPEN_NODE_MESSAGE,
      ref,
      view: "story-detail",
      beside: true,
    };

    post(window, window.location.origin, message);
    post(source, "https://elsewhere.test", message);
    expect(onOpenNode).not.toHaveBeenCalled();
    post(source, window.location.origin, message);
    expect(onOpenNode).toHaveBeenCalledExactlyOnceWith(ref, { view: "story-detail", beside: true });
    post(source, window.location.origin, {
      type: OPEN_VIEW_MESSAGE,
      id: "delivery",
      context: { kind: "group", group: "Delivery" },
    });
    expect(onOpenView).toHaveBeenCalledExactlyOnceWith("delivery", {
      kind: "group",
      group: "Delivery",
    });
    expect(frame.src).toContain("context=");

    rerender(<CustomViewFrame {...props} context={{ kind: "group", group: "Knowledge" }} />);
    expect(screen.getByTitle("Action Board")).not.toBe(frame);
    post(source, window.location.origin, message);
    expect(onOpenNode).toHaveBeenCalledTimes(1);
  });

  it("does not deliver an old stage result into a newly mounted context", async () => {
    let settle: () => void = () => undefined;

    const onStageOps = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          settle = resolve;
        }),
    );

    const props = { view, session: null, onOpenNote: vi.fn(), onStageOps };

    const { rerender } = render(
      <CustomViewFrame {...props} context={{ kind: "group", group: "Delivery" }} />,
    );

    const oldFrame = screen.getByTitle<HTMLIFrameElement>("Action Board").contentWindow;
    post(oldFrame, window.location.origin, {
      type: STAGE_OPS_MESSAGE,
      requestId: "stage-1",
      ops: [{ kind: "setField", path: "a.md", field: "title" }],
    });

    rerender(<CustomViewFrame {...props} context={{ kind: "group", group: "Other" }} />);

    const sent = vi.spyOn(
      screen.getByTitle<HTMLIFrameElement>("Action Board").contentWindow!,
      "postMessage",
    );

    settle();
    await Promise.resolve();
    expect(sent).not.toHaveBeenCalled();
  });

  it("bridges the workspace edit session to the view", async () => {
    const op: OntologyEditOp = { kind: "setField", path: "docs/a.md#item-1", field: "done" };
    // SAFETY: the frame forwards the session untouched; only identity matters here.
    const staged = { sessionId: "sess-1", revision: 2 } as OntologyEditSessionResponse;

    const onStageOps = vi
      .fn()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new Error("note is reserved"));

    const { rerender } = render(
      <CustomViewFrame view={view} session={null} onOpenNote={vi.fn()} onStageOps={onStageOps} />,
    );

    const frame = screen.getByTitle<HTMLIFrameElement>("Action Board").contentWindow!;
    const sent = vi.spyOn(frame, "postMessage");
    const origin = window.location.origin;

    post(frame, origin, { type: EDIT_SESSION_HELLO_MESSAGE });
    expect(sent).toHaveBeenLastCalledWith({ type: EDIT_SESSION_MESSAGE, session: null }, origin);

    rerender(
      <CustomViewFrame view={view} session={staged} onOpenNote={vi.fn()} onStageOps={onStageOps} />,
    );
    expect(sent).toHaveBeenLastCalledWith({ type: EDIT_SESSION_MESSAGE, session: staged }, origin);

    const readLifecycle = { revision: 1, outcome: "saved" as const, savedSession: staged };
    rerender(
      <CustomViewFrame
        view={view}
        session={null}
        readLifecycle={readLifecycle}
        onOpenNote={vi.fn()}
        onStageOps={onStageOps}
      />,
    );
    expect(sent).toHaveBeenLastCalledWith(
      { type: EDIT_SESSION_MESSAGE, session: null, readLifecycle },
      origin,
    );

    post(frame, origin, { type: STAGE_OPS_MESSAGE, requestId: "bad", ops: "nope" });
    post(window, origin, { type: STAGE_OPS_MESSAGE, requestId: "stray", ops: [op] });
    post(frame, origin, { type: STAGE_OPS_MESSAGE, requestId: "stage-1", ops: [op] });
    expect(onStageOps).toHaveBeenCalledExactlyOnceWith([op]);
    await waitFor(() =>
      expect(sent).toHaveBeenLastCalledWith(
        { type: STAGE_RESULT_MESSAGE, requestId: "stage-1" },
        origin,
      ),
    );

    post(frame, origin, { type: STAGE_OPS_MESSAGE, requestId: "stage-2", ops: [op] });
    await waitFor(() =>
      expect(sent).toHaveBeenLastCalledWith(
        { type: STAGE_RESULT_MESSAGE, requestId: "stage-2", error: "note is reserved" },
        origin,
      ),
    );
  });

  it("forwards every host vault event to the current frame only", () => {
    const props = { view, session: null, onOpenNote: vi.fn(), onStageOps: vi.fn() };

    const { rerender, unmount } = render(
      <CustomViewFrame {...props} context={{ kind: "group", group: "Delivery" }} />,
    );

    const oldFrame = screen.getByTitle<HTMLIFrameElement>("Action Board").contentWindow!;
    const oldSent = vi.spyOn(oldFrame, "postMessage");
    const origin = window.location.origin;

    publishVaultEvent({ event: "schema.invalidated" });
    publishVaultEvent({ event: "views.changed", data: '{"folder":"board"}' });
    expect(oldSent.mock.calls).toEqual([
      [{ type: VAULT_EVENT_MESSAGE, event: "schema.invalidated" }, origin],
      [{ type: VAULT_EVENT_MESSAGE, event: "views.changed", data: '{"folder":"board"}' }, origin],
    ]);

    rerender(<CustomViewFrame {...props} context={{ kind: "group", group: "Knowledge" }} />);

    const newSent = vi.spyOn(
      screen.getByTitle<HTMLIFrameElement>("Action Board").contentWindow!,
      "postMessage",
    );

    publishVaultEvent({ event: "index.changed" });
    expect(oldSent).toHaveBeenCalledTimes(2);
    expect(newSent).toHaveBeenCalledExactlyOnceWith(
      { type: VAULT_EVENT_MESSAGE, event: "index.changed" },
      origin,
    );

    unmount();
    publishVaultEvent({ event: "index.changed" });
    expect(newSent).toHaveBeenCalledTimes(1);
  });

  it("opens issues and collections only for well-formed requests from its frame", () => {
    const onOpenIssues = vi.fn();
    const onSelectCollection = vi.fn();
    render(
      <CustomViewFrame
        view={view}
        session={null}
        onOpenNote={vi.fn()}
        onStageOps={vi.fn()}
        onOpenIssues={onOpenIssues}
        onSelectCollection={onSelectCollection}
      />,
    );
    const frame = screen.getByTitle<HTMLIFrameElement>("Action Board").contentWindow;
    const origin = window.location.origin;
    const scoped: ViewMessage = { type: OPEN_ISSUES_MESSAGE, scope: { kind: "type", key: "Spec" } };
    const collection: ViewMessage = { type: OPEN_COLLECTION_MESSAGE, name: "Spec" };

    post(window, origin, scoped);
    post(frame, "https://elsewhere.example", scoped);
    post(window, origin, collection);
    post(frame, "https://elsewhere.example", collection);
    post(frame, origin, { type: OPEN_ISSUES_MESSAGE, scope: { kind: "global" } });
    post(frame, origin, { type: OPEN_ISSUES_MESSAGE, scope: { kind: "note", key: " " } });
    post(frame, origin, { type: OPEN_COLLECTION_MESSAGE, name: 7 });
    post(frame, origin, { type: OPEN_COLLECTION_MESSAGE, name: "" });
    post(frame, origin, { type: OPEN_COLLECTION_MESSAGE, name: "__issues__" });
    expect(onOpenIssues).not.toHaveBeenCalled();
    expect(onSelectCollection).not.toHaveBeenCalled();

    post(frame, origin, scoped);
    post(frame, origin, { type: OPEN_ISSUES_MESSAGE });
    post(frame, origin, collection);
    expect(onOpenIssues.mock.calls).toEqual([[{ kind: "type", key: "Spec" }], [undefined]]);
    expect(onSelectCollection).toHaveBeenCalledExactlyOnceWith("Spec");
  });
});
