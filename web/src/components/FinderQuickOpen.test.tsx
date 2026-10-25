import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { FinderQuickOpen, useFinderNoteRequests } from "./FinderQuickOpen";
import type { OpenMode } from "./useNoteTabs";

function NotesStub({ open }: { open: (path: string, mode: OpenMode) => void }) {
  useFinderNoteRequests(open);

  return null;
}

function renderQuickOpen(notes?: (path: string, mode: OpenMode) => void) {
  return renderWithQueryClient(
    <>
      {notes && <NotesStub open={notes} />}
      <FinderQuickOpen />
    </>,
  );
}

function sendFind() {
  act(() => {
    window.dispatchEvent(new CustomEvent("rhizome:desktop", { detail: { command: "find" } }));
  });
}

describe("FinderQuickOpen", () => {
  const http = withFakeFetch();
  const proto = HTMLDialogElement.prototype;

  const original = {
    showModal: Object.getOwnPropertyDescriptor(proto, "showModal"),
    close: Object.getOwnPropertyDescriptor(proto, "close"),
  };

  beforeEach(() => {
    for (const [name, open] of [
      ["showModal", true],
      ["close", false],
    ] as const) {
      Object.defineProperty(proto, name, {
        configurable: true,
        value(this: HTMLDialogElement) {
          this.open = open;
        },
      });
    }

    window.history.replaceState({}, "", "/notes");
    http.json("GET", "/api/v1/ontology/summary", {
      types: [{ name: "Spec", label: "Specification" }],
    });
    http.on("POST", "/api/v1/find/opens", () => new Response(null, { status: 204 }));
    http.json("GET", "/api/v1/find", {
      items: [
        {
          kind: "note",
          path: "docs/schema.md",
          title: "Schema",
          type: "Spec",
          score: 90,
          titleMatches: [[0, 6]],
          pathMatches: [],
        },
        {
          kind: "code",
          path: "pkg/schema.go",
          title: "schema.go",
          lang: "go",
          score: 80,
          titleMatches: [],
          pathMatches: [],
        },
      ],
    });
  });

  afterEach(() => {
    // Unmount while the dialog stand-ins are still installed.
    cleanup();

    for (const [name, descriptor] of Object.entries(original)) {
      if (descriptor) Object.defineProperty(proto, name, descriptor);
      else Reflect.deleteProperty(proto, name);
    }
  });

  it("opens on the platform shortcut without printing, and from the desktop find command", async () => {
    renderQuickOpen();
    const event = new KeyboardEvent("keydown", { key: "p", ctrlKey: true, cancelable: true });

    act(() => {
      window.dispatchEvent(event);
    });
    expect(event.defaultPrevented).toBe(true);
    expect(await screen.findByRole("combobox", { name: "Find a note or file" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Escape" });
    expect(screen.queryByRole("combobox")).toBeNull();

    sendFind();
    expect(await screen.findByRole("combobox")).toBeInTheDocument();
  });

  it("opens notes through the Notes workspace and code files in Explorer", async () => {
    const notes = vi.fn();

    renderQuickOpen(notes);
    sendFind();
    expect(
      await screen.findByRole("option", { name: /Schema.*Specification/ }),
    ).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter", ctrlKey: true });
    expect(notes).toHaveBeenCalledWith("docs/schema.md", "beside");
    await waitFor(() => expect(http.count("POST", "/api/v1/find/opens")).toBe(1));

    sendFind();
    await screen.findByRole("option", { name: /schema\.go/ });
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
    expect(window.location.pathname).toBe("/explorer");
    expect(new URLSearchParams(window.location.search).get("file")).toBe("pkg/schema.go");
  });

  it("opens a note by URL when the Notes workspace has not mounted", async () => {
    window.history.replaceState({}, "", "/explorer");
    renderQuickOpen();
    sendFind();
    await screen.findByRole("option", { name: /Schema.*Specification/ });
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
    expect(window.location.pathname).toBe("/notes");
    expect(new URLSearchParams(window.location.search).get("note")).toBe("docs/schema.md");
  });
});
