import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { isString } from "../api/parse";
import type { PublicNode } from "../api/publicGraphQLTypes";
import type { ViewCatalog, ViewCatalogEntry } from "../api/types";
import { EDITOR_FLUSH_EVENT } from "./editing/editorFlush";
import { deferredReply, graphQLRequestBody, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { preferenceServer } from "../viewPreferences/testServer";
import { NoteTab } from "./NoteTab";
import { viewSegment } from "../test/workspaceView";

const definition: ViewCatalogEntry = {
  id: "delivery.effort-detail",
  name: "Effort detail",
  source: { kind: "custom", entry: "detail.html" },
  mount: { kind: "node", type: "EffortNote", default: true },
  defaults: {},
  variants: {},
  definition: {
    apiVersion: "rhizome.view.v1",
    id: "delivery.effort-detail",
    name: "Effort detail",
    source: { kind: "custom", entry: "detail.html" },
    mount: { kind: "node", type: "EffortNote", default: true },
    variants: {},
  },
};

const customChoiceId = 'view:["delivery.effort-detail","custom"]';

const catalog: ViewCatalog = {
  views: [definition],
  targets: [
    {
      kind: "node",
      name: "EffortNote",
      defaultChoiceId: customChoiceId,
      choices: [
        { id: "builtin:read", name: "Structure", renderer: "read" },
        { id: "builtin:source", name: "Markdown", renderer: "source" },
        { id: customChoiceId, name: definition.name, renderer: "custom", viewId: definition.id },
      ],
    },
  ],
};

const noop = () => {};

const stage = async () => {};

function tab(path = "notes/a.md", presentation?: string) {
  return { id: `note:${path}`, kind: "note" as const, path, dirty: false, presentation };
}

let fingerprint = "fingerprint-1";

function node(ref: string): PublicNode {
  const [path, fragment] = ref.split("#", 2);

  return {
    ref: {
      ref,
      notePath: path,
      path,
      kind: fragment ? "EMBEDDED" : "NOTE",
      fragment,
      nodeId: fragment ? `embedded:${fragment}` : `note:${path}`,
      structural: fragment ? fingerprint : undefined,
    },
    nodeId: fragment ? `embedded:${fragment}` : `note:${path}`,
    nodeKind: fragment ? "EMBEDDED" : "NOTE",
    path,
    title: fragment || "Effort",
    resolvedType: "EffortNote",
    content: "Effort narrative",
    locator: { sourceLocator: ref, status: "linkable", exists: true, requiresFix: false },
  };
}

function props() {
  return {
    active: true,
    anchor: null,
    editSession: null,
    editing: false,
    onOpen: noop,
    onTitle: noop,
    registerContext: noop,
    onStageOps: stage,
    views: catalog,
    vaultKey: "vault-a",
  };
}

function select(name: string) {
  fireEvent.click(viewSegment(name));
}

function frameContext() {
  const frame = screen.getByTitle<HTMLIFrameElement>(definition.name);

  return JSON.parse(new URL(frame.src).searchParams.get("context") || "null");
}

describe("focused node presentations", () => {
  const http = withFakeFetch();
  withFakeEventSource();
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    fingerprint = "fingerprint-1";
    http.onGraphQL("PublicNodeDetail", (request) => {
      const value = graphQLRequestBody(request).variables?.ref;

      return jsonReply({ data: { node: node(isString(value) ? value : "notes/a.md") } });
    });
  });

  it("opens the catalog default and switches among custom, structured, and source", async () => {
    render(<NoteTab {...props()} tab={tab()} />);
    await screen.findByTitle(definition.name);
    expect(viewSegment(definition.name)).toHaveAttribute("aria-pressed", "true");
    expect(frameContext()).toMatchObject({
      kind: "node",
      type: "EffortNote",
      ref: { notePath: "notes/a.md", kind: "NOTE" },
    });
    select("Structure");
    expect(await screen.findByText("Effort narrative")).toBeVisible();
    expect(screen.queryByTitle(definition.name)).toBeNull();
    select("Markdown");
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    select(definition.name);
    await screen.findByTitle(definition.name);
    expect(screen.queryByRole("textbox", { name: "Note source" })).toBeNull();
  });

  it("uses an explicit presentation ahead of the configured default", async () => {
    render(<NoteTab {...props()} tab={tab("notes/a.md", "builtin:source")} />);
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    expect(screen.queryByTitle(definition.name)).toBeNull();
  });

  it("uses canonical embedded identity and isolates preferences by node and vault", async () => {
    const onTitle = vi.fn();
    const first = render(<NoteTab {...props()} tab={tab()} anchor="^effort-1" onTitle={onTitle} />);
    await waitFor(() => expect(frameContext()?.ref?.fragment).toBe("^effort-1"));
    expect(onTitle.mock.calls.map(([, title]) => title)).toEqual(["Effort"]);
    expect(frameContext().ref).toMatchObject({
      kind: "EMBEDDED",
      nodeId: "embedded:^effort-1",
      structuralFingerprint: "fingerprint-1",
    });
    select("Markdown");
    await screen.findByRole("textbox", { name: "Note source" });
    first.unmount();

    const otherNode = render(<NoteTab {...props()} tab={tab()} anchor="^effort-2" />);
    await waitFor(() => expect(frameContext()?.ref?.fragment).toBe("^effort-2"));
    otherNode.unmount();

    const again = render(<NoteTab {...props()} tab={tab()} anchor="^effort-1" />);
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    again.unmount();

    // Each vault has its own preference server.
    preferenceServer(http);
    render(<NoteTab {...props()} vaultKey="vault-b" tab={tab()} anchor="^effort-1" />);
    await waitFor(() => expect(frameContext()?.ref?.fragment).toBe("^effort-1"));
  });

  it("keeps a node's remembered presentation when its structural fingerprint changes", async () => {
    const first = render(<NoteTab {...props()} tab={tab()} anchor="^effort-1" />);
    await waitFor(() => expect(frameContext()?.ref?.fragment).toBe("^effort-1"));
    select("Markdown");
    await screen.findByRole("textbox", { name: "Note source" });
    first.unmount();

    fingerprint = "fingerprint-2";
    render(<NoteTab {...props()} tab={tab()} anchor="^effort-1" />);
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
  });

  it("opens a canonical embedded reference even without a fragment", async () => {
    const onTitle = vi.fn();
    render(
      <NoteTab
        {...props()}
        onTitle={onTitle}
        tab={{
          ...tab(),
          nodeKind: "EMBEDDED",
          nodeId: "effort-1",
          nodeRef: { notePath: "notes/a.md", kind: "EMBEDDED", nodeId: "effort-1" },
        }}
      />,
    );
    await waitFor(() => expect(frameContext()?.ref?.fragment).toBe("node:effort-1"));
    await waitFor(() =>
      expect(onTitle).toHaveBeenCalledExactlyOnceWith("note:notes/a.md", "node:effort-1"),
    );
    expect(
      http
        .requests("POST", "/api/v1/graphql")
        .map((request) => graphQLRequestBody(request).variables?.ref),
    ).toEqual(["notes/a.md", "notes/a.md#node:effort-1"]);
  });

  it("keeps source-only validation navigation visible despite a custom default", async () => {
    render(<NoteTab {...props()} tab={tab()} anchor="issue:unit=line&start=1" />);
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    expect(screen.queryByTitle(definition.name)).toBeNull();
    select(definition.name);
    await screen.findByTitle(definition.name);
  });

  it("preserves an explicitly linked custom view ahead of a legacy preference", async () => {
    window.sessionStorage.setItem("rhizome:notes:view-mode:v1:notes/a.md", "source");
    const onPresentation = vi.fn();
    render(
      <NoteTab
        {...props()}
        tab={tab("notes/a.md", definition.id)}
        onPresentation={onPresentation}
      />,
    );
    await screen.findByTitle(definition.name);
    expect(viewSegment(definition.name)).toHaveAttribute("aria-pressed", "true");
    expect(onPresentation).not.toHaveBeenCalled();
  });

  it("migrates the former root source preference once", async () => {
    window.sessionStorage.setItem("rhizome:notes:view-mode:v1:notes/a.md", "source");
    render(<NoteTab {...props()} tab={tab()} />);
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    expect(window.sessionStorage.getItem("rhizome:notes:view-mode:v1:notes/a.md")).toBeNull();
  });

  it("drops a former read preference so the configured default still wins", async () => {
    window.sessionStorage.setItem("rhizome:notes:view-mode:v1:notes/a.md", "read");
    const onPresentation = vi.fn();
    render(<NoteTab {...props()} tab={tab()} onPresentation={onPresentation} />);
    await screen.findByTitle(definition.name);
    await waitFor(() =>
      expect(window.sessionStorage.getItem("rhizome:notes:view-mode:v1:notes/a.md")).toBeNull(),
    );
    expect(viewSegment(definition.name)).toHaveAttribute("aria-pressed", "true");
    expect(onPresentation).not.toHaveBeenCalled();
  });

  it("retains the former root preference until the vault identity arrives", async () => {
    const legacyKey = "rhizome:notes:view-mode:v1:notes/a.md";

    window.sessionStorage.setItem(legacyKey, "source");
    const { rerender } = render(<NoteTab {...props()} vaultKey={null} tab={tab()} />);

    await screen.findByTitle(definition.name);
    expect(window.sessionStorage.getItem(legacyKey)).toBe("source");
    expect(window.localStorage.length).toBe(0);
    rerender(<NoteTab {...props()} tab={tab()} />);
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    expect(window.sessionStorage.getItem(legacyKey)).toBeNull();
  });

  it("keeps the current presentation when editor staging fails", async () => {
    render(<NoteTab {...props()} tab={tab()} />);
    await screen.findByTitle(definition.name);
    select("Markdown");
    await screen.findByRole("textbox", { name: "Note source" });

    const flush = (event: Event) => {
      // SAFETY: flushEditors dispatches this event with its typed waitUntil detail.
      (event as CustomEvent<{ waitUntil: (pending: Promise<void>) => void }>).detail.waitUntil(
        Promise.reject(new Error("Edit conflict")),
      );
    };

    window.addEventListener(EDITOR_FLUSH_EVENT, flush);

    try {
      select(definition.name);
      expect(await screen.findByRole("alert")).toHaveTextContent("Edit conflict");
      expect(screen.getByRole("textbox", { name: "Note source" })).toBeVisible();
      expect(screen.queryByTitle(definition.name)).toBeNull();
    } finally {
      window.removeEventListener(EDITOR_FLUSH_EVENT, flush);
    }
  });

  it("flushes pending editor operations before changing presentation", async () => {
    const pending = deferredReply<null>();

    const flush = vi.fn((event: Event) => {
      // SAFETY: flushEditors dispatches this event with its typed waitUntil detail.
      (event as CustomEvent<{ waitUntil: (pending: Promise<void>) => void }>).detail.waitUntil(
        pending.promise.then(() => undefined),
      );
    });

    render(<NoteTab {...props()} tab={tab()} />);
    await screen.findByTitle(definition.name);
    select("Markdown");
    await screen.findByRole("textbox", { name: "Note source" });
    window.addEventListener(EDITOR_FLUSH_EVENT, flush);

    try {
      select(definition.name);
      expect(flush).toHaveBeenCalledOnce();
      expect(screen.getByRole("textbox", { name: "Note source" })).toBeVisible();
      await act(async () => pending.resolve(null));
      await screen.findByTitle(definition.name);
      expect(screen.queryByRole("textbox", { name: "Note source" })).toBeNull();
    } finally {
      window.removeEventListener(EDITOR_FLUSH_EVENT, flush);
    }
  });
});
