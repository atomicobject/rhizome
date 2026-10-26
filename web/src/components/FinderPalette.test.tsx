import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { FinderPalette, type FinderPaletteItem, highlightParts } from "./FinderPalette";

const row = (title: string, path = `notes/${title.toLowerCase()}.md`): FinderPaletteItem => ({
  id: path,
  kind: "note",
  title,
  path,
  titleMatches: [],
  pathMatches: [],
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => (resolve = done));

  return { promise, resolve };
}

function Harness({
  search,
  onOpen = () => {},
}: {
  search: (query: string, signal: AbortSignal) => Promise<FinderPaletteItem[]>;
  onOpen?: (item: FinderPaletteItem, options: { beside: boolean }) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Find
      </button>
      <FinderPalette
        open={open}
        label="Find a note or file"
        placeholder="Find…"
        recentHeading="Recent"
        search={search}
        onOpen={onOpen}
        onClose={() => setOpen(false)}
        emptyText={(query) => `No notes or files match “${query}”`}
      />
    </>
  );
}

describe("FinderPalette", () => {
  const proto = HTMLDialogElement.prototype;

  const original = {
    showModal: Object.getOwnPropertyDescriptor(proto, "showModal"),
    close: Object.getOwnPropertyDescriptor(proto, "close"),
  };

  beforeEach(() => {
    // jsdom has no modal dialogs; these prove our handlers, not native focus trapping.
    Object.defineProperty(proto, "showModal", {
      configurable: true,
      value(this: HTMLDialogElement) {
        this.open = true;
      },
    });
    Object.defineProperty(proto, "close", {
      configurable: true,
      value(this: HTMLDialogElement) {
        this.open = false;
      },
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

  it("moves with the arrows, opens with Enter or beside with ⌘Enter, and returns focus", async () => {
    const onOpen = vi.fn();
    const search = vi.fn(async () => [row("Alpha"), row("Beta"), row("Gamma")]);

    render(<Harness search={search} onOpen={onOpen} />);
    const trigger = screen.getByRole("button", { name: "Find" });

    trigger.focus();
    fireEvent.click(trigger);
    const input = screen.getByRole("combobox", { name: "Find a note or file" });

    expect(input).toHaveFocus();
    await screen.findByRole("option", { name: /Alpha/ });
    expect(screen.getByText("Recent")).toBeInTheDocument();
    const active = () => document.getElementById(input.getAttribute("aria-activedescendant")!);

    expect(active()).toHaveTextContent("Alpha");
    expect(active()).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(active()).toHaveTextContent("Gamma");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(active()).toHaveTextContent("Alpha");
    fireEvent.keyDown(input, { key: "ArrowUp" });
    expect(active()).toHaveTextContent("Gamma");

    fireEvent.keyDown(input, { key: "Enter", metaKey: true });
    expect(onOpen).toHaveBeenLastCalledWith(expect.objectContaining({ title: "Gamma" }), {
      beside: true,
    });
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(trigger).toHaveFocus();

    fireEvent.click(trigger);
    await screen.findByRole("option", { name: /Alpha/ });
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
    expect(onOpen).toHaveBeenLastCalledWith(expect.objectContaining({ title: "Alpha" }), {
      beside: false,
    });
  });

  it("closes on Escape and returns focus to where it was", async () => {
    render(<Harness search={async () => [row("Alpha")]} />);
    const trigger = screen.getByRole("button", { name: "Find" });

    trigger.focus();
    fireEvent.click(trigger);
    await screen.findByRole("option", { name: /Alpha/ });
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(trigger).toHaveFocus();
  });

  it("aborts superseded searches and never shows an older response over a newer one", async () => {
    const pending = new Map<string, ReturnType<typeof deferred<FinderPaletteItem[]>>>();
    const signals = new Map<string, AbortSignal>();

    const search = (query: string, signal: AbortSignal) => {
      const next = deferred<FinderPaletteItem[]>();

      pending.set(query, next);
      signals.set(query, signal);

      return next.promise;
    };

    render(<Harness search={search} />);
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    const input = screen.getByRole("combobox");

    expect(screen.getByRole("status")).toHaveTextContent("Searching…");
    fireEvent.change(input, { target: { value: "sc" } });
    fireEvent.change(input, { target: { value: "sch" } });
    expect(signals.get("")?.aborted).toBe(true);
    expect(signals.get("sc")?.aborted).toBe(true);
    expect(signals.get("sch")?.aborted).toBe(false);

    await act(async () => pending.get("sch")!.resolve([row("Schema")]));
    await screen.findByRole("option", { name: /Schema/ });
    // A slow, superseded response arriving late changes nothing.
    await act(async () => pending.get("sc")!.resolve([row("Scope")]));
    expect(screen.queryByRole("option", { name: /Scope/ })).toBeNull();
    expect(screen.getByRole("option", { name: /Schema/ })).toBeInTheDocument();

    fireEvent.change(input, { target: { value: "zzz" } });
    await act(async () => pending.get("zzz")!.resolve([]));
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("No notes or files match “zzz”"),
    );
  });

  it("highlights matches and keeps both ends of a long path", () => {
    expect(highlightParts("Schema Guide", [[0, 3]])).toEqual([
      { text: "Sch", mark: true },
      { text: "ema Guide", mark: false },
    ]);

    const path = "docs/specs/experience/very/deep/folder/workbench-redesign.md";

    const parts = highlightParts(
      path,
      [
        [0, 4],
        [path.length - 11, path.length - 3],
      ],
      24,
    );

    const text = parts.map((part) => part.text).join("");

    expect(text).toHaveLength(24);
    expect(text.startsWith("docs/")).toBe(true);
    expect(text.endsWith("redesign.md")).toBe(true);
    expect(text).toContain("…");
    expect(parts.filter((part) => part.mark).map((part) => part.text)).toEqual([
      "docs",
      "redesign",
    ]);
  });
  it("opens the current query's first result when Enter beats its response", async () => {
    const later = deferred<FinderPaletteItem[]>();
    const onOpen = vi.fn();

    const search = vi.fn((query: string) =>
      query === "" ? Promise.resolve([row("Old")]) : later.promise,
    );

    render(<Harness search={search} onOpen={onOpen} />);
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    await screen.findByRole("option", { name: /Old/ });

    const input = screen.getByRole("combobox", { name: "Find a note or file" });

    fireEvent.change(input, { target: { value: "new" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(onOpen).not.toHaveBeenCalled();

    await act(async () => later.resolve([row("New")]));

    await waitFor(() =>
      expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ title: "New" }), {
        beside: false,
      }),
    );
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("leaves Enter to an input method that is composing", async () => {
    const onOpen = vi.fn();

    render(<Harness search={() => Promise.resolve([row("Alpha")])} onOpen={onOpen} />);
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    await screen.findByRole("option", { name: /Alpha/ });

    fireEvent.keyDown(screen.getByRole("combobox", { name: "Find a note or file" }), {
      key: "Enter",
      isComposing: true,
    });

    expect(onOpen).not.toHaveBeenCalled();
  });
  it("drops a pending Enter when the query changes again", async () => {
    const pending = deferred<FinderPaletteItem[]>();
    const onOpen = vi.fn();

    const search = vi.fn((query: string) => {
      if (query === "") return Promise.resolve([row("Old")]);

      if (query === "ne") return pending.promise;

      return Promise.resolve([row("Newer")]);
    });

    render(<Harness search={search} onOpen={onOpen} />);
    fireEvent.click(screen.getByRole("button", { name: "Find" }));
    await screen.findByRole("option", { name: /Old/ });

    const input = screen.getByRole("combobox", { name: "Find a note or file" });

    fireEvent.change(input, { target: { value: "ne" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.change(input, { target: { value: "new" } });
    await screen.findByRole("option", { name: /Newer/ });

    expect(onOpen).not.toHaveBeenCalled();
  });
});
