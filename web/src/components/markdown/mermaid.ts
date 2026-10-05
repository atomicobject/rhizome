// Mermaid cannot read CSS custom properties, so the workbench palette from
// web/src/base.css is repeated here: warm neutrals for nodes, teal for edges
// because teal means connection, gold for notes.
const RHIZOME_THEME = {
  fontFamily: '"Barlow", -apple-system, system-ui, "Segoe UI", sans-serif',
  fontSize: "13px",
  background: "#ffffff",
  mainBkg: "#fbfbfa",
  primaryColor: "#fbfbfa",
  primaryTextColor: "#2a2724",
  primaryBorderColor: "#c9c6bf",
  secondaryColor: "#d8f7f5",
  secondaryTextColor: "#0b6a66",
  secondaryBorderColor: "#11b5ae",
  tertiaryColor: "#f4f3ef",
  tertiaryTextColor: "#2a2724",
  tertiaryBorderColor: "#e4e2dd",
  lineColor: "#0b6a66",
  textColor: "#2a2724",
  nodeBorder: "#c9c6bf",
  clusterBkg: "#f4f3ef",
  clusterBorder: "#e4e2dd",
  edgeLabelBackground: "#ffffff",
  noteBkgColor: "#fbefd4",
  noteBorderColor: "#dcad66",
  noteTextColor: "#8a5a0f",
  titleColor: "#2a2724",
};

let mermaidPromise: Promise<(typeof import("mermaid"))["default"]> | null = null;

function loadMermaid() {
  if (!mermaidPromise) {
    mermaidPromise = import("mermaid").then(({ default: mermaid }) => {
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        theme: "base",
        themeVariables: RHIZOME_THEME,
      });

      return mermaid;
    });
  }

  return mermaidPromise;
}

export async function renderMermaid(code: string, id: string): Promise<string> {
  const mermaid = await loadMermaid();
  const { svg } = await mermaid.render(id, code);

  return svg;
}
