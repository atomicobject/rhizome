import type { WikiLinkResolvedTarget, WikiLinkSuggestion } from "@atomic-editor/editor";
import type { MutableRefObject } from "react";

export type MarkdownEditorLinks = {
  onLinkClick?: (url: string) => void;
  onOpenWikiLink?: (target: string) => void;
  resolveWikiLink?: (
    target: string,
  ) => WikiLinkResolvedTarget | null | Promise<WikiLinkResolvedTarget | null>;
  suggestWikiLinks?: (query: string) => Promise<WikiLinkSuggestion[]>;
};

export type MarkdownEditorHandle = {
  focus(): void;
  getMarkdown(): string;
  isComposing(): boolean;
  revealLine(line: number): void;
};

export type MarkdownEditorProps = {
  value: string;
  onChange: (nextMarkdown: string) => void;
  onBlur?: () => void;
  onCompositionEnd?: () => void;
  onCommitShortcut?: () => void;
  inputId?: string;
  revealLine?: number;
  ariaLabel?: string;
  className?: string;
  readOnly?: boolean;
  documentId?: string;
  links?: MarkdownEditorLinks;
  editorRef?: MutableRefObject<MarkdownEditorHandle | null>;
};
