import { useEffect, useMemo, useRef } from "react";
import { FileText, X } from "lucide-react";
import { graphNodeNavigationTarget } from "./graphViewShared";
import { FILE_NOTE_RELATIONS } from "./fileFocusGraph";
import type { FileView, GraphResponse } from "../api/types";
import { convertWikilinks, resolveLinkTarget, stripFrontmatter } from "../lib/content";
import "./file-panel-preview.css";
import { Markdown } from "./markdown/Markdown";

const HIGHLIGHT_LANGS = new Map<string, string>([
  ["go", "go"],
  ["python", "python"],
  ["typescript", "typescript"],
  ["javascript", "javascript"],
  ["csharp", "cs"],
  ["rust", "rust"],
]);

function fencedCodeMarkdown(content: string, lang?: string) {
  const longestFence = Math.max(
    2,
    ...Array.from(content.matchAll(/`+/g), (match) => match[0].length),
  );

  const fence = "`".repeat(longestFence + 1);

  return `${fence}${lang || ""}\n${content}\n${fence}`;
}

function getFileName(path: string) {
  if (!path) return "";
  const parts = path.split("/");
  const name = parts[parts.length - 1] || "";

  return name.endsWith(".md") ? name.slice(0, -3) : name;
}

function getParentPath(path: string) {
  if (!path) return "";
  const parts = path.split("/");

  if (parts.length <= 1) return "";

  return parts.slice(0, -1).join("/");
}

export function FilePanel({
  file,
  graph,
  line,
  onClose,
  onSelectFile,
}: {
  file: FileView | null;
  graph?: GraphResponse;
  line?: number;
  onClose: () => void;
  onSelectFile: (path: string, kind?: string) => void;
}) {
  const selectedLineRef = useRef<HTMLSpanElement>(null);

  const textFile =
    file?.kind === "code" || file?.kind === "file" || (file?.kind === "note" && line !== undefined);

  const markdown = useMemo(() => {
    if (file?.kind !== "note") return "";

    return convertWikilinks(stripFrontmatter(file.content || ""));
  }, [file]);

  const linkGroups = useMemo(() => {
    if (!file) return [];
    const groups = new Map<string, Map<string, { label: string; path: string; count: number }>>();

    const add = (relation: string, path: string, label: string) => {
      const rows = groups.get(relation) ?? new Map();
      const previous = rows.get(path);
      rows.set(path, { path, label: previous?.label ?? label, count: (previous?.count ?? 0) + 1 });
      groups.set(relation, rows);
    };

    if (file.kind === "code") {
      for (const note of file.relatedNotes ?? [])
        add(
          note.reason === "coderef"
            ? "Documented by"
            : note.reason === "codeanchor"
              ? "Mentioned in"
              : "Related in graph",
          note.path,
          note.title || getFileName(note.path),
        );
      const known = new Set((file.relatedNotes ?? []).map((note) => note.path));

      for (const node of graph?.nodes ?? []) {
        const path = graphNodeNavigationTarget(node);

        if ((node.kind === "note" || node.kind === "embedded") && path && !known.has(path)) {
          add("Related in graph", path, node.label || getFileName(path));
          known.add(path);
        }
      }
    } else {
      for (const link of file.links ?? []) {
        const target = resolveLinkTarget(link.target, file);

        if (target) add("Links to", target, link.text || getFileName(target));
      }
    }

    return [...groups]
      .sort(([a], [b]) => FILE_NOTE_RELATIONS.indexOf(a) - FILE_NOTE_RELATIONS.indexOf(b))
      .map(([relation, rows]) => ({ relation, rows: [...rows.values()] }));
  }, [file, graph]);

  const codeMarkdown = useMemo(() => {
    if (!file || !textFile) return "";
    const lang = file.lang ? HIGHLIGHT_LANGS.get(file.lang) : undefined;

    return fencedCodeMarkdown(file.content || "", lang);
  }, [file, textFile]);

  const codeLines = textFile ? (file?.content || "").split("\n") : [];
  const targetLine = line && line <= codeLines.length ? line : undefined;

  useEffect(() => {
    if (!targetLine) return;
    selectedLineRef.current?.scrollIntoView?.({ block: "center" });
  }, [file?.path, targetLine]);

  if (!file) return null;
  const label = file.kind === "note" ? "Note" : file.kind === "code" ? "Code" : "File";
  const name = getFileName(file.path);
  const parent = getParentPath(file.path) || "/";

  return (
    <section className="explorer-pane">
      <header className="explorer-pane__header">
        <div className="explorer-pane__head-main">
          <div className="explorer-pane__label">{label}</div>
          <h2>{name}</h2>
          <span className="explorer-pane__path">
            {parent}
            {targetLine ? ` · line ${targetLine}` : ""}
          </span>
        </div>
        <div className="explorer-pane__actions">
          <button
            type="button"
            className="notes-right-rail__close"
            aria-label="Close file details"
            title="Close file details"
            onClick={onClose}
          >
            <X size={16} strokeWidth={1.5} aria-hidden="true" />
          </button>
        </div>
      </header>

      <div className="explorer-pane__body">
        {linkGroups.map(({ relation, rows }) => (
          <section className="explorer-pane__section" key={relation}>
            <h3>
              {relation}
              <span className="num">
                {rows.length}{" "}
                {file.kind === "code"
                  ? rows.length === 1
                    ? "note"
                    : "notes"
                  : rows.length === 1
                    ? "destination"
                    : "destinations"}
              </span>
            </h3>
            <ul className="explorer-pane__list">
              {rows.map((note) => (
                <li key={note.path}>
                  <button
                    type="button"
                    className="explorer-pane__list-link"
                    onClick={() =>
                      onSelectFile(note.path, file.kind === "code" ? "note" : undefined)
                    }
                    title={note.path}
                  >
                    <span className="explorer-pane__list-icon" aria-hidden="true">
                      <FileText size={16} strokeWidth={1.5} absoluteStrokeWidth />
                    </span>
                    <span className="explorer-pane__link-text">
                      <span className="explorer-pane__list-name link-title">{note.label}</span>
                      <span className="explorer-pane__link-path">
                        {note.path.length > 54
                          ? `${note.path.slice(0, 24)}…${note.path.slice(-29)}`
                          : note.path}
                      </span>
                    </span>
                    {note.count > 1 && (
                      <span
                        className="explorer-pane__multiplicity num"
                        title={`${note.count} mentions`}
                        aria-label={`${note.count} mentions`}
                      >
                        ×{note.count}
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ))}

        {file?.kind === "note" && !textFile && (
          <section className="explorer-pane__section">
            {file.frontmatter && Object.keys(file.frontmatter).length > 0 && (
              <div className="explorer-pane__frontmatter">
                {Object.entries(file.frontmatter).map(([key, value]) => (
                  <div key={key}>
                    <strong>{key}</strong>: {String(value)}
                  </div>
                ))}
              </div>
            )}
            <div className="explorer-pane__markdown">
              <Markdown
                components={{
                  a: ({ href, children }) => {
                    const target = resolveLinkTarget(href, file);

                    if (target) {
                      return (
                        <button
                          type="button"
                          className="markdown-link"
                          onClick={(e) => {
                            e.preventDefault();
                            onSelectFile(target);
                          }}
                        >
                          {children}
                        </button>
                      );
                    }

                    return (
                      <a href={href} target="_blank" rel="noreferrer">
                        {children}
                      </a>
                    );
                  },
                }}
              >
                {markdown}
              </Markdown>
            </div>
          </section>
        )}

        {textFile && (
          <section className="explorer-pane__section">
            <div className="explorer-pane__code">
              {targetLine ? (
                <pre className="explorer-pane__source-lines">
                  <code>
                    {codeLines.map((sourceLine, index) => {
                      const lineNumber = index + 1;
                      const selected = lineNumber === targetLine;

                      return (
                        <span
                          key={lineNumber}
                          ref={selected ? selectedLineRef : undefined}
                          className={selected ? "is-target" : undefined}
                          data-line={lineNumber}
                          aria-current={selected ? "location" : undefined}
                        >
                          {sourceLine || " "}
                          {"\n"}
                        </span>
                      );
                    })}
                  </code>
                </pre>
              ) : (
                <Markdown>{codeMarkdown}</Markdown>
              )}
            </div>
          </section>
        )}
      </div>
    </section>
  );
}
