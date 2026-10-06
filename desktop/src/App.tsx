import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import {
  attach,
  failure,
  pickPath,
  request,
  type Discovery,
  type Failure,
  type GlobalInfo,
  type Library,
  type MenuEntry,
  type Message,
  type OpenState,
  type Repository,
  type Runtime,
  type Worktree,
} from "./api";
import { Settings } from "./Settings";
import { Notice, OpenStatus } from "./Status";

type Found = { info?: Discovery; error?: Failure };
type Item = Omit<MenuEntry, "id" | "items"> & { run?: () => void; items?: Item[] };
type Target = { repository?: string; worktree?: string; after: number };
type Runtimes = Record<string, Runtime>;

export function worktreeName(worktree: Worktree) {
  return worktree.branch || (worktree.head ? `detached ${worktree.head}` : basename(worktree.path));
}
function basename(path: string) {
  return path.split("/").filter(Boolean).pop() ?? path;
}
export function newWorktrees(repository: Repository, found?: Discovery) {
  return found?.worktrees.filter((w) => !repository.acknowledged.includes(w.path)) ?? [];
}
/** The most active runtime state among a repository's worktrees. */
function repositoryState(runtimes: Runtimes, found?: Discovery) {
  const states = found?.worktrees.map((w) => runtimes[w.path]?.state) ?? [];
  return states.includes("running")
    ? "running"
    : states.includes("starting")
      ? "starting"
      : undefined;
}
const runtimeLabel = { running: " · Running", starting: " · Starting…", stopped: "" };
const runtimeTitle = {
  running: "Rhizome running",
  starting: "Rhizome starting",
  stopped: "Rhizome sleeping",
};

