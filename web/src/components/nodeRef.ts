import type { NodeRef } from "../api/types";

export function canonicalNodeRefKey(ref: NodeRef | null | undefined): string {
  if (!ref) return "";

  return [
    ref.notePath || "",
    ref.fragment || "",
    ref.nodeId || "",
    ref.kind || "",
    ref.structuralFingerprint || "",
  ].join("|");
}

/**
 * Whether an event about `candidate` concerns the subscribed ref. The server
 * echoes the subscribed ref, but a pane's ref can gain or lose a structural
 * fingerprint between subscribing and dispatch, and older runtimes echoed the
 * resolved ref. A fingerprint therefore narrows the match only when both refs
 * carry one.
 */
export function nodeRefMatches(
  subscribed: NodeRef | null | undefined,
  candidate: NodeRef | null | undefined,
): boolean {
  if (!subscribed || !candidate) return false;

  if (
    canonicalNodeRefKey({ ...subscribed, structuralFingerprint: "" }) !==
    canonicalNodeRefKey({ ...candidate, structuralFingerprint: "" })
  ) {
    return false;
  }

  return (
    !subscribed.structuralFingerprint ||
    !candidate.structuralFingerprint ||
    subscribed.structuralFingerprint === candidate.structuralFingerprint
  );
}
