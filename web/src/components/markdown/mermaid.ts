import type { DiagramType, RenderOptions } from "pele";

// ponytail: C4 emits $link links but rejects appended click directives; add types only after verifying both parsing and emitted links.
const LINK_TYPES = new Set<DiagramType>(["flowchart", "gantt", "class", "state", "agentflow"]);

/** Shared Pele layout and safety options for Markdown and custom views. */
export async function renderDiagram(
  code: string,
  id: string,
  links = false,
  linkIds: string[] = [],
  /** Container width: charts lay out at it, other diagrams shrink to fit it. */
  width?: number,
) {
  const { render, detectType } = await import("pele");
  const type = detectType(code);
  const linksSupported = type !== null && LINK_TYPES.has(type);
  const ignoredLinks = linkIds.length > 0 && !linksSupported;

  const source =
    linksSupported && links
      ? [
          code,
          ...linkIds.flatMap((nodeId, n) =>
            nodeId && !/\s/.test(nodeId) ? [`click ${nodeId} href "#rzm-link-${n}"`] : [],
          ),
        ].join("\n")
      : code;

  const options: RenderOptions = {
    idPrefix: id,
    fontFamily: '"Inter", -apple-system, system-ui, "Segoe UI", sans-serif',
    fontFamilyMono: '"JetBrains Mono", ui-monospace, Menlo, Consolas, monospace',
    fontSize: 13,
    links: links && !ignoredLinks,
    linkSchemes: [],
    images: false,
  };

  if (width) options.maxWidth = width;

  let result: ReturnType<typeof render>;
  const sized = width && type === "xychart" ? fitChartWidth(source, width) : source;

  try {
    result = render(sized, options);
  } catch (error) {
    if (sized === code) throw error;

    // Malformed generated click lines must not hide an otherwise valid diagram.
    result = render(code, { ...options, links: false });
  }

  return { ...result, linksSupported };
}

/**
 * Lay a chart out at `width` unless its author set one. Frontmatter config
 * replaces Pele's config option, so the width goes in as an init directive
 * after the frontmatter, which merges with it.
 */
export function fitChartWidth(code: string, width: number) {
  if (/xyChart[\s\S]*?\bwidth\b/.test(code)) return code;
  const frontmatter = /^\s*---\r?\n[\s\S]*?\r?\n---\r?\n/.exec(code)?.[0] ?? "";

  return `${frontmatter}%%{init: {"xyChart": {"width": ${width}}}}%%\n${code.slice(frontmatter.length)}`;
}

export async function renderMermaid(code: string, id: string): Promise<string> {
  return (await renderDiagram(code, id)).svg;
}
