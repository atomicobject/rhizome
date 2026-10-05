import { syntaxTree } from "@codemirror/language";
import { type EditorState, type Extension, type Range, StateField } from "@codemirror/state";
import { Decoration, type DecorationSet, EditorView, WidgetType } from "@codemirror/view";
import { renderMermaid } from "../markdown/mermaid";

const svgCache = new Map<string, Promise<string>>();

// ponytail: Keep the SVG cache at a 64-entry ceiling.
const SVG_CACHE_MAX_ENTRIES = 64;

let renderId = 0;

function cachedSvg(source: string) {
  let result = svgCache.get(source);

  if (!result) {
    renderId += 1;
    result = renderMermaid(source, `mermaid-editor-${renderId}`).catch((error: Error) => {
      if (svgCache.get(source) === result) svgCache.delete(source);
      throw error;
    });
    svgCache.set(source, result);

    if (svgCache.size > SVG_CACHE_MAX_ENTRIES) {
      const oldest = svgCache.keys().next().value;

      if (oldest !== undefined) svgCache.delete(oldest);
    }
  }

  return result;
}

class MermaidWidget extends WidgetType {
  constructor(
    readonly source: string,
    readonly sourcePosition: number,
  ) {
    super();
  }

  eq(other: MermaidWidget) {
    return other.source === this.source && other.sourcePosition === this.sourcePosition;
  }

  toDOM(view: EditorView) {
    const container = document.createElement("div");
    container.className = "mermaid-block mermaid-block--editor mermaid-block--loading";
    container.setAttribute("role", "button");
    container.tabIndex = 0;
    container.setAttribute("aria-label", "Mermaid diagram, press Enter to edit source");
    container.textContent = "Rendering diagram…";
    void cachedSvg(this.source).then(
      (svg) => {
        container.classList.remove("mermaid-block--loading", "mermaid-block--error");
        container.innerHTML = svg;
      },
      (error: Error) => {
        container.classList.remove("mermaid-block--loading");
        container.classList.add("mermaid-block--error");
        container.textContent = `Could not render diagram: ${error.message}`;
        container.setAttribute(
          "aria-label",
          "Mermaid diagram failed to render, press Enter to edit source",
        );
      },
    );

    const openSource = (event: Event) => {
      event.preventDefault();
      event.stopPropagation();
      view.focus();
      view.dispatch({ selection: { anchor: this.sourcePosition }, scrollIntoView: true });
    };

    container.addEventListener("mousedown", openSource);
    container.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") openSource(event);
    });

    return container;
  }

  ignoreEvent(event: Event) {
    return event.type === "mousedown" || event.type === "click" || event.type === "keydown";
  }
}

function selectionTouches(state: EditorState, from: number, to: number) {
  return state.selection.ranges.some((range) => range.from <= to && range.to >= from);
}

function buildMermaidBlocks(state: EditorState): DecorationSet {
  const ranges: Range<Decoration>[] = [];
  const tree = syntaxTree(state);
  tree.iterate({
    enter(node) {
      if (node.name !== "FencedCode") return;

      if (node.node.getChildren("CodeMark").length !== 2) return false;
      const info = node.node.getChild("CodeInfo");

      if (!info || state.doc.sliceString(info.from, info.to).trim() !== "mermaid") return;

      if (selectionTouches(state, node.from, node.to)) return false;
      const content = node.node.getChild("CodeText");
      const source = content ? state.doc.sliceString(content.from, content.to) : "";
      const firstContentPosition = Math.min(state.doc.lineAt(node.from).to + 1, node.to);
      ranges.push(
        Decoration.replace({
          block: true,
          widget: new MermaidWidget(source, firstContentPosition),
        }).range(node.from, node.to),
      );

      return false;
    },
  });

  return Decoration.set(ranges, true);
}

const mermaidBlocksField = StateField.define<DecorationSet>({
  create: buildMermaidBlocks,
  update(decorations, transaction) {
    if (
      transaction.docChanged ||
      transaction.selection ||
      syntaxTree(transaction.state) !== syntaxTree(transaction.startState)
    ) {
      return buildMermaidBlocks(transaction.state);
    }

    return decorations;
  },
  provide: (field) => EditorView.decorations.from(field),
});

export function mermaidBlocks(): Extension {
  return mermaidBlocksField;
}
