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
export function NarrativeView({ block, context }: BodyRendererProps) {
  const markdown = convertWikilinks(block.markdown || "");

  if (!markdown.trim()) return null;

  return (
    <div className="body-narrative">
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
