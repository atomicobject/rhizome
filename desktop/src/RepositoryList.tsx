import { useState } from "react";
import type { Discovery, Failure, Repository, Runtime } from "./api";

type Found = { info?: Discovery; error?: Failure };
type Point = { x: number; y: number };
export type Runtimes = Record<string, Runtime>;

/** What the sidebar and worktree menu say about a runtime. A sleeping
 * runtime is one the user stopped while this window shows it. */
export const runtimeText = {
  running: { menu: " · Running", title: "Rhizome running" },
  starting: { menu: " · Starting…", title: "Rhizome starting" },
  stopped: { menu: "", title: "Rhizome not running" },
  sleeping: { menu: "", title: "Rhizome sleeping" },
};

export function newWorktrees(repository: Repository, found?: Discovery) {
  return found?.worktrees.filter((w) => !repository.acknowledged.includes(w.path)) ?? [];
}

/** Worktrees discovered since the repository was added and not opened yet.
 * A count in words, not a dot, so it never reads as a second runtime status. */
export function NewCount({ count }: { count: number }) {
  return (
    <span className="new-count" title={`${count} new ${count === 1 ? "worktree" : "worktrees"}`}>
      {/* The leading space keeps the count apart from the name in the tab's
          accessible name; flex layout drops it visually. */}
      {` ${count} new`}
    </span>
  );
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

/** Repository ids with `id` moved before or after `target`. */
function moved(repositories: Repository[], id: string, target: string, after: boolean) {
  const ids = repositories.map((r) => r.id).filter((other) => other !== id);
  ids.splice(ids.indexOf(target) + (after ? 1 : 0), 0, id);
  return ids;
}

/** The sidebar's repositories, reordered by drag or Alt+Up and Alt+Down. */
export function RepositoryList({
  repositories,
  found,
  runtimes,
  selected,
  sleeping,
  onSelect,
  onMenu,
  onReorder,
}: {
  repositories: Repository[];
  found: Record<string, Found>;
  runtimes: Runtimes;
  selected?: string;
  /** The repository whose worktree this window shows sleeping. */
  sleeping?: string;
  onSelect: (id: string) => void;
  onMenu: (repository: Repository, at: HTMLElement | Point) => void;
  onReorder: (ids: string[]) => void;
}) {
  const [dragging, setDragging] = useState<string | null>(null);
  const [dropAt, setDropAt] = useState<{ id: string; after: boolean } | null>(null);
  const move = (id: string, target: string, after: boolean) => {
    if (id !== target) onReorder(moved(repositories, id, target, after));
  };
  const endDrag = () => {
    setDragging(null);
    setDropAt(null);
  };

  return (
    <div role="tablist" aria-orientation="vertical">
      {repositories.map((repository, index) => {
        const state = found[repository.id];
        const fresh = newWorktrees(repository, state?.info).length;
        const runtime =
          sleeping === repository.id ? "sleeping" : repositoryState(runtimes, state?.info);
        const drop =
          dropAt?.id === repository.id ? (dropAt.after ? " drop-after" : " drop-before") : "";
        return (
          <div
            className={`repository-tab${dragging === repository.id ? " dragging" : ""}${drop}`}
            key={repository.id}
            draggable
            onContextMenu={(e) => {
              e.preventDefault();
              onMenu(repository, { x: e.clientX, y: e.clientY });
            }}
            onDragStart={(e) => {
              e.dataTransfer.effectAllowed = "move";
              e.dataTransfer.setData("text/plain", repository.name);
              setDragging(repository.id);
            }}
            onDragEnd={endDrag}
            onDragOver={(e) => {
              if (!dragging) return;
              e.preventDefault();
              const box = e.currentTarget.getBoundingClientRect();
              const after = e.clientY > box.top + box.height / 2;
              if (dropAt?.id !== repository.id || dropAt.after !== after)
                setDropAt({ id: repository.id, after });
            }}
            onDrop={(e) => {
              e.preventDefault();
              if (dragging && dropAt) move(dragging, dropAt.id, dropAt.after);
              endDrag();
            }}
          >
            <button
              role="tab"
              aria-selected={repository.id === selected}
              aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
              title={repository.root}
              onClick={() => onSelect(repository.id)}
              onKeyDown={(e) => {
                if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
                e.preventDefault();
                const down = e.key === "ArrowDown";
                const neighbor = repositories[index + (down ? 1 : -1)];
                if (neighbor) move(repository.id, neighbor.id, down);
              }}
            >
              <span className="name">{repository.name}</span>
              {state?.error && (
                <span className="tab-status warning">
                  {state.error.code === "folder_missing" ? "Missing" : "Error"}
                </span>
              )}
              {fresh > 0 && <NewCount count={fresh} />}
              {state?.info && (
                <span
                  className={`runtime-dot ${runtime}`}
                  role="img"
                  aria-label={runtimeText[runtime].title}
                  title={runtimeText[runtime].title}
                />
              )}
            </button>
            <button
              className="icon-button options"
              aria-label={`Options for ${repository.name}`}
              onClick={(e) => onMenu(repository, e.currentTarget)}
            >
              •••
            </button>
          </div>
        );
      })}
    </div>
  );
}
