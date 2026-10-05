import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { NoteTab, Tab } from "./useNoteTabs";
import { NoteTabStrip } from "./NoteTabStrip";

const home: Tab = { id: "home", kind: "home" };

function note(path: string, options: { dirty?: boolean; title?: string } = {}): NoteTab {
  const tab: NoteTab = {
    id: `note:${path}`,
    kind: "note",
    path,
    dirty: options.dirty ?? false,
  };

  if (options.title !== undefined) tab.title = options.title;

  return tab;
}

function renderStrip(tabs: Tab[], activeId = "home") {
  return render(
    <NoteTabStrip tabs={tabs} activeId={activeId} onActivate={vi.fn()} onClose={vi.fn()} />,
  );
}

function ClosingStrip({ initialTabs, cancel = false }: { initialTabs: Tab[]; cancel?: boolean }) {
  const [tabs, setTabs] = useState(initialTabs);

  return (
    <NoteTabStrip
      tabs={tabs}
      activeId={tabs.at(-1)?.id ?? "home"}
      onActivate={vi.fn()}
      onClose={(id) => {
        if (!cancel) setTabs((current) => current.filter((tab) => tab.id !== id));
      }}
    />
  );
}

describe("NoteTabStrip", () => {
  afterEach(() => vi.restoreAllMocks());

  it("exposes regular tab names and dirty state", () => {
    renderStrip(
      [home, note("a.md", { title: "Alpha" }), note("b.md", { dirty: true, title: "Beta" })],
      "note:a.md",
    );

    expect(screen.getByRole("tablist", { name: "Open notes" })).toBeInTheDocument();

    const homeTab = screen.getByRole("tab", { name: "Home" });
    expect(homeTab).toHaveAttribute("aria-selected", "false");
    expect(homeTab).toHaveClass("notes-tab--pinned-home");
    expect(homeTab).toHaveAttribute("title", "Home");
    expect(homeTab).toHaveTextContent("");
    expect(homeTab.querySelector("svg.notes-tab__home-icon")).toHaveAttribute(
      "aria-hidden",
      "true",
    );

    const activeTab = screen.getByRole("tab", { name: "Alpha" });
    expect(activeTab).toHaveAttribute("aria-selected", "true");
    expect(activeTab).toHaveClass("is-active");
    expect(activeTab).not.toHaveClass("notes-tab--preview");

    const dirtyTab = screen.getByRole("tab", { name: "Beta, unsaved changes" });
    expect(dirtyTab).toHaveAttribute("aria-selected", "false");
    expect(dirtyTab).toHaveClass("is-dirty");
    expect(dirtyTab.querySelector(".notes-tab__dirty")).toHaveAttribute("aria-hidden", "true");
    expect(screen.getByRole("button", { name: "Close Beta" })).toBeInTheDocument();
  });

  it("labels collection tabs by their reusable workspace", () => {
    renderStrip([
      home,
      {
        id: "collection:issues",
        kind: "collection",
        collection: "issues",
        issueScope: null,
        issueKey: null,
      },
      { id: "collection:modified", kind: "collection", collection: "modified" },
    ]);

    expect(screen.getByRole("tab", { name: "Problems" })).toHaveClass("notes-tab--collection");
    expect(screen.getByRole("tab", { name: "Changes" })).toHaveClass("notes-tab--collection");
    expect(screen.getByRole("button", { name: "Close Problems" })).toBeInTheDocument();
  });

  it("activates and focuses tabs with Arrow, Home, and End keys", () => {
    const onActivate = vi.fn();
    const tabs: Tab[] = [home, note("a.md"), note("b.md"), note("c.md")];
    render(
      <NoteTabStrip tabs={tabs} activeId="note:a.md" onActivate={onActivate} onClose={vi.fn()} />,
    );

    const homeTab = screen.getByRole("tab", { name: "Home" });
    const firstTab = screen.getByRole("tab", { name: "a" });
    const secondTab = screen.getByRole("tab", { name: "b" });
    const lastTab = screen.getByRole("tab", { name: "c" });

    firstTab.focus();
    fireEvent.keyDown(firstTab, { key: "ArrowRight" });
    expect(onActivate).toHaveBeenLastCalledWith("note:b.md");
    expect(document.activeElement).toBe(secondTab);

    fireEvent.keyDown(secondTab, { key: "ArrowLeft" });
    expect(onActivate).toHaveBeenLastCalledWith("note:a.md");
    expect(document.activeElement).toBe(firstTab);

    fireEvent.keyDown(firstTab, { key: "Home" });
    expect(onActivate).toHaveBeenLastCalledWith("home");
    expect(document.activeElement).toBe(homeTab);

    fireEvent.keyDown(homeTab, { key: "End" });
    expect(onActivate).toHaveBeenLastCalledWith("note:c.md");
    expect(document.activeElement).toBe(lastTab);
  });

  it("closes a focused note on Delete or Backspace and focuses the nearest tab", () => {
    const frames: FrameRequestCallback[] = [];

    const requestAnimationFrame = vi
      .spyOn(window, "requestAnimationFrame")
      .mockImplementation((callback) => {
        frames.push(callback);

        return frames.length;
      });

    const tabs: Tab[] = [home, note("a.md"), note("b.md"), note("c.md")];
    render(<ClosingStrip initialTabs={tabs} />);

    const middleTab = screen.getByRole("tab", { name: "b" });
    middleTab.focus();
    fireEvent.keyDown(middleTab, { key: "Delete" });
    frames.shift()?.(0);
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "c" }));

    const lastTab = screen.getByRole("tab", { name: "c" });
    lastTab.focus();
    fireEvent.keyDown(lastTab, { key: "Backspace" });
    frames.shift()?.(0);
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "a" }));
    expect(requestAnimationFrame).toHaveBeenCalledTimes(2);
  });

  it("falls back to Home when Backspace closes the only note", () => {
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);

      return frames.length;
    });
    render(<ClosingStrip initialTabs={[home, note("only.md")]} />);

    const onlyTab = screen.getByRole("tab", { name: "only" });
    const homeTab = screen.getByRole("tab", { name: "Home" });
    onlyTab.focus();
    fireEvent.keyDown(onlyTab, { key: "Backspace" });
    frames.shift()?.(0);

    expect(document.activeElement).toBe(homeTab);
  });

  it("keeps focus on a tab when its dirty-close confirmation is canceled", () => {
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);

      return frames.length;
    });
    render(<ClosingStrip initialTabs={[home, note("dirty.md", { dirty: true })]} cancel />);

    const dirtyTab = screen.getByRole("tab", { name: "dirty, unsaved changes" });
    dirtyTab.focus();
    fireEvent.keyDown(dirtyTab, { key: "Delete" });
    frames.shift()?.(0);

    expect(document.activeElement).toBe(dirtyTab);
  });

  it("returns focus after closing the active tab with its close button", () => {
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      frames.push(callback);

      return frames.length;
    });
    render(<ClosingStrip initialTabs={[home, note("first.md"), note("last.md")]} />);

    fireEvent.click(screen.getByRole("button", { name: "Close last" }));
    frames.shift()?.(0);

    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "first" }));
  });
});
