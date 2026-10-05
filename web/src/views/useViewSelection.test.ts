import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ViewCatalogEntry, ViewTarget } from "../api/types";
import { resolveViewChoice, useViewSelection, viewSelectionKey } from "./useViewSelection";

import { deferredReply, withFakeFetch } from "../test/fakeFetch";
import { preferenceServer } from "../viewPreferences/testServer";
import { clearPreferenceStores } from "../viewPreferences/store";
import type { ViewContext } from "./context";
import type { ViewPreferenceResponse, ViewPreferenceScope } from "../viewPreferences/types";

const http = withFakeFetch();

let server: ReturnType<typeof preferenceServer>;

type VaultProps = { vaultKey: string | null };

const target: ViewTarget = {
  kind: "group",
  name: "Delivery",
  defaultChoiceId: "dashboard",
  choices: [
    { id: "builtin:overview", name: "Overview", renderer: "overview" },
    { id: "dashboard", name: "Dashboard", renderer: "custom", viewId: "delivery.dashboard" },
  ],
};

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  server = preferenceServer(http);
});

afterEach(clearPreferenceStores);

describe("view selection", () => {
  const noteTarget: ViewTarget = {
    kind: "node",
    name: "_FallbackNote",
    defaultChoiceId: "builtin:read",
    choices: [
      { id: "builtin:read", name: "Read", renderer: "read" },
      { id: "builtin:source", name: "Source", renderer: "source" },
    ],
  };

  const noteContext: ViewContext = {
    kind: "node",
    type: "_FallbackNote",
    ref: {
      notePath: "task-flow.md",
      kind: "NOTE",
      fragment: undefined,
      nodeId: undefined,
      typeName: undefined,
      structuralFingerprint: undefined,
    },
  };

  const noteScope: ViewPreferenceScope = { viewId: "$selection", context: noteContext };
  const noteModeKey = "rhizome:notes:view-mode:v1:task-flow.md";

  it("persists an in-memory node reference with undefined optional fields", async () => {
    const { result } = renderHook(() =>
      useViewSelection({ target: noteTarget, context: noteContext, vaultKey: "vault" }),
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(http.requests("GET", "/api/v1/view-preferences")).toHaveLength(1);
    act(() => result.current.select("builtin:source"));
    await waitFor(() => expect(result.current.pending).toBe(false));
    expect(server.read(noteScope).values).toEqual({ choice: "builtin:source" });
    expect(result.current.choice?.id).toBe("builtin:source");
  });

  it("retains legacy note mode until import acknowledgment for the canonical whole note", async () => {
    window.sessionStorage.setItem(noteModeKey, "source");
    const delayed = deferredReply<ViewPreferenceResponse>();
    http.on("POST", "/api/v1/view-preferences/import", () => delayed.promise);

    const { result } = renderHook(() =>
      useViewSelection({
        target: noteTarget,
        context: { ...noteContext, ref: { ...noteContext.ref, fragment: "#heading" } },
        vaultKey: "vault",
      }),
    );

    await waitFor(() =>
      expect(http.requests("POST", "/api/v1/view-preferences/import")).toHaveLength(1),
    );
    const request = http.requests("POST", "/api/v1/view-preferences/import")[0];
    expect(JSON.parse(request.body!)).toMatchObject({
      scope: { context: { ref: { notePath: "task-flow.md", kind: "NOTE" } } },
      migrationId: noteModeKey,
      values: { choice: "builtin:source" },
    });
    expect(JSON.parse(request.body!).scope.context.ref).not.toHaveProperty("fragment");
    expect(window.sessionStorage.getItem(noteModeKey)).toBe("source");
    delayed.resolve({
      ...server.read(noteScope),
      revision: 1,
      values: { choice: "builtin:source" },
    });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.choice?.id).toBe("builtin:source");
    expect(window.sessionStorage.getItem(noteModeKey)).toBeNull();
  });

  it("keeps modern note choices and prevents legacy source mode returning after reset", async () => {
    server.change(noteScope, { choice: "builtin:read" });
    window.sessionStorage.setItem(noteModeKey, "source");

    const render = () =>
      renderHook(() =>
        useViewSelection({ target: noteTarget, context: noteContext, vaultKey: "vault" }),
      );

    const first = render();
    await waitFor(() => expect(first.result.current.loading).toBe(false));
    expect(first.result.current.choice?.id).toBe("builtin:read");
    expect(window.sessionStorage.getItem(noteModeKey)).toBeNull();
    expect(http.requests("PATCH", "/api/v1/view-preferences")).toHaveLength(0);
    await act(() => first.result.current.reset());
    first.unmount();
    clearPreferenceStores();
    window.sessionStorage.setItem(noteModeKey, "source");
    const second = render();
    await waitFor(() => expect(second.result.current.loading).toBe(false));
    expect(second.result.current.choice?.id).toBe("builtin:read");
    expect(server.read(noteScope).values).toEqual({});
    expect(window.sessionStorage.getItem(noteModeKey)).toBeNull();
  });

  it("claims legacy Read as an empty override without pinning the authored default", async () => {
    window.sessionStorage.setItem(noteModeKey, "read");

    const { result } = renderHook(() =>
      useViewSelection({ target: noteTarget, context: noteContext, vaultKey: "vault" }),
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(server.read(noteScope).values).toEqual({});
    expect(http.requests("POST", "/api/v1/view-preferences/import")).toHaveLength(1);
    expect(window.sessionStorage.getItem(noteModeKey)).toBeNull();
  });

  it("keeps a newer explicit default ahead of legacy Source while acknowledging both sources", async () => {
    const key = viewSelectionKey("vault", noteTarget);
    window.localStorage.setItem(key, "__default__");
    window.sessionStorage.setItem(noteModeKey, "source");

    const { result } = renderHook(() =>
      useViewSelection({ target: noteTarget, context: noteContext, vaultKey: "vault" }),
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.choice?.id).toBe("builtin:read");
    expect(server.read(noteScope).values).toEqual({});
    const imports = http.requests("POST", "/api/v1/view-preferences/import");
    expect(imports).toHaveLength(2);
    expect(imports.map((request) => JSON.parse(request.body!))).toEqual([
      expect.objectContaining({ migrationId: key, values: {} }),
      expect.objectContaining({ migrationId: noteModeKey, values: {} }),
    ]);
    expect(window.localStorage.getItem(key)).toBeNull();
    expect(window.sessionStorage.getItem(noteModeKey)).toBeNull();
  });

  it("uses explicit then valid remembered then configured default", () => {
    expect(resolveViewChoice(target)?.id).toBe("dashboard");
    expect(resolveViewChoice(target, null, "builtin:overview")?.id).toBe("builtin:overview");
    expect(resolveViewChoice(target, "delivery.dashboard", "builtin:overview")?.id).toBe(
      "dashboard",
    );
    expect(resolveViewChoice(target, "removed", "removed")?.id).toBe("dashboard");
  });
  it("resolves a registered native view ID to its declared default variant", () => {
    const native: ViewTarget = {
      ...target,
      choices: [
        ...target.choices,
        {
          id: "native-table",
          name: "Table",
          renderer: "table",
          viewId: "native",
          variant: "table",
        },
        {
          id: "native-board",
          name: "Board",
          renderer: "kanban",
          viewId: "native",
          variant: "kanban",
        },
      ],
    };

    const definition = { id: "native", defaults: { variant: "kanban" } };

    // Only the definition identity and default variant affect selection.
    const choices: ViewCatalogEntry[] = [
      {
        ...definition,
        name: "Native",
        source: { kind: "ontology_type" },
        mount: { kind: "type", type: "Spec" },
        variants: { table: {}, kanban: { columnField: "status" } },
        definition: {
          apiVersion: "rhizome.view.v1",
          id: "native",
          name: "Native",
          source: { kind: "ontology_type" },
          mount: { kind: "type", type: "Spec" },
          variants: { table: {}, kanban: { columnField: "status" } },
        },
      },
    ];

    expect(resolveViewChoice(native, "native", null, choices)?.id).toBe("native-board");
  });

  it("moves a remembered generated layout to the view that replaced it", () => {
    const generated = (variant: string) =>
      `view:${JSON.stringify(["generated.type.Idea.table", variant])}`;

    const replaced: ViewTarget = {
      kind: "type",
      name: "Idea",
      defaultChoiceId: "ideas-table",
      choices: [
        { id: "builtin:overview", name: "Overview", renderer: "overview" },
        { id: "ideas-table", name: "Table", renderer: "table", viewId: "ideas", variant: "table" },
        { id: "ideas-cards", name: "Cards", renderer: "card", viewId: "ideas", variant: "card" },
        {
          id: "other-board",
          name: "Other · Board",
          renderer: "kanban",
          viewId: "other",
          variant: "kanban",
          custom: true,
        },
      ],
    };

    expect(resolveViewChoice(replaced, null, generated("card"))?.id).toBe("ideas-cards");
    expect(resolveViewChoice(replaced, null, generated("kanban"))?.id).toBe("ideas-table");
  });

  it("follows the configured default after the default is picked", async () => {
    const onSelect = vi.fn();

    const { result, rerender } = renderHook(
      ({ current }) => useViewSelection({ target: current, vaultKey: "vault", onSelect }),
      { initialProps: { current: target } },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.select("builtin:overview"));
    act(() => result.current.select("dashboard"));
    await waitFor(() => expect(result.current.pending).toBe(false));
    expect(onSelect).toHaveBeenLastCalledWith(null);
    rerender({ current: { ...target, defaultChoiceId: "builtin:overview" } });
    expect(result.current.choice?.id).toBe("builtin:overview");
    expect(window.localStorage.length).toBe(0);
  });

  it("scopes node selections by canonical node subject", async () => {
    const nodeTarget = { ...target, kind: "node" as const, name: "Task" };

    const context = (nodeId: string): ViewContext => ({
      kind: "node",
      type: "Task",
      ref: { notePath: "tasks.md", kind: "EMBEDDED", nodeId },
    });

    const { result, rerender } = renderHook(
      ({ nodeId }) =>
        useViewSelection({ target: nodeTarget, vaultKey: "vault", context: context(nodeId) }),
      { initialProps: { nodeId: "one" } },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => result.current.select("builtin:overview"));
    await waitFor(() => expect(result.current.pending).toBe(false));
    rerender({ nodeId: "two" });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.choice?.id).toBe("dashboard");
    rerender({ nodeId: "one" });
    expect(result.current.choice?.id).toBe("builtin:overview");
  });

  const native = (kind: ViewTarget["kind"], name: string): ViewTarget => ({
    kind,
    name,
    defaultChoiceId: "table",
    choices: (kind === "standalone" ? [] : target.choices.slice(0, 1)).concat(
      { id: "table", name: "Table", renderer: "table", viewId: "spec", variant: "table" },
      { id: "cards", name: "Cards", renderer: "card", viewId: "spec", variant: "card" },
    ),
  });

  async function migrated(target: ViewTarget) {
    const hook = renderHook(() => useViewSelection({ target, vaultKey: "vault" }));
    await waitFor(() => expect(hook.result.current.loading).toBe(false));

    return hook.result.current;
  }

  const selectionValues = (target: ViewTarget) => {
    const scope: ViewPreferenceScope = {
      viewId: "$selection",
      context:
        target.kind === "standalone" ? { kind: "standalone" } : { kind: "type", type: target.name },
    };

    if (target.kind === "standalone") scope.slot = target.name;

    return server.read(scope).values;
  };

  it("waits for vault identity before migrating legacy mode and variant", async () => {
    const type = native("type", "Spec");
    window.localStorage.setItem("rhizome:notes:viewModeByType", JSON.stringify({ Spec: "table" }));
    window.localStorage.setItem("rhizome.view.variant.spec", "card");
    const initialProps: VaultProps = { vaultKey: null };

    const { result, rerender } = renderHook(
      ({ vaultKey }: VaultProps) => useViewSelection({ target: type, vaultKey }),
      { initialProps },
    );

    expect(result.current.choice?.id).toBe("table");
    expect(selectionValues(type)).toEqual({});
    rerender({ vaultKey: "vault" });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.choice?.id).toBe("cards");
    expect(selectionValues(type)).toEqual({ choice: "cards" });
    expect(window.localStorage.getItem("rhizome.view.variant.spec")).toBe("card");
  });

  it.each([
    ["standalone", native("standalone", "spec")],
    ["type", native("type", "Spec")],
  ])(
    "migrates scoped native variant for %s only after server acknowledgment",
    async (_, target) => {
      window.localStorage.setItem("rhizome:view:variant:v2:vault:spec", "card");
      expect((await migrated(target)).choice?.id).toBe("cards");
      expect(selectionValues(target)).toEqual({ choice: "cards" });
      expect(window.localStorage.getItem("rhizome:view:variant:v2:vault:spec")).toBeNull();
    },
  );

  it("migrates Home to Overview while preserving the shared legacy map", async () => {
    const type = native("type", "Spec");
    window.localStorage.setItem("rhizome:notes:viewModeByType", JSON.stringify({ Spec: "home" }));
    expect((await migrated(type)).choice?.id).toBe("builtin:overview");
    expect(selectionValues(type)).toEqual({ choice: "builtin:overview" });
    expect(window.localStorage.getItem("rhizome:notes:viewModeByType")).toBe('{"Spec":"home"}');
  });

  it("uses the default when a removed Overview was remembered", async () => {
    const type = { ...native("type", "Spec"), choices: native("type", "Spec").choices.slice(1) };
    window.localStorage.setItem(viewSelectionKey("vault", type), "builtin:overview");
    expect((await migrated(type)).choice?.id).toBe("table");
  });

  it("stores no override for legacy Table when only generated Table exists", async () => {
    const table = 'view:["generated.type.Spec.table","table"]';

    const type: ViewTarget = {
      kind: "type",
      name: "Spec",
      defaultChoiceId: table,
      choices: [
        {
          id: table,
          name: "Table",
          renderer: "table",
          viewId: "generated.type.Spec.table",
          variant: "table",
        },
      ],
    };

    window.localStorage.setItem("rhizome:notes:viewModeByType", JSON.stringify({ Spec: "table" }));
    expect((await migrated(type)).choice?.id).toBe(table);
    expect(selectionValues(type)).toEqual({});
  });

  it("migrates legacy Table to authored view ahead of generated default", async () => {
    const generated = 'view:["generated.type.Spec.table","table"]';

    const type = {
      ...native("type", "Spec"),
      defaultChoiceId: generated,
      choices: native("type", "Spec").choices.concat({
        id: generated,
        name: "Table",
        renderer: "table",
        viewId: "generated.type.Spec.table",
        variant: "table",
      }),
    };

    window.localStorage.setItem("rhizome:notes:viewModeByType", JSON.stringify({ Spec: "table" }));
    expect((await migrated(type)).choice?.id).toBe("table");
    expect(selectionValues(type)).toEqual({ choice: "spec" });
  });

  it("prefers authored default over a lower-order view during migration", async () => {
    const board = 'view:["board","table"]';

    const type = {
      ...native("type", "Spec"),
      defaultChoiceId: board,
      choices: native("type", "Spec").choices.concat({
        id: board,
        name: "Board",
        renderer: "table",
        viewId: "board",
        variant: "table",
      }),
    };

    window.localStorage.setItem("rhizome:notes:viewModeByType", JSON.stringify({ Spec: "table" }));
    expect((await migrated(type)).choice?.id).toBe(board);
    expect(selectionValues(type)).toEqual({ choice: "board" });
  });

  it("allows temporary selection while vault is unknown without claiming it saved", async () => {
    const onSelect = vi.fn();
    const initialProps: VaultProps = { vaultKey: null };

    const { result, rerender } = renderHook(
      ({ vaultKey }: VaultProps) => useViewSelection({ target, vaultKey, onSelect }),
      { initialProps },
    );

    act(() => result.current.select("builtin:overview"));
    expect(result.current.choice?.id).toBe("builtin:overview");
    expect(onSelect).toHaveBeenCalledWith("builtin:overview");
    expect(window.localStorage.length).toBe(0);
    rerender({ vaultKey: "vault" });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.choice?.id).toBe("dashboard");
    act(() => result.current.select("builtin:overview"));
    await waitFor(() => expect(result.current.pending).toBe(false));
    expect(
      server.read({ viewId: "$selection", context: { kind: "group", group: "Delivery" } }).values,
    ).toEqual({ choice: "builtin:overview" });
    expect(window.localStorage.length).toBe(0);
  });

  it("keeps scope-less node choices usable and isolated until a concrete context is known", () => {
    const onSelect = vi.fn();

    const { result, rerender } = renderHook(
      ({ subject }) =>
        useViewSelection({ target: noteTarget, vaultKey: "vault", subject, onSelect }),
      { initialProps: { subject: "one.html" } },
    );

    act(() => result.current.select("builtin:source"));
    expect(result.current.choice?.id).toBe("builtin:source");
    expect(onSelect).toHaveBeenCalledWith("builtin:source");
    expect(result.current.pending).toBe(false);
    expect(http.requests("GET", "/api/v1/view-preferences")).toHaveLength(0);
    expect(http.requests("PATCH", "/api/v1/view-preferences")).toHaveLength(0);
    rerender({ subject: "two.html" });
    expect(result.current.choice?.id).toBe("builtin:read");
    rerender({ subject: "one.html" });
    expect(result.current.choice?.id).toBe("builtin:source");
  });

  it.each(["", "*"])(
    "waits for a concrete subject before reading selection for %j",
    async (name) => {
      const { result, rerender } = renderHook(
        ({ subjectName }) =>
          useViewSelection({
            target: { ...target, kind: "type", name: subjectName },
            vaultKey: "vault",
          }),
        { initialProps: { subjectName: name } },
      );

      expect(http.requests("GET", "/api/v1/view-preferences")).toEqual([]);
      rerender({ subjectName: "Task" });
      await waitFor(() => expect(result.current.loading).toBe(false));
      expect(http.requests("GET", "/api/v1/view-preferences")).toHaveLength(1);
    },
  );
});
