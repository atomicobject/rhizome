import "@atomic-editor/editor/styles.css";

import {
  atomicEditorTheme,
  atomicMarkdownSyntax,
  autoCloseCodeFence,
  extendEmphasisPair,
  highlightMarkdown,
  inlinePreview,
  readOnlyExtension,
  startAsteriskList,
  tables,
  type WikiLinkResolvedTarget,
  wikiLinks,
} from "@atomic-editor/editor";
import { ATOMIC_CODE_LANGUAGES } from "@atomic-editor/editor/code-languages";
import { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import {
  deleteMarkupBackward,
  insertNewlineContinueMarkup,
  markdown,
  markdownLanguage,
} from "@codemirror/lang-markdown";
import { indentOnInput } from "@codemirror/language";
import { search, searchKeymap } from "@codemirror/search";
import { Compartment, EditorState, type Extension, Prec, Transaction } from "@codemirror/state";
import {
  drawSelection,
  dropCursor,
  EditorView,
  highlightActiveLine,
  highlightSpecialChars,
  keymap,
  rectangularSelection,
  ViewPlugin,
} from "@codemirror/view";
import type { MutableRefObject } from "react";
import { useEffect, useRef, useState } from "react";
import { frontmatterBlock } from "./frontmatter";
import { mermaidBlocks } from "./mermaidBlocks";
import type { MarkdownEditorHandle, MarkdownEditorLinks, MarkdownEditorProps } from "./types";

type EditorContentAttributes = { "aria-label": string } | { "aria-label": string; id: string };

type EditorCallbacks = Pick<
  MarkdownEditorProps,
  "onChange" | "onBlur" | "onCompositionEnd" | "onCommitShortcut"
> &
  MarkdownEditorLinks;

type CurrentRef<T> = { current: T };

const markdownListKeymap = Prec.highest(
  keymap.of([
    { key: "Enter", run: insertNewlineContinueMarkup },
    { key: "Backspace", run: deleteMarkupBackward },
  ]),
);

export type AtomicMarkdownEditorProps = MarkdownEditorProps;

export function AtomicMarkdownEditor({
  value,
  onChange,
  onBlur,
  onCompositionEnd,
  onCommitShortcut,
  inputId,
  revealLine,
  ariaLabel = "Markdown editor",
  className,
  readOnly = false,
  documentId = "markdown",
  links,
  editorRef,
}: AtomicMarkdownEditorProps) {
  const readonly = readOnly;
  const rootRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const composingRef = useRef(false);
  const [readOnlyCompartment] = useState(() => new Compartment());
  const [wikiLinksCompartment] = useState(() => new Compartment());
  const valueRef = useRef(value);
  const readonlyRef = useRef(readonly);
  const applyingExternalValueRef = useRef(false);

  const callbacksRef = useRef({
    onChange,
    onBlur,
    onCompositionEnd,
    onCommitShortcut,
    ...links,
  });

  useEffect(() => {
    callbacksRef.current = {
      onChange,
      onBlur,
      onCompositionEnd,
      onCommitShortcut,
      ...links,
    };
  }, [onChange, onBlur, onCompositionEnd, onCommitShortcut, links]);

  useEffect(() => {
    const root = rootRef.current;

    if (!root) return;

    const openNormalLink = (url: string) => {
      if (callbacksRef.current.onLinkClick) {
        callbacksRef.current.onLinkClick(url);

        return;
      }

      try {
        window.open(url, "_blank", "noopener,noreferrer");
      } catch {
        // Sandboxed browser contexts can reject window.open.
      }
    };

    const contentAttributes = editorContentAttributes(ariaLabel, inputId);

    const extensions: Extension[] = [
      highlightSpecialChars(),
      history(),
      drawSelection(),
      dropCursor(),
      EditorState.allowMultipleSelections.of(true),
      indentOnInput(),
      rectangularSelection(),
      highlightActiveLine(),
      closeBrackets(),
      startAsteriskList,
      extendEmphasisPair,
      autoCloseCodeFence,
      EditorView.lineWrapping,
      EditorView.contentAttributes.of(contentAttributes),
      search({ top: true }),
      markdown({
        base: markdownLanguage,
        codeLanguages: [...ATOMIC_CODE_LANGUAGES],
        extensions: [frontmatterBlock, highlightMarkdown],
        addKeymap: false,
      }),
      markdownLanguage.data.of({
        closeBrackets: {
          brackets: ["(", "[", "{", "'", '"', "*", "_", "`"],
        },
      }),
      atomicMarkdownSyntax,
      atomicEditorTheme,
      markdownListKeymap,
      keymap.of([
        {
          key: "Mod-Enter",
          run: () => {
            const callback = callbacksRef.current.onCommitShortcut;

            if (!callback) return false;
            callback();

            return true;
          },
        },
        ...closeBracketsKeymap,
        ...historyKeymap,
        ...searchKeymap,
        indentWithTab,
        ...defaultKeymap,
      ]),
      inlinePreview({ onLinkClick: openNormalLink }),
      tables(),
      mermaidBlocks(),
      wikiLinksCompartment.of(createWikiLinksExtension(callbacksRef)),
      accessibleAtomicWidgets(callbacksRef),
      EditorState.transactionFilter.of((transaction) =>
        readonlyRef.current && transaction.docChanged ? [] : transaction,
      ),
      readOnlyCompartment.of(readOnlyExtension(readonlyRef.current)),
      EditorView.updateListener.of((update) => {
        if (!update.docChanged || applyingExternalValueRef.current) return;
        callbacksRef.current.onChange(update.state.doc.toString());
      }),
      EditorView.domEventHandlers({
        blur: () => callbacksRef.current.onBlur?.(),
        compositionstart: () => {
          composingRef.current = true;
        },
        compositionend: () => {
          composingRef.current = false;
          callbacksRef.current.onCompositionEnd?.();
        },
      }),
    ];

    const view = new EditorView({
      parent: root,
      state: EditorState.create({ doc: valueRef.current, extensions }),
    });

    viewRef.current = view;
    const editorHandle = createEditorHandle(viewRef, composingRef);

    if (editorRef) editorRef.current = editorHandle;

    return () => {
      if (editorRef) editorRef.current = null;
      view.destroy();
      viewRef.current = null;
    };
  }, [ariaLabel, documentId, editorRef, inputId, readOnlyCompartment, wikiLinksCompartment]);

  useEffect(() => {
    const view = viewRef.current;

    if (view && revealLine) revealEditorLine(view, revealLine);
  }, [documentId, revealLine]);

  useEffect(() => {
    valueRef.current = value;
    const view = viewRef.current;

    if (!view || view.state.doc.toString() === value) return;
    const selection = view.state.selection.main;
    applyingExternalValueRef.current = true;

    try {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value },
        selection: {
          anchor: Math.min(selection.anchor, value.length),
          head: Math.min(selection.head, value.length),
        },
        annotations: Transaction.addToHistory.of(false),
      });
    } finally {
      applyingExternalValueRef.current = false;
    }
  }, [value]);

  useEffect(() => {
    readonlyRef.current = readonly;
    const view = viewRef.current;

    if (!view) return;
    view.dispatch({
      effects: readOnlyCompartment.reconfigure(readOnlyExtension(readonly)),
    });
  }, [readOnlyCompartment, readonly]);

  const previousResolverRef = useRef(links?.resolveWikiLink);
  useEffect(() => {
    if (previousResolverRef.current === links?.resolveWikiLink) return;
    previousResolverRef.current = links?.resolveWikiLink;
    const view = viewRef.current;

    if (!view) return;
    view.dispatch({
      effects: wikiLinksCompartment.reconfigure(createWikiLinksExtension(callbacksRef)),
    });
  }, [links?.resolveWikiLink, wikiLinksCompartment]);

  const rootClassName = [
    "markdown-editor",
    "markdown-editor--atomic",
    readonly ? "markdown-editor--readonly" : "",
    className ?? "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={rootClassName}>
      <div ref={rootRef} className="atomic-cm-editor" />
    </div>
  );
}

