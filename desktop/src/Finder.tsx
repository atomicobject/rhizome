import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { attachFinder, request, type FinderChoice, type FinderModel, type FinderRow } from "./api";

// The repository (⌘O) and worktree (⇧⌘O) finders (SPEC-0126 US9, US10). The
// shell builds the rows (Switcher.tsx); this overlay webview filters them and
// sends back the user's choice.

/** What the toolbar and finder say about a runtime. A sleeping runtime is one
 * the user stopped while this window shows it. */
export const runtimeText = {
  running: { menu: " · Running", title: "Rhizome running" },
  starting: { menu: " · Starting…", title: "Rhizome starting" },
  stopped: { menu: "", title: "Rhizome not running" },
  sleeping: { menu: "", title: "Rhizome sleeping" },
};

type Match = { score: number; at: number[] };

/** Scores `text` as containing `query` in order, smart-case: an all-lowercase
 * query ignores case. Consecutive and word-start characters score higher.
 * Returns null when `text` does not contain the query's characters in order. */
export function fuzzy(query: string, text: string): Match | null {
  const hay = query === query.toLowerCase() ? text.toLowerCase() : text;
  const at: number[] = [];
  let score = 0;
  // ponytail: greedy leftmost match, not the best alignment; fine for short names.
  for (const char of query) {
    const index = hay.indexOf(char, (at.at(-1) ?? -1) + 1);
    if (index < 0) return null;
    if (at.length && index === at.at(-1)! + 1) score += 3;
    if (index === 0 || /[\s/_.-]/.test(text[index - 1])) score += 2;
    at.push(index);
    score += 1;
  }
  return { score, at };
}

type Ranked = { row: FinderRow; title?: number[]; detail?: number[] };

/** Rows that match `query` by title or path, best first. A title match
 * outranks a path match; ties keep the shell's order. */
export function rank(rows: FinderRow[], query: string): Ranked[] {
  const trimmed = query.trim();
  if (!trimmed) return rows.map((row) => ({ row }));
  return rows
    .flatMap((row, order) => {
      const title = fuzzy(trimmed, row.title);
      const detail = title ? null : fuzzy(trimmed, row.detail);
      const match = title ?? detail;
      if (!match) return [];
      const score = match.score + (title ? 100 : 0);
      return [{ row, title: title?.at, detail: detail?.at, score, order }];
    })
    .sort((a, b) => b.score - a.score || a.order - b.order);
}

function Marked({ text, at }: { text: string; at?: number[] }) {
  if (!at?.length) return <>{text}</>;
  const parts: ReactNode[] = [];
  let from = 0;
  for (const index of at) {
    if (index > from) parts.push(text.slice(from, index));
    parts.push(<mark key={index}>{text[index]}</mark>);
    from = index + 1;
  }
  parts.push(text.slice(from));
  return <>{parts}</>;
}

/** The finder overlay's root: it shows whatever model the shell last sent. */
export function FinderApp() {
  const [model, setModel] = useState<FinderModel | null>(null);
  useEffect(() => {
    void attachFinder(setModel);
  }, []);
  useEffect(() => {
    if (model) document.documentElement.dataset.theme = model.theme;
  }, [model]);
  if (!model) return null;
  return (
    <Finder
      key={model.opened}
      model={model}
      onChoose={(choice) => void request("finder-choice", choice).catch(() => {})}
    />
  );
}

const HOME = /^\/Users\/[^/]+/;

