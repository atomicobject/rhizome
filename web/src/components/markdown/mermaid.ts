export async function renderMermaid(code: string, id: string): Promise<string> {
  const { render } = await import("pele");

  const { svg } = render(code, {
    idPrefix: id,
    fontFamily: '"Barlow", -apple-system, system-ui, "Segoe UI", sans-serif',
    fontFamilyMono: '"JetBrains Mono", ui-monospace, Menlo, Consolas, monospace',
    fontSize: 13,
    // Preserve strict rendering: authored diagrams cannot navigate or load media.
    links: false,
    images: false,
  });

  return svg;
}
