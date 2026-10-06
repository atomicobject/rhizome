import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ViewCatalogEntry } from "../api/types";
import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  type ViewMessage,
} from "../lib/customViewMessages";
import { withFakeFetch } from "../test/fakeFetch";
import { preferenceServer } from "../viewPreferences/testServer";
import { ViewHost, hostPreferenceScope, type ViewRuntimeProps } from "./ViewHost";

const base: ViewRuntimeProps = {
  view: { id: "overview", name: "Overview" },
  choice: { id: "overview", name: "Overview", renderer: "overview" },
  context: { kind: "type", type: "Spec" },
  active: true,
  services: { session: null, onOpenNote: vi.fn(), onStageOps: async () => undefined },
};

const definition: ViewCatalogEntry = {
  id: "custom",
  name: "Custom",
  source: { kind: "custom", entry: "index.html" },
  mount: { kind: "type", type: "Spec" },
  variants: {},
  defaults: {},
  definition: {
    variants: {},
    apiVersion: "rhizome.view.v1",
    id: "custom",
    name: "Custom",
    source: { kind: "custom", entry: "index.html" },
    mount: { kind: "type", type: "Spec" },
  },
};

function Overview({ context, services }: ViewRuntimeProps) {
  return (
    <button onClick={() => services.onOpenNote("spec.md")}>
      {context.kind === "type" ? context.type : "Unknown"}
    </button>
  );
}

describe("shared host", () => {
  it("runs builtins through context and the same navigation services", () => {
    render(<ViewHost {...base} renderers={{ overview: Overview }} />);
    screen.getByRole("button", { name: "Spec" }).click();
    expect(base.services.onOpenNote).toHaveBeenCalledWith("spec.md");
  });
  it("unmounts inactive custom views while retaining builtins", () => {
    const { rerender } = render(
      <ViewHost
        {...base}
        definition={definition}
        choice={{ id: "custom", name: "Custom", renderer: "custom" }}
      />,
    );

    expect(screen.getByTitle("Custom")).toHaveAttribute("src", expect.stringContaining("context="));
    rerender(
      <ViewHost
        {...base}
        definition={definition}
        choice={{ id: "custom", name: "Custom", renderer: "custom" }}
        active={false}
      />,
    );
    expect(screen.queryByTitle("Custom")).toBeNull();
    rerender(<ViewHost {...base} renderers={{ overview: Overview }} active={false} />);
    expect(screen.getByRole("button", { name: "Spec" })).toBeVisible();
  });
  it("gives a custom frame the issues and collection services", () => {
    const onOpenIssues = vi.fn();
    const onSelectCollection = vi.fn();
    render(
      <ViewHost
        {...base}
        definition={definition}
        choice={{ id: "custom", name: "Custom", renderer: "custom" }}
        services={{ ...base.services, onOpenIssues, onSelectCollection }}
      />,
    );
    const source = screen.getByTitle<HTMLIFrameElement>("Custom").contentWindow;

    const send = (data: ViewMessage) =>
      window.dispatchEvent(
        new MessageEvent("message", { source, origin: window.location.origin, data }),
      );

    send({ type: OPEN_ISSUES_MESSAGE, scope: { kind: "interface", key: "Work" } });
    send({ type: OPEN_COLLECTION_MESSAGE, name: "Work" });
    expect(onOpenIssues).toHaveBeenCalledExactlyOnceWith({ kind: "interface", key: "Work" });
    expect(onSelectCollection).toHaveBeenCalledExactlyOnceWith("Work");
  });
});

describe("custom view preferences", () => {
  const http = withFakeFetch();

  const custom = {
    ...base,
    definition,
    choice: { id: "custom", name: "Custom", renderer: "custom" as const },
  };

  it("offers Reset to shared only once the view holds a personal override", async () => {
    const server = preferenceServer(http);
    const services = { ...base.services, vaultKey: "fresh-vault" };
    render(<ViewHost {...custom} services={services} />);

    await waitFor(() => expect(http.requests("GET", "/api/v1/view-preferences")).toHaveLength(1));
    expect(screen.queryByRole("button", { name: "Reset to shared" })).toBeNull();
    expect(server.read(hostPreferenceScope(custom)).values).toEqual({});
  });

  it("resets the view and its widget slots from the host", async () => {
    const server = preferenceServer(http);
    server.change(hostPreferenceScope(custom), { "overview.mode": "matrix" });
    const services = { ...base.services, vaultKey: "remembered-vault" };
    render(<ViewHost {...custom} services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Reset to shared" }));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Reset to shared" })).toBeNull(),
    );
    expect(server.read(hostPreferenceScope(custom)).values).toEqual({});
  });
});