function editorContentAttributes(
  ariaLabel: string,
  inputId: string | undefined,
): EditorContentAttributes {
  if (inputId) return { "aria-label": ariaLabel, id: inputId };

  return { "aria-label": ariaLabel };
}

function createEditorHandle(
  viewRef: MutableRefObject<EditorView | null>,
  composingRef: MutableRefObject<boolean>,
): MarkdownEditorHandle {
  return {
    focus: () => viewRef.current?.focus(),
    revealLine: (line) => {
      const view = viewRef.current;

      if (view) revealEditorLine(view, line);
    },
    isComposing: () => composingRef.current || (viewRef.current?.composing ?? false),
    getMarkdown: () => viewRef.current?.state.doc.toString() || "",
  };
}

function revealEditorLine(view: EditorView, line: number): void {
  const lineNumber = Math.min(Math.max(Math.trunc(line), 1), view.state.doc.lines);
  const start = view.state.doc.line(lineNumber).from;
  view.dispatch({
    selection: { anchor: start },
    effects: EditorView.scrollIntoView(start, { y: "center" }),
  });
  view.focus();
}

function createWikiLinksExtension(callbacksRef: CurrentRef<EditorCallbacks>): Extension {
  return wikiLinks({
    suggest: async (query) =>
      callbacksRef.current.suggestWikiLinks ? callbacksRef.current.suggestWikiLinks(query) : [],
    resolve: async (target) => {
      const resolver = callbacksRef.current.resolveWikiLink;

      return resolver ? normalizeResolvedTarget(await resolver(target)) : null;
    },
    onOpen: (target) => callbacksRef.current.onOpenWikiLink?.(target),
    openOnClick: true,
  });
}

