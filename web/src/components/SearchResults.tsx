import { useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { DisclosureChevron } from "./DisclosureChevron";
import { MiddleTruncate } from "./MiddleTruncate";
import { noteExcerptParts } from "./searchExcerpt";
import { publicTypeName } from "../lib/typeNames";
import { humanizeName } from "../lib/labels";
import { publicOntologyListItemRef, publicWorkspaceRef } from "../api/client";
import type { WorkspaceSearchMatch } from "../api/types";
import { PSEUDO_TYPE_ALL } from "./notesRoute";
import { useOntologyTypeQuery } from "./useNotesQueries";
import { notifyLocationChange } from "./locationStore";
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import {
  ROOT_FOLDER,
  folderLabel,
  inFolder,
  normalizeSearchFilters,
  normalizeSearchQuery,
  useSearchScrollRestoration,
} from "./searchState";
import type { SearchWorkspaceProps } from "./SearchWorkspace";

function highlightLiteral(text: string, query: string) {
  const terms = normalizeSearchQuery(query)
    .split(" ")
    .filter(Boolean)
    .map((term) => term.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));

  if (terms.length === 0) return text;
  const matcher = new RegExp(`(${terms.join("|")})`, "gi");

  return text
    .split(matcher)
    .map((part, index) => (index % 2 === 1 ? <mark key={`${part}-${index}`}>{part}</mark> : part));
}

function excerptFor(match: WorkspaceSearchMatch): string | null {
  if (match.snippetStatus !== "available") return null;
  const value = match.snippet || "";

  return value.trim() ? value.trim() : null;
}

// A code preview joins a symbol's signature and its doc comment with " — ".
// The signature keeps its source lines; the comment reads as one paragraph.
function codeExcerptParts(excerpt: string) {
  const at = excerpt.indexOf(" — ");

  if (at < 0) return { signature: excerpt, doc: "" };

  return { signature: excerpt.slice(0, at), doc: excerpt.slice(at + 3).replace(/\s+/g, " ") };
}

// What the match is, from the result itself: a code symbol's kind or a note
// section. The payload carries no per-match retrieval evidence.
function matchedAs(match: WorkspaceSearchMatch): string {
  if (match.type === "code") return match.kind ? `${humanizeName(match.kind)} symbol` : "";

  return match.nodeKind?.toUpperCase() === "SECTION" ? "Section" : "";
}

