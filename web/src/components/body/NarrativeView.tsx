import { convertWikilinks, resolveRenderedLinkTarget } from "../../lib/content";
import { Markdown } from "../markdown/Markdown";
import { NoteLinkPreview } from "../notePreview/NoteLinkPreview";
import type { BodyRendererProps } from "./registry";

/**
 * Read-only renderer for a narrative (prose) body block. Renders the raw
 * markdown through the shared renderer with wikilink conversion + link-target
 * resolution so vault-relative links follow the Notes tab open rules.
 *
 * The edit-mode counterpart (`NarrativeEditor`) is a separate component;
 * swap via the registry instead of branching here.
 */
export function NarrativeView({ block, kindOrdinal, node, context }: BodyRendererProps) {
  const markdown = convertWikilinks(block.markdown || "");

  if (!markdown.trim()) return null;

  return (
    <div className={narrativeClassName("body-narrative", { block, kindOrdinal, node })}>
      <Markdown
        urlTransform={(url) => url}
        components={{
          a: ({ href, children }) => {
            const target = resolveRenderedLinkTarget(href, context.rendered || null);

            if (target && context.onOpen) {
              return (
                <NoteLinkPreview
                  target={target}
                  from={context.rendered?.path || context.workspace.content.path}
                  open={context.onOpen}
                >
                  {children}
                </NoteLinkPreview>
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
  );
}

// A short label, then only links and separators: "Home: [[A]] · [[B]]".
const LEAD_METADATA_LINE =
  /^[\p{L}][\p{L}\p{N} ]{0,23}:(?:\s*(?:\[\[[^\]\n]+\]\]|\[[^\]\n]*\]\([^)\n]*\))\s*[·•|,/–—-]?)+\s*$/u;

/**
 * The class for a narrative block. A note's lead line that holds only a label
 * and links is metadata about the note, so it reads quietly in both modes.
 */
export function narrativeClassName(
  base: string,
  { block, kindOrdinal, node }: Pick<BodyRendererProps, "block" | "kindOrdinal" | "node">,
): string {
  const lead =
    node.kind === "note" &&
    kindOrdinal === 0 &&
    node.body?.find((candidate) => candidate.kind !== "inline_field") === block;

  return lead && LEAD_METADATA_LINE.test((block.markdown || "").trim())
    ? `${base} body-narrative--meta`
    : base;
}
