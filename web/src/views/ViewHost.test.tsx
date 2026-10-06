import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ViewCatalogEntry } from "../api/types";
import {
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  type ViewMessage,
} from "../lib/customViewMessages";
import { ViewHost, type ViewRuntimeProps } from "./ViewHost";

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