export function App() {
  const [library, setStoredLibrary] = useState<Library | null>(null);
  const [loadError, setLoadError] = useState<Failure | null>(null);
  const [found, setFound] = useState<Record<string, Found>>({});
  const [runtimes, setRuntimes] = useState<Runtimes>({});
  const [selection, setSelection] = useState<{ repository?: string; worktree?: string }>({});
  const [collapsed, setCollapsed] = useState(false);
  const [restored, setRestored] = useState(false);
  const [settings, setSettings] = useState(false);
  const [global, setGlobal] = useState<GlobalInfo | null>(null);
  const [busy, setBusy] = useState("");
  const [alert, setAlert] = useState<Failure | null>(null);
  const [open, setOpen] = useState<OpenState | null>(null);
  const [dragging, setDragging] = useState<string | null>(null);
  const [dropAt, setDropAt] = useState<{ id: string; after: boolean } | null>(null);
  const target = useRef<Target>({ after: 0 });
  // Numbers each selection when the user makes it; only the newest may open.
  const selecting = useRef(0);
  const seen = useRef(0);
  const menus = useRef<Record<string, () => void>>({});
  const remembered = useRef<Record<string, string>>({});
  const handler = useRef<(message: Message) => void>(() => {});
  const region = useRef<HTMLDivElement>(null);
  const reported = useRef("");
  const revision = useRef(-1);

  /** Applies a library snapshot unless this shell already holds a newer one,
   * as when another window's later save arrived before this response. */
  function setLibrary(saved: Library) {
    if (saved.revision <= revision.current) return false;
    revision.current = saved.revision;
    setStoredLibrary(saved);
    return true;
  }

  const discover = useCallback(async (repository: Repository) => {
    try {
      const info = await request("discover", { id: repository.id });
      setFound((current) => ({ ...current, [repository.id]: { info } }));
      return info;
    } catch (cause) {
      setFound((current) => ({ ...current, [repository.id]: { error: failure(cause) } }));
      return undefined;
    }
  }, []);

  async function perform(label: string, task: () => Promise<void>) {
    setBusy(label);
    setAlert(null);
    try {
      await task();
    } catch (cause) {
      setAlert(failure(cause));
    } finally {
      setBusy("");
    }
  }

  async function openWorktree(
    id: string,
    worktree: string,
    skipSeed = false,
    ticket = ++selecting.current,
  ) {
    if (ticket !== selecting.current) return;
    remembered.current[id] = worktree;
    target.current = { repository: id, worktree, after: seen.current };
    setSelection({ repository: id, worktree });
    setSettings(false);
    setOpen({
      type: "open",
      generation: seen.current,
      started: Date.now(),
      repository: id,
      worktree,
      step: "checking",
    });
    try {
      const result = await request("open", { id, worktree, skipSeed, selection: ticket });
      seen.current = Math.max(seen.current, result.generation);
      setLibrary(result.library);
    } catch (cause) {
      if (ticket !== selecting.current) return;
      const problem = failure(cause);
      setOpen((current) => current && { ...current, step: "error", ...problem });
    }
  }

  async function selectRepository(current: Library, id: string, worktree?: string) {
    const ticket = ++selecting.current;
    const repository = current.repositories.find((r) => r.id === id);
    if (!repository) return;
    target.current = { repository: id, after: seen.current };
    const info = await discover(repository);
    if (ticket !== selecting.current) return;
    if (!info) return deselect(id, ticket);
    const known = (path?: string) => path && info.worktrees.some((w) => w.path === path);
    const choice = [worktree, remembered.current[id]].find(known) ?? info.primary;
    await openWorktree(id, choice, false, ticket);
  }

  /** Shows no worktree; the native pane stops displaying and keeping one alive. */
  function deselect(repository?: string, ticket = ++selecting.current) {
    target.current = { repository, after: seen.current };
    setSelection(repository ? { repository } : {});
    setOpen(null);
    void request("deselect", { selection: ticket }).catch(() => {});
  }

  /** Applies a library saved by any window, moving off a repository it no longer has. */
  function applyLibrary(saved: Library) {
    if (!setLibrary(saved)) return;
    const { repository, worktree } = target.current;
    if (!repository || saved.repositories.some((r) => r.id === repository)) return;
    const next =
      saved.repositories.find((r) => worktree && r.acknowledged.includes(worktree)) ??
      saved.repositories[0];
    if (next) void selectRepository(saved, next.id, worktree);
    else deselect();
  }

  async function load(session?: { repository?: string; worktree?: string }) {
    setLoadError(null);
    try {
      const saved = await request("list", {});
      setLibrary(saved);
      // A selection made meanwhile, such as one from the terminal, stands.
      if (selecting.current > 0) return;
      const recent = [...saved.repositories].sort(
        (a, b) => (b.lastOpened ?? 0) - (a.lastOpened ?? 0),
      )[0];
      const restore = saved.repositories.some((r) => r.id === session?.repository)
        ? session
        : { repository: recent?.id };
      if (restore?.repository) await selectRepository(saved, restore.repository, restore.worktree);
    } catch (cause) {
      setLoadError(failure(cause));
    }
  }

  useEffect(() => {
    void attach((message) => handler.current(message)).then((session) => {
      setCollapsed(session.sidebarCollapsed);
      setRestored(true);
      return load(session);
    });
    // The shell attaches once; later messages use the current handler.
  }, []);

  useEffect(() => {
    if (!restored) return;
    void request("session", {
      repository: selection.repository ?? null,
      worktree: selection.worktree ?? null,
      sidebarCollapsed: collapsed,
    }).catch(() => {});
  }, [restored, selection, collapsed]);

  const covered = settings || !selection.worktree;
  const report = useCallback(() => {
    const box = region.current?.getBoundingClientRect();
    if (!box) return;
    const layout = { x: box.left, y: box.top, width: box.width, height: box.height, covered };
    const key = JSON.stringify(layout);
    if (key === reported.current) return;
    reported.current = key;
    void request("layout", layout).catch(() => {
      reported.current = "";
    });
  }, [covered]);
  useLayoutEffect(report);
  useEffect(() => {
    const observer = new ResizeObserver(report);
    if (region.current) observer.observe(region.current);
    return () => observer.disconnect();
  }, [report]);

  const repositories = library?.repositories ?? [];
  const selected = repositories.find((r) => r.id === selection.repository);
  const selectedFound = selected ? found[selected.id] : undefined;
  const worktree = selectedFound?.info?.worktrees.find((w) => w.path === selection.worktree);

  function popup(items: Item[], at: HTMLElement | { x: number; y: number }) {
    menus.current = {};
    let next = 0;
    const convert = (list: Item[]): MenuEntry[] =>
      list.map(({ run, items: children, ...entry }) => {
        const id = `m${next++}`;
        if (run) menus.current[id] = run;
        return { ...entry, id, items: children && convert(children) };
      });
    const box = at instanceof HTMLElement ? at.getBoundingClientRect() : undefined;
    const point = box ? { x: box.left, y: box.bottom + 4 } : (at as { x: number; y: number });
    void request("menu", { ...point, items: convert(items) }).catch((cause) =>
      setAlert(failure(cause)),
    );
  }

  /** Moves a repository before or after another in the sidebar. */
  function moveRepository(id: string, target: string, after: boolean) {
    if (id === target) return;
    const ids = repositories.map((r) => r.id).filter((other) => other !== id);
    ids.splice(ids.indexOf(target) + (after ? 1 : 0), 0, id);
    void request("reorder", { ids })
      .then(setLibrary)
      .catch((cause) => setAlert(failure(cause)));
  }

  function addRepository() {
    void perform("Adding repository…", async () => {
      const path = await pickPath(true);
      if (!path) return;
      const result = await request("add", { path });
      setLibrary(result.library);
      await selectRepository(result.library, result.id);
    });
  }

  function chooseExecutable(repository: Repository, path: string) {
    void perform("Choosing executable…", async () => {
      const executable = await pickPath(false);
      if (!executable) return;
      setLibrary(
        await request("set-executable", { id: repository.id, worktree: path, executable }),
      );
      if (target.current.worktree === path) await openWorktree(repository.id, path);
    });
  }

  function openSettings() {
    setSettings(true);
    void perform("Checking Rhizome…", async () => setGlobal(await request("global-status", {})));
  }

  function worktreeMenu(anchor: HTMLElement) {
    if (!selected || !selectedFound?.info) return;
    const info = selectedFound.info;
    const fresh = newWorktrees(selected, info).map((w) => w.path);
    popup(
      info.worktrees.map((w) => ({
        label: `${fresh.includes(w.path) ? "● " : ""}${worktreeName(w)} — ${basename(w.path)}${w.path === info.primary ? " (primary)" : ""}${w.error ? " · Unavailable" : runtimeLabel[runtimes[w.path]?.state ?? "stopped"]}`,
        checked: w.path === selection.worktree,
        run: () => void openWorktree(selected.id, w.path),
      })),
      anchor,
    );
  }

  function repositoryMenu(repository: Repository, at: HTMLElement | { x: number; y: number }) {
    const info = found[repository.id]?.info;
    const path =
      (repository.id === selection.repository && selection.worktree) || info?.primary || undefined;
    const subject = info?.worktrees.find((w) => w.path === path);
    const setPrimary = (worktree: string | null) =>
      void perform("Saving primary worktree…", async () => {
        const saved = await request("set-primary", { id: repository.id, worktree });
        setLibrary(saved);
        await discover(repository);
      });
    const automatic = info?.worktrees.find((w) => w.path === info.defaultPrimary);
    const state = path ? runtimes[path]?.state : undefined;
    const name = subject ? ` for ${worktreeName(subject)}` : "";
    popup(
      [
        {
          label: "Open in Browser",
          disabled: state !== "running",
          run: () =>
            path &&
            void request("open-in-browser", { worktree: path }).catch((cause) =>
              setAlert(failure(cause)),
            ),
        },
        {
          label: "Reveal in Finder",
          disabled: !path,
          run: () =>
            path &&
            void request("reveal", { worktree: path }).catch((cause) => setAlert(failure(cause))),
        },
        { separator: true },
        {
          label: `Stop Rhizome${name}`,
          disabled: !path || !state || state === "stopped",
          run: () =>
            path &&
            void perform("Stopping Rhizome…", async () => {
              await request("stop", { worktree: path });
            }),
        },
        {
          label: `Restart Rhizome${name}`,
          disabled: state !== "running",
          run: () =>
            path &&
            void perform("Restarting Rhizome…", async () => {
              await request("restart", { id: repository.id, worktree: path });
            }),
        },
        { separator: true },
        {
          label: "Primary worktree",
          disabled: !info,
          items: [
            {
              label: `Automatic${automatic ? ` (${worktreeName(automatic)})` : ""}`,
              checked: !repository.primary,
              run: () => setPrimary(null),
            },
            { separator: true },
            ...(info?.worktrees ?? []).map((w) => ({
              label: `${worktreeName(w)} — ${basename(w.path)}`,
              checked: repository.primary === w.path,
              run: () => setPrimary(w.path),
            })),
          ],
        },
        {
          label: `Rhizome executable for ${subject ? worktreeName(subject) : "this worktree"}`,
          disabled: !path,
          items: [
            { label: "Choose executable…", run: () => path && chooseExecutable(repository, path) },
            {
              label: "Use configured selection",
              disabled: !path || !repository.executables?.[path],
              run: () =>
                path &&
                void perform("Saving executable…", async () => {
                  setLibrary(
                    await request("set-executable", {
                      id: repository.id,
                      worktree: path,
                      executable: null,
                    }),
                  );
                }),
            },
          ],
        },
        { separator: true },
        {
          label: "Remove from Library",
          run: () =>
            void perform("Removing repository…", async () => {
              applyLibrary(await request("remove", { id: repository.id }));
            }),
        },
      ],
      at,
    );
  }

  useEffect(() => {
    handler.current = (message) => {
      if (message.type === "open") {
        const expected = target.current;
        seen.current = Math.max(seen.current, message.generation);
        if (
          message.generation > expected.after &&
          message.repository === expected.repository &&
          message.worktree === expected.worktree
        )
          setOpen(message);
      } else if (message.type === "library") {
        applyLibrary(message.library);
      } else if (message.type === "select") {
        setLibrary(message.library);
        void selectRepository(message.library, message.repository, message.worktree);
      } else if (message.type === "alert") {
        setAlert({ code: message.code, message: message.message });
      } else if (message.type === "presence") {
        setFound((current) => ({ ...current, ...message.repositories }));
        setRuntimes(message.runtimes);
      } else if (message.type === "menu") {
        const run = menus.current[message.id];
        menus.current = {};
        run?.();
      } else if (message.type === "command") {
        if (message.command === "add-repository") addRepository();
        if (message.command === "toggle-sidebar") setCollapsed((value) => !value);
        if (message.command === "settings") openSettings();
      }
    };
  });

  function content() {
    if (settings) {
      return (
        <Settings
          info={global}
          selected={library?.globalExecutable}
          busy={!!busy}
          developmentBuilds={repositories.flatMap((repository) =>
            (found[repository.id]?.info?.worktrees ?? []).flatMap((w) =>
              w.executable
                ? [
                    {
                      repository: repository.name,
                      worktree: worktreeName(w),
                      executable: w.executable,
                    },
                  ]
                : [],
            ),
          )}
          onClose={() => setSettings(false)}
          onRefresh={() =>
            void perform("Checking Rhizome…", async () =>
              setGlobal(await request("global-status", {})),
            )
          }
          onInstall={() =>
            void perform("Installing Rhizome…", async () => {
              setGlobal(await request("global-install", {}));
              setLibrary(await request("list", {}));
            })
          }
          onUpdate={() =>
            void perform("Updating Rhizome…", async () => {
              setGlobal(await request("global-update", {}));
              setLibrary(await request("list", {}));
            })
          }
          onSelect={(executable) =>
            void perform("Saving executable…", async () => {
              setLibrary(await request("set-global-executable", { executable }));
              setGlobal(await request("global-status", {}));
            })
          }
          onPick={() =>
            void perform("Choosing executable…", async () => {
              const executable = await pickPath(false);
              if (!executable) return;
              setLibrary(await request("set-global-executable", { executable }));
              setGlobal(await request("global-status", {}));
            })
          }
        />
      );
    }
    if (loadError) {
      return (
        <Notice title="Could not load your repositories" failure={loadError}>
          <button className="primary" onClick={() => void load()}>
            Try again
          </button>
        </Notice>
      );
    }
    if (!library) return <Notice title="Loading your repositories…" />;
    if (repositories.length === 0) {
      return (
        <Notice title="Add a repository to begin">
          <p>
            Rhizome opens each repository with the version it configures. Its Git worktrees appear
            together, and you can switch between them from the toolbar.
          </p>
          <button className="primary" disabled={!!busy} onClick={addRepository}>
            Add Repository…
          </button>
        </Notice>
      );
    }
    if (!selected) return <Notice title="Choose a repository" />;
    if (selectedFound?.error && !selection.worktree) {
      return (
        <Notice title={`${selected.name} is unavailable`} failure={selectedFound.error}>
          <p className="path">{selected.root}</p>
          <div className="actions">
            <button
              className="primary"
              disabled={!!busy}
              onClick={() => void selectRepository(library, selected.id)}
            >
              Try again
            </button>
          </div>
        </Notice>
      );
    }
    if (!open) return null;
    return (
      <OpenStatus
        state={open}
        busy={!!busy}
        onRetry={() => void openWorktree(open.repository, open.worktree)}
        onWake={() => void openWorktree(open.repository, open.worktree)}
        onSkipSeed={() => void openWorktree(open.repository, open.worktree, true)}
        onTrust={() =>
          void perform("Trusting worktree…", async () => {
            const ticket = ++selecting.current;
            await request("trust", { id: open.repository, worktree: open.worktree });
            await openWorktree(open.repository, open.worktree, false, ticket);
          })
        }
        onSetup={() =>
          void perform("Setting up Rhizome…", async () => {
            const ticket = ++selecting.current;
            await request("initialize", { id: open.repository, worktree: open.worktree });
            await openWorktree(open.repository, open.worktree, false, ticket);
          })
        }
        onChooseExecutable={() => selected && chooseExecutable(selected, open.worktree)}
        onManageInstallation={openSettings}
      />
    );
  }

  return (
    <div className={collapsed ? "shell collapsed" : "shell"}>
      <header className="toolbar" data-tauri-drag-region>
        <button
          className="icon-button"
          aria-label={collapsed ? "Show sidebar" : "Hide sidebar"}
          aria-pressed={!collapsed}
          onClick={() => setCollapsed(!collapsed)}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <rect x="1.5" y="2.5" width="13" height="11" rx="1.5" />
            <path d="M6 2.5v11" />
          </svg>
        </button>
        {(
          [
            ["back", "Back", "M10 3.5 5.5 8l4.5 4.5"],
            ["forward", "Forward", "M6 3.5 10.5 8 6 12.5"],
            ["reload", "Reload", "M12.5 8a4.5 4.5 0 1 1-1.3-3.2M12.5 2.5v2.8H9.7"],
          ] as const
        ).map(([to, label, path]) => (
          <button
            key={to}
            className="icon-button"
            aria-label={label}
            title={to === "reload" ? "Reload (⌘R)" : label}
            disabled={covered || open?.step !== "ready"}
            onClick={() => void request("browse", { to }).catch(() => {})}
          >
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d={path} />
            </svg>
          </button>
        ))}
        {selected && (
          <button
            className="worktree-selector"
            aria-haspopup="menu"
            aria-label="Worktree"
            disabled={!selectedFound?.info}
            onClick={(e) => worktreeMenu(e.currentTarget)}
          >
            <strong>{selected.name}</strong>
            <span className="branch">{worktree ? worktreeName(worktree) : "…"}</span>
            {worktree && <span className="path-hint">{basename(worktree.path)}</span>}
            {newWorktrees(selected, selectedFound?.info).length > 0 && (
              <span className="new-dot" title="New worktrees" />
            )}
            <span aria-hidden="true">▾</span>
          </button>
        )}
        <span className="toolbar-space" data-tauri-drag-region />
        {busy && <span className="busy">{busy}</span>}
        <button
          className="icon-button"
          aria-label="Settings"
          aria-pressed={settings}
          onClick={() => (settings ? setSettings(false) : openSettings())}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <circle cx="8" cy="8" r="2.2" />
            <path d="M8 1.5v2M8 12.5v2M1.5 8h2M12.5 8h2M3.4 3.4l1.4 1.4M11.2 11.2l1.4 1.4M3.4 12.6l1.4-1.4M11.2 4.8l1.4-1.4" />
          </svg>
        </button>
      </header>
      {!collapsed && (
        <nav className="sidebar" aria-label="Repositories">
          <div className="sidebar-heading">
            <span>Repositories</span>
            <button
              className="icon-button"
              aria-label="Add repository"
              disabled={!!busy || !library}
              onClick={addRepository}
            >
              +
            </button>
          </div>
          <div role="tablist" aria-orientation="vertical">
            {repositories.map((repository) => {
              const state = found[repository.id];
              const fresh = newWorktrees(repository, state?.info).length;
              const runtime = repositoryState(runtimes, state?.info);
              const drop =
                dropAt?.id === repository.id ? (dropAt.after ? " drop-after" : " drop-before") : "";
              return (
                <div
                  className={`repository-tab${dragging === repository.id ? " dragging" : ""}${drop}`}
                  key={repository.id}
                  draggable
                  onContextMenu={(e) => {
                    e.preventDefault();
                    repositoryMenu(repository, { x: e.clientX, y: e.clientY });
                  }}
                  onDragStart={(e) => {
                    e.dataTransfer.effectAllowed = "move";
                    e.dataTransfer.setData("text/plain", repository.name);
                    setDragging(repository.id);
                  }}
                  onDragEnd={() => {
                    setDragging(null);
                    setDropAt(null);
                  }}
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
                    if (dragging && dropAt) moveRepository(dragging, dropAt.id, dropAt.after);
                    setDragging(null);
                    setDropAt(null);
                  }}
                >
                  <button
                    role="tab"
                    aria-selected={repository.id === selection.repository}
                    aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
                    title={repository.root}
                    onClick={() => library && void selectRepository(library, repository.id)}
                    onKeyDown={(e) => {
                      if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
                      e.preventDefault();
                      const index = repositories.indexOf(repository);
                      const neighbor = repositories[index + (e.key === "ArrowUp" ? -1 : 1)];
                      if (neighbor)
                        moveRepository(repository.id, neighbor.id, e.key === "ArrowDown");
                    }}
                  >
                    <span className="name">{repository.name}</span>
                    {state?.error && (
                      <span className="tab-status warning">
                        {state.error.code === "folder_missing" ? "Missing" : "Error"}
                      </span>
                    )}
                    {state?.info && (
                      <span
                        className={`runtime-dot ${runtime ?? "stopped"}`}
                        role="img"
                        aria-label={runtimeTitle[runtime ?? "stopped"]}
                        title={runtimeTitle[runtime ?? "stopped"]}
                      />
                    )}
                    {fresh > 0 && (
                      <span
                        className="new-dot"
                        title={`${fresh} new ${fresh === 1 ? "worktree" : "worktrees"}`}
                      />
                    )}
                  </button>
                  <button
                    className="icon-button options"
                    aria-label={`Options for ${repository.name}`}
                    onClick={(e) => repositoryMenu(repository, e.currentTarget)}
                  >
                    •••
                  </button>
                </div>
              );
            })}
          </div>
        </nav>
      )}
      <main className="main">
        {alert && (
          <div className="alert" role="alert">
            <span>{alert.message}</span>
            <button className="icon-button" aria-label="Dismiss" onClick={() => setAlert(null)}>
              ×
            </button>
          </div>
        )}
        <div className="content" ref={region}>
          {content()}
        </div>
      </main>
    </div>
  );
}
