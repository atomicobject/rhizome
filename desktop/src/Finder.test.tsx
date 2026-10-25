import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Discovery, FinderModel, FinderRow, Library, Repository, Worktree } from "./api";
import { Finder, fuzzy, rank } from "./Finder";
import { repositoryRows, worktreeRows } from "./Switcher";

const row = (title: string, detail: string, extra: Partial<FinderRow> = {}): FinderRow => ({
  repository: title,
  title,
  detail,
  runtime: "stopped",
  ...extra,
});
const rows = [
  row("rhizome", "/Users/drew/dev/rhizome", { current: true, runtime: "running" }),
  row("drews-vault", "/Volumes/notes"),
  row("harness-lab", "/Users/drew/dev/harness-lab"),
];
const model = (extra: Partial<FinderModel> = {}): FinderModel => ({
  mode: "repositories",
  opened: 1,
  theme: "graphite",
  rows,
  ...extra,
});

function show(extra: Partial<FinderModel> = {}) {
  const onChoose = vi.fn();
  render(<Finder model={model(extra)} onChoose={onChoose} />);
  const input = screen.getByRole("combobox");
  const options = () => within(screen.getByRole("listbox")).queryAllByRole("option");
  const active = () =>
    document.getElementById(input.getAttribute("aria-activedescendant") ?? "")?.textContent;
  return { onChoose, input, options, active };
}

describe("matching", () => {
  it("matches characters in order, smart-case, preferring word starts and runs", () => {
    expect(fuzzy("hrn", "harness")?.at).toEqual([0, 2, 3]);
    expect(fuzzy("nrh", "harness")).toBeNull();
    expect(fuzzy("Harn", "harness")).toBeNull();
    expect(fuzzy("harn", "Harness")).not.toBeNull();
    expect(fuzzy("fs", "finder-spike")!.score).toBeGreaterThan(fuzzy("fs", "fixes")!.score);
  });

  it("ranks title matches over path matches and keeps the given order on ties", () => {
    expect(rank(rows, "").map((r) => r.row.title)).toEqual([
      "rhizome",
      "drews-vault",
      "harness-lab",
    ]);
    expect(rank(rows, "volumes").map((r) => r.row.title)).toEqual(["drews-vault"]);
    // "/dev/" is in two paths and no title; both match by path, in order.
    expect(rank(rows, "/dev/").map((r) => [r.row.title, r.detail?.length])).toEqual([
      ["rhizome", 5],
      ["harness-lab", 5],
    ]);
    expect(rank(rows, "h").map((r) => r.row.title)[0]).toBe("harness-lab");
  });
});

describe("rows", () => {
  const repository = (id: string, lastOpened?: number): Repository => ({
    id,
    root: `/w/${id}/.git`,
    name: id,
    acknowledged: [`/w/${id}`],
    lastOpened,
  });
  const worktree = (path: string, branch: string): Worktree => ({
    path,
    branch,
    configured: true,
    hasDatabase: true,
    trusted: true,
    trustRequired: false,
  });

  it("lists repositories most recently opened first", () => {
    const library: Library = {
      version: 3,
      revision: 1,
      repositories: [repository("old", 1), repository("never"), repository("new", 9)],
    };
    expect(repositoryRows(library, {}, {}, "old").map((r) => [r.title, r.current])).toEqual([
      ["new", false],
      ["old", true],
      ["never", false],
    ]);
  });

  it("pins the primary worktree first and marks new and sleeping ones", () => {
    const info: Discovery = {
      id: "r",
      root: "/w/r/.git",
      name: "r",
      git: true,
      defaultPrimary: "/w/r",
      primary: "/w/r-main",
      worktrees: [worktree("/w/r", "dev"), worktree("/w/r-main", "main"), worktree("/w/r-x", "x")],
    };
    const rows = worktreeRows(
      { ...repository("r"), acknowledged: ["/w/r", "/w/r-main"] },
      info,
      { "/w/r": { state: "running", mode: "headless" } },
      "/w/r-x",
      true,
    );
    expect(rows.map((r) => [r.title, r.primary, r.fresh, r.runtime])).toEqual([
      ["main", true, false, "stopped"],
      ["dev", false, false, "running"],
      ["x", false, true, "sleeping"],
    ]);
  });
});

