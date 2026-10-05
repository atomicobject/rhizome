import { inlinePreview } from "@atomic-editor/editor";
import { markdown } from "@codemirror/lang-markdown";
import { forceParsing, syntaxTree } from "@codemirror/language";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderMermaid } from "../markdown/mermaid";
import { mermaidBlocks } from "./mermaidBlocks";

// oxlint-disable-next-line anti-slop/no-module-mocking -- The acceptance test requires a mocked renderMermaid boundary.
vi.mock("../markdown/mermaid", () => ({
  renderMermaid: vi.fn().mockResolvedValue("<svg></svg>"),
}));

const source = ["Before", "", "```mermaid", "graph TD", "A-->B", "```", "", "After"].join("\n");

// CodeMirror parses within a time budget when a state is created, so under a
// loaded test run the first tree can stop short of the fence and the widget
// appears only after background parsing. Finish the parse before asserting.
function finishParsing(view: EditorView): void {
  forceParsing(view, view.state.doc.length, 5000);
}

describe("mermaidBlocks", () => {
  let view: EditorView | null = null;

  afterEach(() => {
    view?.destroy();
    view = null;
    document.body.replaceChildren();
    vi.mocked(renderMermaid).mockClear();
  });

  it("replaces a Mermaid fence outside the selection and preserves source bytes", async () => {
    const parent = document.createElement("div");
    document.body.appendChild(parent);
    view = new EditorView({
      parent,
      state: EditorState.create({
        doc: source,
        selection: { anchor: 0 },
        extensions: [markdown(), inlinePreview(), mermaidBlocks()],
      }),
    });
    finishParsing(view);

    const widget = parent.querySelector<HTMLElement>(".mermaid-block--editor");
    expect(widget).not.toBeNull();
    expect(view.state.doc.toString()).toBe(source);
    expect(renderMermaid).toHaveBeenCalledTimes(1);

    const insideFence = source.indexOf("graph TD");
    widget?.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, button: 0 }));
    expect(parent.querySelectorAll(".mermaid-block--editor")).toHaveLength(0);
    expect(view.state.selection.main.anchor).toBe(insideFence);
    expect(view.state.doc.toString()).toBe(source);

    view.dispatch({ selection: { anchor: 0 } });
    expect(parent.querySelectorAll(".mermaid-block--editor")).toHaveLength(1);
    expect(view.state.doc.toString()).toBe(source);
    expect(renderMermaid).toHaveBeenCalledTimes(1);
  });

  it("leaves an unclosed Mermaid fence and following paragraphs visible", () => {
    const unclosed = [
      "Before",
      "",
      "```mermaid",
      "graph TD",
      "A-->B",
      "",
      "First paragraph",
      "",
      "Second paragraph",
    ].join("\n");

    const parent = document.createElement("div");
    document.body.appendChild(parent);
    view = new EditorView({
      parent,
      state: EditorState.create({
        doc: unclosed,
        selection: { anchor: 0 },
        extensions: [markdown(), mermaidBlocks()],
      }),
    });
    finishParsing(view);

    const fencedCode = syntaxTree(view.state).topNode.getChild("FencedCode");
    expect(fencedCode?.getChildren("CodeMark")).toHaveLength(1);
    expect(parent.querySelector(".mermaid-block--editor")).toBeNull();
    expect(view.contentDOM).toHaveTextContent("First paragraph");
    expect(view.contentDOM).toHaveTextContent("Second paragraph");
  });

  it.each(["Enter", " "])("opens Mermaid source with the %s key", (key) => {
    const parent = document.createElement("div");
    document.body.appendChild(parent);
    view = new EditorView({
      parent,
      state: EditorState.create({
        doc: source,
        selection: { anchor: 0 },
        extensions: [markdown(), mermaidBlocks()],
      }),
    });
    finishParsing(view);

    const widget = parent.querySelector<HTMLElement>(".mermaid-block--editor");
    expect(widget).toHaveAttribute("role", "button");
    expect(widget).toHaveAttribute("tabindex", "0");
    expect(widget).toHaveAccessibleName("Mermaid diagram, press Enter to edit source");

    widget?.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key }));

    expect(parent.querySelector(".mermaid-block--editor")).toBeNull();
    expect(view.state.selection.main.anchor).toBe(source.indexOf("graph TD"));
  });

  it("evicts the oldest SVG after 64 cached diagrams", () => {
    const parent = document.createElement("div");
    document.body.appendChild(parent);

    const diagrams = Array.from({ length: 65 }, (_, index) =>
      ["Before", "", "```mermaid", `graph TD; A${index}-->B${index}`, "```"].join("\n"),
    );

    for (const diagram of diagrams) {
      view = new EditorView({
        parent,
        state: EditorState.create({
          doc: diagram,
          selection: { anchor: 0 },
          extensions: [markdown(), mermaidBlocks()],
        }),
      });
      finishParsing(view);
      view.destroy();
      view = null;
      parent.replaceChildren();
    }

    expect(renderMermaid).toHaveBeenCalledTimes(65);

    view = new EditorView({
      parent,
      state: EditorState.create({
        doc: diagrams[0],
        selection: { anchor: 0 },
        extensions: [markdown(), mermaidBlocks()],
      }),
    });
    finishParsing(view);

    expect(renderMermaid).toHaveBeenCalledTimes(66);
  });
});
