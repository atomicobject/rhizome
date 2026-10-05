import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { isJsonObject } from "../api/parse";
import type { ViewExecuteResponse, ViewSaveRequest } from "../api/types";
import { decodeJSONBody, deferredReply, withFakeFetch } from "../test/fakeFetch";
import { ConfiguredView } from "./ConfiguredView";
import type { ViewContext } from "../views/context";
import { nativePreferenceScope } from "../viewPreferences/native";
import { clearPreferenceStores, getPreferenceStore } from "../viewPreferences/store";
import { preferenceServer } from "../viewPreferences/testServer";
import { baseView, boardExecution, execution, renderView } from "./ConfiguredView.testFixtures";

const VAULT = "vault-a";

const TYPE: ViewContext = { kind: "type", type: "ProductSpec" };

const SAVE_PATH = `/api/v1/views/${encodeURIComponent(baseView.id)}/save`;

function grouped(fingerprint: string, extraRow = false): ViewExecuteResponse {
  const result = execution();

  return {
    ...result,
    executionFingerprint: fingerprint,
    rows: extraRow
      ? [...result.rows, { ...result.rows[1], ref: { notePath: "c.md", kind: "NOTE" } }]
      : result.rows,
    // execution() returns a fresh state, whose generated type takes no literal.
    state: Object.assign(result.state, { group: { field: "frontmatter.spec-status" } }),
    groups: [
      {
        field: "frontmatter.spec-status",
        key: "frontmatter.spec-status=draft",
        label: "Draft",
        value: "draft",
        count: extraRow ? 2 : 1,
        depth: 0,
        rowStart: 1,
        rowEnd: extraRow ? 3 : 2,
        collapsedByDefault: true,
      },
    ],
  };
}

const draft = () => screen.getByRole("button", { name: /^Draft/ });

function isSaveRequest(value: unknown): value is ViewSaveRequest {
  return isJsonObject(value) && isJsonObject(value.state);
}

/** A reload: the window's stores go, and the next mount reads the server again. */
function reload(unmount: () => void) {
  unmount();
  clearPreferenceStores();
}

afterEach(() => clearPreferenceStores());

describe("remembered view preferences", () => {
  const http = withFakeFetch();

  it("keeps a group's expansion through new executions, content changes, and a reload", async () => {
    const server = preferenceServer(http);
    const scope = nativePreferenceScope(baseView, TYPE);
    const first = renderView({ execution: grouped("a"), vaultKey: VAULT, context: TYPE });

    await waitFor(() => expect(draft()).toHaveAttribute("aria-expanded", "false"));
    fireEvent.click(draft());
    expect(draft()).toHaveAttribute("aria-expanded", "true");
    await waitFor(() => expect(server.read(scope).values).toHaveProperty("native.tableGroups"));

    // A staged edit or refresh brings a new fingerprint and different rows.
    first.rerender(
      <ConfiguredView
        view={baseView}
        execution={grouped("b", true)}
        loading={false}
        error={null}
        state={{ page: { offset: 0, first: 25 } }}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
        vaultKey={VAULT}
        context={TYPE}
      />,
    );
    expect(draft()).toHaveAttribute("aria-expanded", "true");

    reload(first.unmount);
    renderView({ execution: grouped("c", true), vaultKey: VAULT, context: TYPE });
    await waitFor(() => expect(draft()).toHaveAttribute("aria-expanded", "true"));
  });

  it("keeps each mount context's choices apart", async () => {
    preferenceServer(http);
    const typed = renderView({ execution: grouped("a"), vaultKey: VAULT, context: TYPE });

    await waitFor(() => expect(draft()).toHaveAttribute("aria-expanded", "false"));
    fireEvent.click(draft());
    reload(typed.unmount);

    renderView({
      execution: grouped("a"),
      vaultKey: VAULT,
      context: { kind: "group", group: "Planning" },
    });
    await waitFor(() => expect(draft()).toHaveAttribute("aria-expanded", "false"));
  });

  it("remembers board columns and resets every personal choice to the shared defaults", async () => {
    const server = preferenceServer(http);
    const scope = nativePreferenceScope(baseView, TYPE);

    const board = {
      ...baseView,
      availableVariants: ["table", "kanban"],
      defaults: { variant: "kanban" },
      variants: { ...baseView.variants, kanban: { columnField: "frontmatter.spec-status" } },
    };

    renderView({
      view: board,
      execution: { ...boardExecution(), view: board },
      state: { variant: "kanban" },
      vaultKey: VAULT,
      context: TYPE,
    });

    fireEvent.click(await screen.findByRole("button", { name: "Collapse Planned column" }));
    await waitFor(() => expect(server.read(scope).values).toHaveProperty("native.boardColumns"));
    expect(screen.getByRole("button", { name: /^Expand Planned column/ })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Reset to shared" }));
    expect(await screen.findByRole("button", { name: "Collapse Planned column" })).toBeVisible();
    await waitFor(() => expect(server.read(scope).values).toEqual({}));
    expect(screen.queryByRole("button", { name: "Reset to shared" })).toBeNull();
  });

  it("shows a failed write with a retry that saves it", async () => {
    const server = preferenceServer(http);
    const scope = nativePreferenceScope(baseView, TYPE);
    renderView({ execution: grouped("a"), vaultKey: VAULT, context: TYPE });

    await waitFor(() => expect(draft()).toHaveAttribute("aria-expanded", "false"));
    server.failWrites("disk full");
    fireEvent.click(draft());

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Your view preferences are not saved.",
    );
    // The choice still applies while it waits.
    expect(draft()).toHaveAttribute("aria-expanded", "true");

    server.failWrites(null);
    fireEvent.click(within(screen.getByRole("alert")).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(server.read(scope).values).toHaveProperty("native.tableGroups"));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });
});