export function Finder({
  model,
  onChoose,
}: {
  model: FinderModel;
  onChoose: (choice: FinderChoice) => void;
}) {
  const [query, setQuery] = useState("");
  // Without a query, start on the first row this window does not show: one
  // Enter switches away, as ⌘Tab does.
  const [active, setActive] = useState(() =>
    Math.max(
      0,
      model.rows.findIndex((r) => !r.current),
    ),
  );
  const input = useRef<HTMLInputElement>(null);
  const list = useId();
  const repositories = model.mode === "repositories";
  const shown = useMemo(() => rank(model.rows, query), [model.rows, query]);
  const index = Math.min(active, shown.length - 1);

  // The overlay keeps its document between openings; take focus each time.
  useEffect(() => {
    input.current?.focus();
  }, []);
  useEffect(() => {
    document.getElementById(`${list}-${index}`)?.scrollIntoView?.({ block: "nearest" });
  }, [index, list]);

  const open = (row: FinderRow | undefined, newWindow: boolean) =>
    row &&
    onChoose({
      choice: "open",
      repository: row.repository,
      worktree: row.worktree ?? null,
      newWindow,
    });

  function onKeyDown(event: React.KeyboardEvent) {
    const digit = Number(event.key);
    if (event.key === "Escape") onChoose({ choice: "close" });
    else if (event.key === "ArrowDown") setActive(Math.min(index + 1, shown.length - 1));
    else if (event.key === "ArrowUp") setActive(Math.max(index - 1, 0));
    else if (event.key === "Enter") open(shown[index]?.row, event.metaKey);
    else if (repositories && event.metaKey && digit >= 1 && digit <= 9)
      open(shown[digit - 1]?.row, false);
    else return;
    event.preventDefault();
  }

  const label = repositories ? "Switch repository" : "Switch worktree";
  return (
    <div className="finder-scrim" onMouseDown={() => onChoose({ choice: "close" })}>
      <div
        className="finder"
        role="dialog"
        aria-label={label}
        onMouseDown={(event) => event.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        <div className="finder-input">
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <circle cx="7" cy="7" r="4.5" />
            <path d="m10.5 10.5 3.5 3.5" />
          </svg>
          <input
            ref={input}
            role="combobox"
            aria-label={label}
            aria-expanded="true"
            aria-controls={list}
            aria-activedescendant={shown.length ? `${list}-${index}` : undefined}
            aria-autocomplete="list"
            placeholder={repositories ? "Find a repository…" : "Find a worktree…"}
            spellCheck={false}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(0);
            }}
          />
          <kbd>esc</kbd>
        </div>
        <ul className="finder-list" id={list} role="listbox" aria-label={label}>
          {shown.map(({ row, title, detail }, at) => (
            <li
              key={row.worktree ?? row.repository}
              id={`${list}-${at}`}
              role="option"
              aria-selected={at === index}
              className="finder-row"
              onMouseMove={() => at !== index && setActive(at)}
              onClick={(event) => open(row, event.metaKey)}
            >
              <span
                className={`runtime-dot ${row.runtime}`}
                role="img"
                aria-label={runtimeText[row.runtime].title}
                title={runtimeText[row.runtime].title}
              />
              <span className="finder-main">
                <span className={repositories ? "finder-title" : "finder-title branch"}>
                  <Marked text={row.title} at={title} />
                </span>
                {row.primary && <span className="finder-tag">primary</span>}
                {row.fresh && (
                  <span className="new-dot" title={repositories ? "New worktrees" : "New"} />
                )}
              </span>
              <span className="finder-side">
                {row.current ? (
                  <span className="finder-current">This window</span>
                ) : (
                  <span className="finder-path" title={row.detail}>
                    <Marked text={row.detail.replace(HOME, "~")} at={tilde(row.detail, detail)} />
                  </span>
                )}
                {repositories && at < 9 && <kbd>⌘{at + 1}</kbd>}
                {repositories && (
                  <button
                    className="icon-button finder-actions"
                    aria-label={`Actions for ${row.title}`}
                    tabIndex={-1}
                    onClick={(event) => {
                      event.stopPropagation();
                      const box = event.currentTarget.getBoundingClientRect();
                      onChoose({
                        choice: "menu",
                        repository: row.repository,
                        x: box.left,
                        y: box.bottom + 4,
                      });
                    }}
                  >
                    •••
                  </button>
                )}
              </span>
            </li>
          ))}
          {shown.length === 0 && (
            <li className="finder-empty" role="presentation">
              {model.rows.length > 0
                ? `No ${repositories ? "repository" : "worktree"} matches “${query.trim()}”.`
                : repositories
                  ? "No repositories yet."
                  : "Finding this repository’s worktrees…"}
            </li>
          )}
        </ul>
        <div className="finder-foot">
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd>
            navigate
          </span>
          <span>
            <kbd>↵</kbd>
            open
          </span>
          <span>
            <kbd>⌘↵</kbd>
            new window
          </span>
          <span className="grow" />
          {repositories && (
            <button className="text-button" onClick={() => onChoose({ choice: "add" })}>
              Add repository…
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

/** Shifts match positions in a path to its `~`-abbreviated form. */
function tilde(path: string, at?: number[]) {
  const home = path.match(HOME)?.[0].length ?? 0;
  if (!home || !at) return at;
  return at.filter((i) => i >= home).map((i) => i - home + 1);
}
