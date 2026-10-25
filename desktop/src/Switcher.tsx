import { useEffect, useRef, useState } from "react";
import {
  request,
  type Discovery,
  type Failure,
  type FinderMode,
  type FinderModel,
  type FinderRow,
  type Library,
  type Repository,
  type Runtime,
  type Worktree,
} from "./api";
import type { ThemeName } from "../../web/src/lib/appearance";

// The shell's side of the finders: the toolbar's repository and worktree
// targets, the rows it builds from the library and presence it holds, and the
// overlay it shows them in (SPEC-0126 US9, US10).

type Found = { info?: Discovery; error?: Failure };
export type Runtimes = Record<string, Runtime>;

export function newWorktrees(repository: Repository, found?: Discovery) {
  return found?.worktrees.filter((w) => !repository.acknowledged.includes(w.path)) ?? [];
}

export function worktreeName(worktree: Worktree) {
  return worktree.branch || (worktree.head ? `detached ${worktree.head}` : basename(worktree.path));
}

export function basename(path: string) {
  return path.split("/").filter(Boolean).pop() ?? path;
}

/** The most active runtime state among a repository's worktrees. */
function repositoryState(runtimes: Runtimes, found?: Discovery) {
  const states = found?.worktrees.map((w) => runtimes[w.path]?.state) ?? [];
  return states.includes("running")
    ? "running"
    : states.includes("starting")
      ? "starting"
      : "stopped";
}

/** Known repositories, most recently opened first. */
export function repositoryRows(
  library: Library,
  found: Record<string, Found>,
  runtimes: Runtimes,
  current?: string,
  sleeping?: string,
): FinderRow[] {
  return [...library.repositories]
    .sort((a, b) => (b.lastOpened ?? 0) - (a.lastOpened ?? 0))
    .map((repository) => {
      const info = found[repository.id]?.info;
      return {
        repository: repository.id,
        title: repository.name,
        detail: info?.primary ?? repository.root.replace(/\/\.git$/, ""),
        runtime: sleeping === repository.id ? "sleeping" : repositoryState(runtimes, info),
        fresh: newWorktrees(repository, info).length > 0,
        current: repository.id === current,
      };
    });
}

/** One repository's worktrees with the primary pinned first. */
export function worktreeRows(
  repository: Repository,
  info: Discovery,
  runtimes: Runtimes,
  current?: string,
  sleeping?: boolean,
): FinderRow[] {
  const fresh = newWorktrees(repository, info).map((w) => w.path);
  const rows: FinderRow[] = info.worktrees.map((w) => ({
    repository: repository.id,
    worktree: w.path,
    title: worktreeName(w),
    detail: w.path,
    runtime: sleeping && w.path === current ? "sleeping" : (runtimes[w.path]?.state ?? "stopped"),
    fresh: fresh.includes(w.path),
    primary: w.path === info.primary,
    current: w.path === current,
  }));
  return [...rows.filter((r) => r.primary), ...rows.filter((r) => !r.primary)];
}

/** The finder this window shows. Its rows stay live while it is open, and it
 * returns focus to whatever had it when it opened. */
export function useFinder(
  theme: ThemeName,
  rows: (mode: FinderMode) => FinderRow[],
  onError: (cause: unknown) => void,
) {
  const [finder, setFinder] = useState<{ mode: FinderMode; opened: number } | null>(null);
  const toContent = useRef(false);
  const returnTo = useRef<HTMLElement | null>(null);
  // Each opening's stamp is newer than the last, even if the clock steps back.
  const stamp = useRef(0);
  // Numbers each model sent, so the app drops one that arrives after a newer one.
  const revision = useRef(0);
  const model: FinderModel | null = finder && { ...finder, theme, rows: rows(finder.mode) };
  const key = JSON.stringify(model);
  useEffect(() => {
    if (!model) return;
    revision.current += 1;
    void request("finder", {
      model,
      returnToContent: toContent.current,
      revision: revision.current,
    }).catch(onError);
  }, [key]);

  /** Opens a finder, switches to it from the other, or closes it when it shows. */
  function open(mode: FinderMode) {
    if (finder?.mode === mode) return close();
    if (!finder) {
      // Opened from the menu while the page had focus, it returns focus there.
      const focused = document.hasFocus();
      toContent.current = !focused;
      returnTo.current = focused ? (document.activeElement as HTMLElement | null) : null;
    }
    stamp.current = Math.max(Date.now(), stamp.current + 1);
    setFinder({ mode, opened: stamp.current });
  }

  /** Closes the finder. `toShell` keeps focus in the shell, which is about to
   * show something of its own; otherwise focus goes back where it was. */
  function close(toShell = false) {
    setFinder(null);
    // Naming the opening lets the app drop a show for it that arrives late.
    void request("finder", { model: null, opened: finder?.opened, toShell }).catch(() => {});
  }

  /** The finder closed itself after a choice; the app already hid it. */
  function closed() {
    setFinder(null);
    returnTo.current?.focus();
    returnTo.current = null;
  }

  return { shown: !!finder, open, close, closed };
}

/** The toolbar's repository and worktree, each opening its finder. The
 * worktree is quiet on the primary checkout and a pill anywhere else. */
export function Switcher({
  repository,
  info,
  worktree,
  onOpen,
  onMenu,
}: {
  repository?: Repository;
  info?: Discovery;
  worktree?: Worktree;
  onOpen: (mode: FinderMode) => void;
  onMenu: (at: { x: number; y: number }) => void;
}) {
  return (
    <>
      <button
        className="switch-repository"
        aria-haspopup="dialog"
        aria-keyshortcuts="Meta+O"
        aria-label={repository ? `Repository ${repository.name}` : "Choose a repository"}
        title={repository ? `${repository.root}\nSwitch repository (⌘O)` : "Switch repository (⌘O)"}
        onClick={() => onOpen("repositories")}
        onContextMenu={(event) => {
          event.preventDefault();
          if (repository) onMenu({ x: event.clientX, y: event.clientY });
        }}
      >
        <strong>{repository?.name ?? "Choose repository"}</strong>
        <kbd>⌘O</kbd>
      </button>
      {repository && (
        <button
          className={
            worktree && worktree.path !== info?.primary
              ? "switch-worktree other"
              : "switch-worktree"
          }
          aria-haspopup="dialog"
          aria-keyshortcuts="Meta+Shift+O"
          aria-label={`Worktree ${worktree ? worktreeName(worktree) : ""}`.trim()}
          title={worktree ? `${worktree.path}\nSwitch worktree (⇧⌘O)` : "Switch worktree (⇧⌘O)"}
          disabled={!info}
          onClick={() => onOpen("worktrees")}
        >
          <span className="branch">{worktree ? worktreeName(worktree) : "…"}</span>
          {newWorktrees(repository, info).length > 0 && (
            <span className="new-dot" title="New worktrees" />
          )}
          <kbd>⇧⌘O</kbd>
        </button>
      )}
    </>
  );
}
