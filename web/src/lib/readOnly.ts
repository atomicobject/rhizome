import type { NodeWorkspace } from "../api/types";

/** Shown for a structured record when no owner declared a reason: records have no write path yet. */
export const RECORD_READ_ONLY = "Change this record with the owning tool.";

/**
 * Why a node cannot be edited here, or undefined when it can. Every edit
 * affordance asks this one question: ownership and other read-only
 * capabilities apply to any node, Markdown or record.
 */
export function nodeReadOnlyReason(
  workspace: Pick<NodeWorkspace, "capabilities" | "content"> | null | undefined,
): string | undefined {
  if (!workspace) return undefined;

  return (
    workspace.capabilities.readOnlyReason ||
    (workspace.content.source ? RECORD_READ_ONLY : undefined)
  );
}