function accessibleAtomicWidgets(callbacksRef: CurrentRef<EditorCallbacks>): Extension {
  return ViewPlugin.fromClass(
    class {
      private readonly observer: MutationObserver;

      constructor(private readonly view: EditorView) {
        this.decorate();
        this.observer = new MutationObserver(() => this.decorate());
        this.observer.observe(view.contentDOM, {
          childList: true,
          subtree: true,
        });
        view.contentDOM.addEventListener("keydown", this.onKeyDown, true);
      }

      update() {
        this.decorate();
      }

      destroy() {
        this.observer.disconnect();
        this.view.contentDOM.removeEventListener("keydown", this.onKeyDown, true);
      }

      private readonly onKeyDown = (event: KeyboardEvent) => {
        if (event.key !== "Enter" && event.key !== " ") return;
        const target = event.target;

        if (!(target instanceof Element)) return;
        const link = target.closest<HTMLElement>("[data-wiki-link-target]");
        const wikiTarget = link?.dataset.wikiLinkTarget;

        if (!wikiTarget) return;
        event.preventDefault();
        event.stopPropagation();
        callbacksRef.current.onOpenWikiLink?.(wikiTarget);
      };

      private decorate() {
        for (const link of this.view.contentDOM.querySelectorAll<HTMLElement>(
          "[data-wiki-link-target]",
        )) {
          link.setAttribute("role", "link");
          link.tabIndex = 0;
          link.setAttribute(
            "aria-label",
            link.textContent?.trim() || link.dataset.wikiLinkTarget || "Link",
          );
        }

        for (const task of this.view.contentDOM.querySelectorAll<HTMLElement>(
          "input.cm-atomic-task-checkbox",
        )) {
          task.setAttribute("aria-label", "Toggle task");
        }
      }
    },
  );
}

function normalizeResolvedTarget(
  target: WikiLinkResolvedTarget | null,
): WikiLinkResolvedTarget | null {
  if (!target) return null;

  return {
    ...target,
    label: target.label || target.target,
    status: target.status ?? "resolved",
  };
}