describe("Save view shared configuration", () => {
  const http = withFakeFetch();
  const view = { ...baseView, defaults: { variant: "table", search: "authored" } };
  const scope = nativePreferenceScope(view, TYPE);
  const sort = [{ field: "title", direction: "desc" }];

  function renderSaving(state = {}) {
    renderView({
      view,
      execution: { ...grouped("a"), view, definitionFingerprint: "fp-1" },
      state: { variant: "table", sort, search: "draft notes", ...state },
      vaultKey: VAULT,
      context: TYPE,
    });
  }

  const openReview = async () => {
    fireEvent.click(await screen.findByRole("button", { name: "Save view, unsaved changes" }));

    return screen.getByRole("dialog", { name: "Save shared view configuration" });
  };

  const savedBody = () => decodeJSONBody(http.requests("POST", SAVE_PATH)[0], isSaveRequest);

  it("lists the shared settings and keeps a temporary search out unless included", async () => {
    preferenceServer(http);
    http.json("POST", SAVE_PATH, {
      id: view.id,
      path: ".rhizome/views/specs.yaml",
      created: false,
    });
    renderSaving();

    const review = await openReview();
    expect(review).toHaveTextContent(".rhizome/views");
    expect(within(review).getByRole("listitem")).toHaveTextContent("Sort");
    expect(
      within(review).getByRole("checkbox", { name: "Include search “draft notes”" }),
    ).not.toBeChecked();

    fireEvent.click(within(review).getByRole("button", { name: "Write view YAML" }));
    expect(await screen.findByText(".rhizome/views/specs.yaml")).toBeVisible();
    expect(savedBody().state).toEqual(expect.objectContaining({ search: "authored", sort }));
  });

  it("saves an included search", async () => {
    preferenceServer(http);
    http.json("POST", SAVE_PATH, {
      id: view.id,
      path: ".rhizome/views/specs.yaml",
      created: false,
    });
    renderSaving();

    const review = await openReview();
    fireEvent.click(within(review).getByRole("checkbox", { name: "Include search “draft notes”" }));
    fireEvent.click(within(review).getByRole("button", { name: "Write view YAML" }));

    await waitFor(() => expect(savedBody().state.search).toBe("draft notes"));
  });

  it("clears only promoted overrides after the write, keeping widths and expansion", async () => {
    const server = preferenceServer(http);
    server.change(scope, {
      "native.sort": sort,
      "native.density": "one-line",
      "native.widths": { title: 240 },
      "native.tableGroups": {
        '["frontmatter.spec-status"]': { "frontmatter.spec-status=draft": true },
      },
    });
    http.json("POST", SAVE_PATH, {
      id: view.id,
      path: ".rhizome/views/specs.yaml",
      created: false,
    });
    renderSaving();

    fireEvent.click(within(await openReview()).getByRole("button", { name: "Write view YAML" }));

    await waitFor(() => expect(server.read(scope).values).not.toHaveProperty("native.sort"));
    expect(Object.keys(server.read(scope).values).sort()).toEqual([
      "native.tableGroups",
      "native.widths",
    ]);
  });

  it("keeps every override when the write fails, and one changed while it was in flight", async () => {
    const server = preferenceServer(http);
    server.change(scope, { "native.sort": sort, "native.widths": { title: 240 } });
    const reply = deferredReply<unknown>();
    http.on("POST", SAVE_PATH, () => reply.promise);
    renderSaving();

    fireEvent.click(within(await openReview()).getByRole("button", { name: "Write view YAML" }));
    const newer = [{ field: "updatedAt", direction: "asc" }];
    await getPreferenceStore(scope, VAULT).patch({ set: { "native.sort": newer } });
    reply.resolve({ id: view.id, path: ".rhizome/views/specs.yaml", created: false });

    await screen.findByText(".rhizome/views/specs.yaml");
    await waitFor(() => expect(getPreferenceStore(scope, VAULT).getSnapshot().pending).toBe(false));
    // The newer sort is not what the file holds, and widths are never promoted.
    expect(server.read(scope).values).toEqual({
      "native.sort": newer,
      "native.widths": { title: 240 },
    });

    http.json("POST", SAVE_PATH, { error: "invalid view" }, 422);
    fireEvent.click(within(await openReview()).getByRole("button", { name: "Write view YAML" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not save view");
    expect(server.read(scope).values).toEqual({
      "native.sort": newer,
      "native.widths": { title: 240 },
    });
  });

  it("carries every unpromoted setting to the view a generated view saves as", async () => {
    const server = preferenceServer(http);
    const widths = { title: 240 };

    const personal = {
      "native.widths": widths,
      "native.reshown": ["frontmatter.spec-status"],
      // The view's own density, so the file does not take it and the choice stays personal.
      "native.density": "two-line",
      "native.tableGroups": {
        '["frontmatter.spec-status"]': { "frontmatter.spec-status=draft": true },
      },
    };

    server.change(scope, { ...personal, "native.sort": sort, "native.filters": [] });
    server.change({ ...scope, viewId: "specs.saved" }, { "native.widths": { title: 90 } });
    http.json("POST", SAVE_PATH, {
      id: "specs.saved",
      path: ".rhizome/views/specs.yaml",
      created: true,
    });
    renderSaving();

    fireEvent.click(within(await openReview()).getByRole("button", { name: "Write view YAML" }));

    await waitFor(() =>
      expect(server.read({ ...scope, viewId: "specs.saved" }).values).toEqual({
        ...personal,
        "native.widths": { title: 90 },
      }),
    );
  });
});
