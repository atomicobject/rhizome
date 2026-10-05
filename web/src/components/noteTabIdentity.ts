import type { NodeRef } from "../api/types";
import type { NotesLocation } from "./notesRoute";

export function noteTabID(path: string, ref?: NodeRef) {
  if (!ref || (ref.kind === "NOTE" && !ref.fragment)) return `note:${path}`;

  return `note:${path}:node:${encodeURIComponent(JSON.stringify([ref.kind, ref.nodeId || ref.fragment || ref.structuralFingerprint || ""]))}`;
}

export function locationNodeRef(
  location: Pick<NotesLocation, "note" | "fragment" | "nodeKind" | "nodeId" | "structural">,
): NodeRef | undefined {
  if (!location.note || !location.nodeKind) return undefined;

  return {
    notePath: location.note,
    kind: location.nodeKind,
    nodeId: location.nodeId ?? undefined,
    fragment: location.fragment ?? undefined,
    structuralFingerprint: location.structural ?? undefined,
  };
}