export function SearchBanner({
  tone = "status",
  details = [],
  action,
  children,
}: {
  tone?: "status" | "alert";
  details?: string[];
  action?: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const detailsId = useId();

  return (
    <div className={`search-workspace__banner is-${tone}`}>
      <div className="search-workspace__banner-row">
        <svg
          className="search-workspace__banner-mark"
          viewBox="0 0 16 16"
          aria-hidden="true"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinejoin="round"
        >
          <path d="M8 2.5 14 13H2L8 2.5Z" />
          <path d="M8 6.5v3" strokeLinecap="round" />
          <circle cx="8" cy="11.25" r="0.5" fill="currentColor" stroke="none" />
        </svg>
        <span className="search-workspace__banner-text" role={tone}>
          {children}
        </span>
        {action}
        {details.length > 0 ? (
          <button
            type="button"
            aria-expanded={open}
            aria-controls={detailsId}
            onClick={() => setOpen((value) => !value)}
          >
            {open ? "Hide details" : "Details"}
          </button>
        ) : null}
      </div>
      {details.length > 0 ? (
        <ul id={detailsId} className="search-workspace__banner-details" hidden={!open}>
          {details.map((detail) => (
            <li key={detail}>{detail}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function resultLabel(match: WorkspaceSearchMatch): string {
  return match.title || match.symbol || match.fqn || match.path || "Untitled result";
}

/** A provenance path that gives way in the middle, keeping its file name and line. */
function SourcePath({ path }: { path: string }) {
  return <MiddleTruncate text={path} tail={path.length - path.lastIndexOf("/") - 1} />;
}

function sourceLabel(match: WorkspaceSearchMatch): string {
  if (match.type === "code") return match.path || "Code source";

  return match.path || "Note source";
}

function isNoteMatch(match: WorkspaceSearchMatch): boolean {
  return match.type === "note" || match.path?.toLowerCase().endsWith(".md") === true;
}

function noteTargetForMatch(match: WorkspaceSearchMatch): string {
  const ref = match.linkTarget?.ref || match.nodeRef;

  return ref ? publicWorkspaceRef(ref) : match.path || "";
}

function openCodeSource(path: string, line?: number) {
  if (!path) return;
  const params = new URLSearchParams({ file: path });

  if (line && line > 0) params.set("line", String(line));
  window.history.pushState({}, "", `/explorer?${params.toString()}`);
  notifyLocationChange();
}

function NoteResultLink({
  target,
  children,
  onOpenNote,
}: {
  target: string;
  children: ReactNode;
  onOpenNote: SearchWorkspaceProps["onOpenNote"];
}) {
  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    open: (path, mode) => onOpenNote(path, mode === "beside" ? "beside" : "activate"),
  });

  return (
    <>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="search-workspace__result-link link-quiet link-title"
        onClick={(event) => {
          previewTrigger.close();
          onOpenNote(target, event.metaKey || event.ctrlKey ? "beside" : "activate");
        }}
      >
        {children}
      </button>
      {previewTrigger.preview}
    </>
  );
}

export function FolderNotesWorkspace({
  tab,
  active,
  restoreReady = true,
  editSession = null,
  onRefineSearch,
  onOpenNote,
  onScrollPosition,
}: SearchWorkspaceProps) {
  const folder = tab.filters.folder || "";
  const [folderDraft, setFolderDraft] = useState(folder);
  const [queryDraft, setQueryDraft] = useState("");
  const notesQuery = useOntologyTypeQuery(PSEUDO_TYPE_ALL, editSession, active);

  const scrollRef = useSearchScrollRestoration(
    tab,
    active,
    restoreReady && Boolean(notesQuery.data),
  );

  useEffect(() => {
    setFolderDraft(folder);
  }, [tab.id, folder]);

  const notes = useMemo(
    () =>
      (notesQuery.data?.notes ?? [])
        .filter((note) => inFolder(note.path, folder))
        .sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0)),
    [folder, notesQuery.data?.notes],
  );

  const applyFolder = () => {
    // Clearing the folder would leave nothing to list, so the field reverts instead.
    if (!normalizeSearchFilters({ folder: folderDraft }).folder) setFolderDraft(folder);
    else onRefineSearch(tab.id, { ...tab.filters, folder: folderDraft });
  };

  const submitQuery = () => {
    if (normalizeSearchQuery(queryDraft)) onRefineSearch(tab.id, tab.filters, queryDraft);
  };

  return (
    <main
      ref={scrollRef}
      className="search-workspace"
      aria-label={`Notes in ${folderLabel(folder)}`}
      onScroll={(event) => {
        if (active) onScrollPosition(tab.id, event.currentTarget.scrollTop);
      }}
    >
      <header className="search-workspace__header">
        <div className="search-workspace__title-row">
          <h1>Notes in {folderLabel(folder)}</h1>
          {notesQuery.data ? (
            <span className="search-workspace__count">
              {notes.length} {notes.length === 1 ? "note" : "notes"}
            </span>
          ) : null}
        </div>
        <div className="search-workspace__filters" aria-label="Search filters">
          {/* Ranked search filters by path prefix, which cannot hold to the root's own notes. */}
          {folder !== ROOT_FOLDER && (
            <label>
              <span>Search</span>
              <input
                aria-label="Search this folder"
                placeholder="Search this folder"
                value={queryDraft}
                onChange={(event) => setQueryDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") submitQuery();
                }}
              />
            </label>
          )}
          <label>
            <span>Folder</span>
            <input
              aria-label="Search folder"
              value={folderDraft}
              onChange={(event) => setFolderDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") applyFolder();
              }}
              onBlur={applyFolder}
            />
          </label>
        </div>
      </header>
      {notesQuery.isError && !notesQuery.data ? (
        <div className="search-workspace__state" role="alert">
          <strong>Notes unavailable</strong>
          <span>The workspace could not list this folder's notes.</span>
          <button type="button" onClick={() => void notesQuery.refetch()}>
            Retry
          </button>
        </div>
      ) : notesQuery.isPending ? (
        <div className="search-workspace__state" role="status">
          Loading notes…
        </div>
      ) : notes.length === 0 ? (
        <div className="search-workspace__state" role="status">
          <strong>No notes in {folderLabel(folder)}</strong>
        </div>
      ) : (
        <ol className="search-workspace__results">
          {notes.map((note) => (
            <li key={note.path}>
              <article className="search-workspace__result">
                <NoteResultLink target={publicOntologyListItemRef(note)} onOpenNote={onOpenNote}>
                  {note.title || note.path}
                </NoteResultLink>
                <div className="search-workspace__result-source">
                  <span className="search-workspace__result-kind">
                    Note ·{" "}
                    <span>{humanizeName(publicTypeName(note.resolvedType)) || "Untyped"}</span>
                  </span>
                  <span aria-hidden="true"> · </span>
                  <SourcePath path={note.path} />
                </div>
              </article>
            </li>
          ))}
        </ol>
      )}
    </main>
  );
}

export function RankedResults({
  matches,
  query,
  onOpenNote,
}: {
  matches: WorkspaceSearchMatch[];
  query: string;
  onOpenNote: SearchWorkspaceProps["onOpenNote"];
}) {
  return matches.map((match, index) => {
    const excerpt = excerptFor(match);
    const line = match.startLine && match.startLine > 0 ? match.startLine : undefined;

    return (
      <li key={`${match.type}:${match.path}:${match.anchorID || match.nodeID || index}`}>
        <article className="search-workspace__result">
          {isNoteMatch(match) ? (
            <NoteResultLink target={noteTargetForMatch(match)} onOpenNote={onOpenNote}>
              {highlightLiteral(resultLabel(match), query)}
            </NoteResultLink>
          ) : (
            <button
              type="button"
              className="search-workspace__result-link search-workspace__result-link--code link-quiet link-title"
              onClick={() => openCodeSource(match.path || "", line)}
            >
              {highlightLiteral(resultLabel(match), query)}
            </button>
          )}
          <div className="search-workspace__result-source">
            <span className="search-workspace__result-kind">
              {[
                humanizeName(match.type) || "Source",
                humanizeName(publicTypeName(match.noteType)),
                matchedAs(match),
                match.heading !== resultLabel(match) ? match.heading : "",
              ]
                .filter(Boolean)
                .join(" · ")}
            </span>
            <span aria-hidden="true"> · </span>
            <SourcePath path={`${sourceLabel(match)}${line ? `:${line}` : ""}`} />
          </div>
          {excerpt && match.type === "code" ? (
            <CodeExcerpt excerpt={excerpt} query={query} />
          ) : (
            <p className={excerpt ? undefined : "is-missing"}>
              {excerpt
                ? noteExcerptParts(excerpt).map((part, partIndex) =>
                    part.code ? (
                      // oxlint-disable-next-line react/no-array-index-key -- parts have no identity beyond order
                      <code key={partIndex}>{highlightLiteral(part.text, query)}</code>
                    ) : (
                      highlightLiteral(part.text, query)
                    ),
                  )
                : "Excerpt unavailable."}
            </p>
          )}
        </article>
      </li>
    );
  });
}

function CodeExcerpt({ excerpt, query }: { excerpt: string; query: string }) {
  const { signature, doc } = codeExcerptParts(excerpt);
  const [expanded, setExpanded] = useState(false);
  const lines = signature.split("\n");
  const truncated = lines.length > 3;
  const visible = truncated && !expanded ? lines.slice(0, 3).join("\n") : signature;

  return (
    <>
      <pre className="search-workspace__code">{highlightLiteral(visible, query)}</pre>
      {truncated && (
        <button
          type="button"
          className="list-more"
          aria-expanded={expanded}
          onClick={() => setExpanded((value) => !value)}
        >
          <DisclosureChevron open={expanded} />
          {expanded ? "Show less" : "Show more"}
        </button>
      )}
      {doc && <p>{highlightLiteral(doc, query)}</p>}
    </>
  );
}
