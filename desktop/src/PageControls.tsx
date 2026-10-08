import { useEffect, useState, type Ref } from "react";
import type { PageCommand, PageState } from "./api";

const SECTIONS = [
  ["notes", "Notes"],
  ["ontology", "Ontology"],
  ["explorer", "Explorer"],
  ["agent", "Agent"],
  ["graphql", "GraphQL"],
] as const;

type Props = { state: PageState; onCommand: (command: PageCommand) => void };

// The workspace page's header controls, drawn in the toolbar (SPEC-0119). They
// show the state the page last reported and send it commands.

export function PageSections({ state, onCommand }: Props) {
  return (
    <nav className="page-sections" aria-label="Sections">
      {SECTIONS.map(([section, label]) => (
        <button
          key={section}
          aria-current={state.section === section ? "page" : undefined}
          onClick={() => onCommand({ command: "section", section })}
        >
          {label}
        </button>
      ))}
    </nav>
  );
}

export function PageTools({
  state,
  searchRef,
  onCommand,
}: Props & { searchRef: Ref<HTMLInputElement> }) {
  const [query, setQuery] = useState(state.search);
  useEffect(() => setQuery(state.search), [state.search]);

  return (
    <>
      <form
        className="page-search"
        role="search"
        aria-label="Project search"
        onSubmit={(event) => {
          event.preventDefault();
          if (query.trim()) onCommand({ command: "search", query });
        }}
      >
        <input
          ref={searchRef}
          type="search"
          aria-label="Search this project"
          placeholder="Search this project…"
          maxLength={500}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <kbd>⌘K</kbd>
      </form>
      <IssueBadge state={state} onOpen={() => onCommand({ command: "issues" })} />
      <button
        className="icon-button"
        aria-label="Keyboard shortcuts"
        title="Keyboard shortcuts (?)"
        onClick={() => onCommand({ command: "shortcuts" })}
      >
        ?
      </button>
    </>
  );
}

/** Mirrors the web header's validation badge: a count, or a mark while the
 * count is unknown because validation is running, stale, or failed. */
function IssueBadge({ state, onOpen }: { state: PageState; onOpen: () => void }) {
  const { issues, health } = state;
  const unknown = health === "running" || health === "stale" || health === "failed";
  if (state.section !== "notes" || issues === 0 || (issues == null && !unknown)) return null;
  const mark = health === "running" ? "…" : health === "stale" ? "old" : "?";
  return (
    <button
      className={`issue-badge ${health}`}
      aria-label={issues ? `Open ${issues} validation issues` : "Open validation status"}
      onClick={onOpen}
    >
      {issues ?? mark}
    </button>
  );
}
