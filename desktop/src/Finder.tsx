import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { attachFinder, request, type FinderChoice, type FinderModel, type FinderRow } from "./api";

// The repository (⌘O) and worktree (⇧⌘O) finders (SPEC-0126 US9, US10). The
// shell builds the rows (Switcher.tsx); this overlay webview filters them and
// sends back the user's choice.

/** What a finder row says about its runtime. A sleeping runtime is one the
 * user stopped while this window shows it. */
const runtimeTitle = {
  running: "Rhizome running",
  starting: "Rhizome starting",
  stopped: "Rhizome not running",
  sleeping: "Rhizome sleeping",
};

type Match = { score: number; at: number[] };

/** Scores `text` as containing `query` in order, smart-case: an all-lowercase
 * query ignores case. Consecutive and word-start characters score higher.
 * Returns null when `text` does not contain the query's characters in order. */
export function fuzzy(query: string, text: string): Match | null {
  const fold = query === query.toLowerCase();
  const at: number[] = [];
  let score = 0;
  // Positions are in `text`'s own UTF-16 units: matching compares its whole
  // characters, so an emoji (two units) or a character that lowercases longer
  // (İ to i̇) never shifts them.
  let index = 0;
  // Where the last matched character ended, for the consecutive bonus.
  let previous = -1;
  // ponytail: greedy leftmost match, not the best alignment; fine for short names.
  for (const char of query) {
    let here = "";
    for (; index < text.length; index += here.length) {
      here = String.fromCodePoint(text.codePointAt(index)!);
      if ((fold ? here.toLowerCase() : here).startsWith(char)) break;
    }
    if (index >= text.length) return null;
    if (index === previous) score += 3;
    if (index === 0 || /[\s/_.-]/.test(text[index - 1])) score += 2;
    at.push(index);
    score += 1;
    index += here.length;
    previous = index;
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
    // The whole character, so an emoji is never split.
    const point = text.codePointAt(index);
    if (point === undefined) continue;
    const char = String.fromCodePoint(point);
    if (index > from) parts.push(text.slice(from, index));
    parts.push(<mark key={index}>{char}</mark>);
    from = index + char.length;
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
      // Naming the opening lets the app drop a choice from one it replaced.
      onChoose={(choice) =>
        void request("finder-choice", { opened: model.opened, choice }).catch(() => {})
      }
    />
  );
}

const HOME = /^\/Users\/[^/]+/;

const rowKey = (row: FinderRow) => row.worktree ?? row.repository;

export function Finder({
  model,
  onChoose,
}: {
  model: FinderModel;
  onChoose: (choice: FinderChoice) => void;
}) {
  const [query, setQuery] = useState("");
  // The highlighted row, by identity, so live updates that reorder the rows
  // keep it; none means the top row. Without a query, start on the first row
  // this window does not show: one Enter switches away, as ⌘Tab does.
  const [active, setActive] = useState(() => {
    const away = model.rows.find((r) => !r.current);
    return away && rowKey(away);
  });
  const input = useRef<HTMLInputElement>(null);
  const list = useId();
  const repositories = model.mode === "repositories";
  const shown = useMemo(() => rank(model.rows, query), [model.rows, query]);
  const index = Math.max(
    0,
    shown.findIndex(({ row }) => rowKey(row) === active),
  );
  const highlight = (at: number) => {
    const row = shown[Math.max(0, Math.min(at, shown.length - 1))]?.row;
    if (row) setActive(rowKey(row));
  };

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

  const actions = (row: FinderRow | undefined, button: Element | null | undefined) => {
    if (!row || !button) return;
    const box = button.getBoundingClientRect();
    onChoose({ choice: "menu", repository: row.repository, x: box.left, y: box.bottom + 4 });
  };

  function onKeyDown(event: React.KeyboardEvent) {
    // Keys that compose or confirm IME text are the input's own.
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    // Enter on a button, such as Add repository…, is the button's own.
    if (event.key === "Enter" && event.target instanceof HTMLButtonElement) return;
    const digit = Number(event.key);
    const caret = input.current;
    if (event.key === "Escape") onChoose({ choice: "close" });
    else if (event.key === "ArrowDown") highlight(index + 1);
    else if (event.key === "ArrowUp") highlight(index - 1);
    else if (event.key === "Enter") open(shown[index]?.row, event.metaKey);
    else if (repositories && event.metaKey && digit >= 1 && digit <= 9)
      open(shown[digit - 1]?.row, false);
    // → at the end of the query opens the highlighted row's actions, as in Alfred.
    else if (
      repositories &&
      event.key === "ArrowRight" &&
      caret?.selectionStart === query.length &&
      caret.selectionEnd === query.length
    )
      actions(
        shown[index]?.row,
        document.getElementById(`${list}-${index}`)?.querySelector(".finder-actions"),
      );
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
              setActive(undefined);
            }}
          />
          <kbd>esc</kbd>
        </div>
        <ul className="finder-list" id={list} role="listbox" aria-label={label}>
          {shown.map(({ row, title, detail }, at) => (
            <li
              key={rowKey(row)}
              id={`${list}-${at}`}
              role="option"
              aria-selected={at === index}
              className="finder-row"
              onMouseMove={() => at !== index && setActive(rowKey(row))}
              onClick={(event) => open(row, event.metaKey)}
            >
              <span
                className={`runtime-dot ${row.runtime}`}
                role="img"
                aria-label={runtimeTitle[row.runtime]}
                title={runtimeTitle[row.runtime]}
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
                      actions(row, event.currentTarget);
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
                : (model.error ??
                  (repositories ? "No repositories yet." : "Finding this repository’s worktrees…"))}
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
          {repositories && (
            <span>
              <kbd>→</kbd>
              actions
            </span>
          )}
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
