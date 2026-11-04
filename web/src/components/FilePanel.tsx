import { useEffect, useMemo, useRef } from "react";
import { FileText } from "lucide-react";
import type { FileView } from "../api/types";
import { convertWikilinks, resolveLinkTarget, stripFrontmatter } from "../lib/content";
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
  line,
  onClose,
  onSelectFile,
}: {
  file: FileView | null;
  line?: number;
  onClose: () => void;
  onSelectFile: (path: string, kind?: string) => void;
}) {
  const selectedLineRef = useRef<HTMLSpanElement>(null);

  const markdown = useMemo(() => {
    if (file?.kind !== "note") return "";

    return convertWikilinks(stripFrontmatter(file.content || ""));
  }, [file]);

  const topLinks = useMemo(() => {
    if (!file) return [];

    if (file.kind === "code") {
      return (file.relatedNotes || []).map((note) => ({
        label: note.title || note.path,
        path: note.path,
      }));
    }

    if (file.kind === "note") {
      const links = file.links || [];
      const out: { label: string; path: string }[] = [];
      links.forEach((link) => {
        const target = resolveLinkTarget(link.target, file);

        if (!target) return;
        out.push({ label: link.text || target, path: target });
      });

      return out;
    }

    return [];
  }, [file]);

  const codeMarkdown = useMemo(() => {
    if (file?.kind !== "code") return "";
    const lang = file.lang ? HIGHLIGHT_LANGS.get(file.lang) : undefined;

    return fencedCodeMarkdown(file.content || "", lang);
  }, [file]);

  const codeLines = file?.kind === "code" ? (file.content || "").split("\n") : [];
  const targetLine = line && line <= codeLines.length ? line : undefined;

  useEffect(() => {
    if (!targetLine) return;
    selectedLineRef.current?.scrollIntoView?.({ block: "center" });
  }, [file?.path, targetLine]);

  if (!file) return null;
  const label = file.kind === "note" ? "Note" : "Code";
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
          <button type="button" className="explorer-pane__close" onClick={onClose}>
            Close
          </button>
        </div>
      </header>

      <div className="explorer-pane__body">
        {file && topLinks.length > 0 && (
          <section className="explorer-pane__section">
            <h3>
              Links
              <span>{topLinks.length}</span>
            </h3>
            <ul className="explorer-pane__list">
              {topLinks.map((note) => (
                <li key={`${note.path}-${note.label}`}>
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
                    <span className="explorer-pane__list-name">{note.label}</span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        )}

        {file?.kind === "note" && (
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

        {file?.kind === "code" && (
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
