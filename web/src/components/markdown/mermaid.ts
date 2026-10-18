import type { DiagramType } from "pele";

// ponytail: C4 emits $link links but rejects appended click directives; add types only after verifying both parsing and emitted links.
const LINK_TYPES = new Set<DiagramType>(["flowchart", "gantt", "class", "state", "agentflow"]);

/** Shared Pele layout and safety options for Markdown and custom views. */
export async function renderDiagram(
  code: string,
  id: string,
  links = false,
  linkIds: string[] = [],
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

  const options = {
    idPrefix: id,
    fontFamily: '"Barlow", -apple-system, system-ui, "Segoe UI", sans-serif',
    fontFamilyMono: '"JetBrains Mono", ui-monospace, Menlo, Consolas, monospace',
    fontSize: 13,
    links: links && !ignoredLinks,
    linkSchemes: [],
    images: false,
  };

  let result: ReturnType<typeof render>;

  try {
    result = render(source, options);
  } catch (error) {
    if (source === code) throw error;

    // Malformed generated click lines must not hide an otherwise valid diagram.
    result = render(code, { ...options, links: false });
  }

  return { ...result, linksSupported };
}

export async function renderMermaid(code: string, id: string): Promise<string> {
  return (await renderDiagram(code, id)).svg;
}