describe("finder", () => {
  it("starts on the first row this window does not show and opens it", () => {
    const { input, active, onChoose } = show();
    expect(input).toHaveFocus();
    expect(active()).toContain("drews-vault");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChoose).toHaveBeenCalledWith({
      choice: "open",
      repository: "drews-vault",
      worktree: null,
      newWindow: false,
    });
  });

  it("moves with the arrows, filters as the user types, and opens a new window with ⌘Enter", () => {
    const { input, active, options, onChoose } = show();
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(active()).toContain("harness-lab");
    fireEvent.keyDown(input, { key: "ArrowUp" });
    fireEvent.keyDown(input, { key: "ArrowUp" });
    fireEvent.keyDown(input, { key: "ArrowUp" });
    expect(active()).toContain("rhizome");
    fireEvent.change(input, { target: { value: "hrn" } });
    expect(options()).toHaveLength(1);
    expect(options()[0]).toHaveAttribute("aria-selected", "true");
    expect(options()[0].querySelectorAll("mark")).toHaveLength(3);
    fireEvent.keyDown(input, { key: "Enter", metaKey: true });
    expect(onChoose).toHaveBeenLastCalledWith({
      choice: "open",
      repository: "harness-lab",
      worktree: null,
      newWindow: true,
    });
    fireEvent.change(input, { target: { value: "zzz" } });
    expect(screen.getByText("No repository matches “zzz”.")).toBeVisible();
  });

  it("opens the first nine repositories with ⌘1–⌘9", () => {
    const { input, onChoose } = show();
    expect(screen.getByText("⌘3")).toBeVisible();
    fireEvent.keyDown(input, { key: "3", metaKey: true });
    expect(onChoose).toHaveBeenCalledWith(
      expect.objectContaining({ repository: "harness-lab", newWindow: false }),
    );
    fireEvent.keyDown(input, { key: "4", metaKey: true });
    expect(onChoose).toHaveBeenCalledTimes(1);
  });

  it("closes on Esc or a click outside, and offers Add repository and row actions", () => {
    const { input, onChoose } = show();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(onChoose).toHaveBeenLastCalledWith({ choice: "close" });
    fireEvent.mouseDown(screen.getByRole("dialog").parentElement!);
    expect(onChoose).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole("button", { name: "Add repository…" }));
    expect(onChoose).toHaveBeenLastCalledWith({ choice: "add" });
    fireEvent.click(screen.getByRole("button", { name: "Actions for harness-lab" }));
    expect(onChoose).toHaveBeenLastCalledWith(
      expect.objectContaining({ choice: "menu", repository: "harness-lab" }),
    );
  });

  it("leaves Enter on a button to the button and opens row actions with →", () => {
    const { input, onChoose } = show();
    fireEvent.keyDown(screen.getByRole("button", { name: "Add repository…" }), { key: "Enter" });
    expect(onChoose).not.toHaveBeenCalled();

    // The highlighted row starts on the first one this window does not show.
    fireEvent.keyDown(input, { key: "ArrowRight" });
    expect(onChoose).toHaveBeenLastCalledWith(
      expect.objectContaining({ choice: "menu", repository: "drews-vault" }),
    );

    // Mid-query, → moves the caret instead.
    fireEvent.change(input, { target: { value: "dr" } });
    (input as HTMLInputElement).setSelectionRange(1, 1);
    fireEvent.keyDown(input, { key: "ArrowRight" });
    expect(onChoose).toHaveBeenCalledTimes(1);
  });

  it("keeps the highlighted repository when live updates reorder the rows", () => {
    const onChoose = vi.fn();
    const first = model();
    const { rerender } = render(<Finder model={first} onChoose={onChoose} />);
    // Another window opens harness-lab, which moves to the top.
    const [current, vault, lab] = first.rows;
    rerender(<Finder model={{ ...first, rows: [lab, current, vault] }} onChoose={onChoose} />);
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Enter" });
    expect(onChoose).toHaveBeenLastCalledWith(
      expect.objectContaining({ choice: "open", repository: "drews-vault" }),
    );
  });

  it("highlights the first worktree once discovery fills an empty list", () => {
    const onChoose = vi.fn();
    const empty = model({ mode: "worktrees", rows: [] });
    const { rerender } = render(<Finder model={empty} onChoose={onChoose} />);
    const input = screen.getByRole("combobox");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    const found = [row("main", "/w/r", { worktree: "/w/r" })];
    rerender(<Finder model={{ ...empty, rows: found }} onChoose={onChoose} />);
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChoose).toHaveBeenLastCalledWith(
      expect.objectContaining({ choice: "open", worktree: "/w/r" }),
    );
  });

  it("lists worktrees by branch without repository-only controls", () => {
    const { options } = show({
      mode: "worktrees",
      rows: [
        row("main", "/w/r", { worktree: "/w/r", primary: true, current: true }),
        row("feature/x", "/w/r-x", { worktree: "/w/r-x", fresh: true }),
      ],
    });
    expect(screen.getByRole("dialog", { name: "Switch worktree" })).toBeVisible();
    expect(options()[0]).toHaveTextContent("primary");
    expect(within(options()[1]).getByTitle("New")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Add repository…" })).toBeNull();
    expect(screen.queryByText("⌘1")).toBeNull();
  });
});
