// Hooks and components name the page they render in (SPEC-0136 R2): a hidden
// page's late navigation, param change, ready mark, edit, or read still
// belongs to it after another page is on screen.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  EDIT_SESSION_MESSAGE,
  OPEN_NODE_MESSAGE,
  OPEN_NOTE_MESSAGE,
  PANEL_TAB_ID,
  PARAMS_CHANGED_MESSAGE,
  PHASE_MESSAGE,
  STAGE_OPS_MESSAGE,
  type HostMessage,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import { jsonReply, withFakeFetch } from "../src/test/fakeFetch";
import type { ViewContext } from "../src/views/context";
import type { ViewParamDeclaration } from "../src/views/params";
import type { PageBinding } from "./pageBinding";

const http = withFakeFetch();

const parentDescriptor = Object.getOwnPropertyDescriptor(window, "parent")!;

afterEach(() => {
  Object.defineProperty(window, "parent", parentDescriptor);
  vi.restoreAllMocks();
  document.body.replaceChildren();
});

// A kit framed by a same-origin workspace, freshly imported so it sees that parent.
async function framedKit() {
  const host = document.createElement("iframe");
  document.body.append(host);
  const parent = host.contentWindow!;
  const sent = vi.spyOn(parent, "postMessage").mockImplementation(() => undefined);
  Object.defineProperty(window, "parent", { configurable: true, value: parent });
  vi.resetModules();
  const kit = await import("./index");
  const pages = await import("./pageBinding");
  const frame = await import("./experienceFrame");
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const answer = (data: HostMessage) =>
    window.dispatchEvent(
      new MessageEvent("message", { source: parent, origin: window.location.origin, data }),
    );

  const posted = (type: string) =>
    sent.mock.calls.map((call): ViewMessage => call[0]).filter((m) => m.type === type);

  const inPage = (binding: PageBinding, children: ReactNode) => (
    <QueryClientProvider client={client}>
      <pages.PageBindingContext value={binding}>{children}</pages.PageBindingContext>
    </QueryClientProvider>
  );

  return { kit, pages, frame, answer, posted, inPage };
}

const page = (
  tabId: string,
  navigationId: string,
  options: { declarations?: ViewParamDeclaration[]; context?: ViewContext } = {},
): PageBinding => ({
  tabId,
  navigationId,
  invocation: {
    view: { id: tabId, name: tabId },
    context: options.context ?? { kind: "standalone" },
    params: options.declarations ?? [],
  },
  params: {},
});

const ref = { notePath: "a.md", kind: "NOTE" } as const;

describe("a hidden page's navigation", () => {
  it("carries its own stamp and checks params against its own declarations", async () => {
    const { kit, pages, posted, inPage } = await framedKit();
    const a = page("tab-a", "n-a", { declarations: [{ name: "tab", kind: "string" }] });
    let navigation: ReturnType<typeof kit.usePageNavigation> | undefined;

    function Debounced() {
      navigation = kit.usePageNavigation();

      return null;
    }

    render(inPage(a, <Debounced />));
    // The user switched to tab B before A's debounced calls fired.
    pages.setActivePageBinding(page("tab-b", "n-b"));
    navigation!.setViewParams({ tab: "history" });
    navigation!.openNode(ref);

    expect(posted(PARAMS_CHANGED_MESSAGE)).toEqual([
      {
        type: PARAMS_CHANGED_MESSAGE,
        params: { tab: "history" },
        tabId: "tab-a",
        navigationId: "n-a",
      },
    ]);
    expect(posted(OPEN_NODE_MESSAGE)).toEqual([
      { type: OPEN_NODE_MESSAGE, ref, tabId: "tab-a", navigationId: "n-a" },
    ]);
    // The imperative export checks the page on screen, which declares no tab.
    expect(() => kit.setViewParams({ tab: "history" })).toThrow(/setViewParams/);
  });

  it("names its page from NodeLink and NoteLink while another page is on screen", async () => {
    const { kit, pages, posted, inPage } = await framedKit();

    render(
      inPage(
        page("tab-a", "n-a"),
        <>
          <kit.NodeLink node={ref}>node</kit.NodeLink>
          <kit.NoteLink path="b.md">note</kit.NoteLink>
        </>,
      ),
    );
    pages.setActivePageBinding(page("tab-b", "n-b"));
    fireEvent.click(screen.getByText("node"));
    fireEvent.click(screen.getByText("note"));

    expect(posted(OPEN_NODE_MESSAGE)).toMatchObject([{ tabId: "tab-a", navigationId: "n-a" }]);
    expect(posted(OPEN_NOTE_MESSAGE)).toMatchObject([{ tabId: "tab-a", navigationId: "n-a" }]);
  });

  it("marks its own page ready", async () => {
    const { kit, pages, frame, posted, inPage } = await framedKit();
    frame.reportPhasesToHost();
    let ready = () => {};

    function Loading() {
      ready = kit.useMarkReady();

      return null;
    }

    render(inPage(page("tab-a", "n-a"), <Loading />));
    pages.setActivePageBinding(page("tab-b", "n-b"));
    ready();

    expect(posted(PHASE_MESSAGE)).toMatchObject([
      { phase: "data-ready", tabId: "tab-a", navigationId: "n-a" },
    ]);
  });
});

describe("an edit made on a panel's earlier subject", () => {
  it("is stamped with the navigation it was made in", async () => {
    const { kit, answer, posted, inPage } = await framedKit();
    let setField: ReturnType<typeof kit.useSetField> | undefined;

    function Panel() {
      setField = kit.useSetField();

      return null;
    }

    const view = render(inPage(page(PANEL_TAB_ID, "p-a"), <Panel />));
    answer({ type: EDIT_SESSION_MESSAGE, session: null });
    const retained = setField!;
    // The panel's page updates in place for a new subject.
    view.rerender(inPage(page(PANEL_TAB_ID, "p-b"), <Panel />));

    await act(async () => {
      retained.mutate({ target: { notePath: "a.md" }, field: "status", value: "done" });
      void retained.mutateAsync({ target: { notePath: "a.md" }, field: "rank", value: "2" });
    });

    await vi.waitFor(() => expect(posted(STAGE_OPS_MESSAGE)).toHaveLength(2));
    expect(posted(STAGE_OPS_MESSAGE).map((message) => message.navigationId)).toEqual([
      "p-a",
      "p-a",
    ]);
  });
});

describe("a retained page's display group", () => {
  it("is the group its own page was opened for", async () => {
    const member = (name: string) => ({
      name,
      kind: "type",
      label: name,
      pluralLabel: `${name}s`,
      count: 1,
      issueCount: 0,
      implementors: [],
      children: [],
    });

    http.on("GET", "/api/v1/display-groups", () =>
      jsonReply({
        groups: [
          { name: "Delivery", members: [member("Spec")] },
          { name: "Knowledge", members: [member("Concept")] },
        ],
      }),
    );
    const { kit, pages, inPage } = await framedKit();

    function Group({ testId }: { testId: string }) {
      const { group } = kit.useDisplayGroup();

      return <p data-testid={testId}>{group?.name ?? "…"}</p>;
    }

    pages.setActivePageBinding(
      page("tab-b", "n-b", { context: { kind: "group", group: "Knowledge" } }),
    );
    render(
      inPage(
        page("tab-a", "n-a", { context: { kind: "group", group: "Delivery" } }),
        <Group testId="hidden" />,
      ),
    );

    await vi.waitFor(() => expect(screen.getByTestId("hidden").textContent).toBe("Delivery"));
  });
});
