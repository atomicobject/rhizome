import { lazy, Suspense } from "react";
import type { MarkdownEditorProps } from "./types";

const AtomicMarkdownEditor = lazy(() =>
  import("./AtomicMarkdownEditor").then((module) => ({
    default: module.AtomicMarkdownEditor,
  })),
);

export type { MarkdownEditorProps } from "./types";

export function MarkdownEditor({ editorRef, ...props }: MarkdownEditorProps) {
  return (
    <Suspense
      fallback={
        <div
          role="status"
          className="markdown-editor markdown-editor--loading"
          aria-label={props.ariaLabel || "Markdown editor"}
          aria-busy="true"
        />
      }
    >
      <AtomicMarkdownEditor {...props} editorRef={editorRef} />
    </Suspense>
  );
}
