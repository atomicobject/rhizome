import type { NodeWorkspace, RenderedSection, StructuralNode } from "../api/types";

export type NoteViewMode = "read" | "source";

export type NoteOutlineContext = {
  sections: RenderedSection[];
  /** Scroll the note to a section in place. */
  navigate: (section: RenderedSection) => void;
  /** The element rendering a section in the current view, when it has one. */
  target?: (section: RenderedSection) => HTMLElement | null;
};

export type NoteTabContext = {
  workspace: NodeWorkspace;
  outline: NoteOutlineContext;
  openNode?: (node: StructuralNode) => void;
};

export function sameNoteTabContext(
  current: NoteTabContext | null | undefined,
  next: NoteTabContext | null,
): boolean {
  return (
    current?.workspace === next?.workspace &&
    current?.outline.sections === next?.outline.sections &&
    current?.outline.navigate === next?.outline.navigate &&
    current?.outline.target === next?.outline.target &&
    current?.openNode === next?.openNode
  );
}
