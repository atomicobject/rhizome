import type { ReactNode } from "react";
import type { Components as MarkdownComponents } from "react-markdown";
import type { RenderedSection } from "../api/types";
import { convertWikilinks } from "../lib/content";
import { Markdown } from "./markdown/Markdown";

type Props = {
  section: RenderedSection;
  children: ReactNode;
  markdownComponents?: MarkdownComponents;
};

/**
 * TypedSectionCard wraps a section whose ontology type declares `@preview`.
 * The compact row renders the interpolated template (already resolved server
 * side, so the frontend runs it through the shared Markdown renderer), a `<details>`
 * block wraps the body so authors can expand to read the full content.
 *
 * The card intentionally leaves body rendering to the caller — the caller
 * passes the ReactMarkdown-rendered own-content as `children` so the same
 * markdown pipeline (wikilink conversion, custom link components) stays in
 * charge of link resolution inside section bodies.
 */
export function TypedSectionCard({ section, children, markdownComponents }: Props) {
  const template = section.previewTemplate || "";
  const preview = template ? convertWikilinks(template) : "";
  const properties = section.properties || {};
  const propertyEntries = Object.entries(properties);
  const open = !section.collapsed;

  return (
    <div className="typed-section-card">
      <details className="typed-section-card__details" open={open}>
        <summary className="typed-section-card__summary">
          <span className="typed-section-card__preview">
            {preview ? (
              <Markdown
                urlTransform={(url) => url}
                components={{
                  // Flatten ReactMarkdown's default paragraph wrapper so the
                  // compact row stays on a single visual line.
                  p: ({ children }) => <>{children}</>,
                  ...markdownComponents,
                }}
              >
                {preview}
              </Markdown>
            ) : (
              section.title
            )}
          </span>
          {propertyEntries.length > 0 && (
            <span className="typed-section-card__chips">
              {propertyEntries.map(([key, value]) => (
                <span key={key} className="typed-section-card__chip">
                  <span className="typed-section-card__chip-key">{key}</span>
                  <span className="typed-section-card__chip-value">{value}</span>
                </span>
              ))}
            </span>
          )}
        </summary>
        <div className="typed-section-card__body">{children}</div>
      </details>
    </div>
  );
}
