import { useEffect, useMemo, useRef } from "react";
import type { ViewExecuteResponse, ViewFieldCapability } from "../api/types";
import { capabilityFor, groupValueLabel } from "./ConfiguredViewModel";

type ViewGroup = NonNullable<ViewExecuteResponse["groups"]>[number];

export type CollapsedGroupNote = { key: string; label: string; count: number };

export function useCollapsedGroupNotes(
  groups: ViewGroup[],
  choices: Record<string, boolean>,
  capabilities: ViewFieldCapability[],
  onChange?: (groups: CollapsedGroupNote[]) => void,
  includeNested = true,
) {
  const notes = useMemo(() => {
    const result: CollapsedGroupNote[] = [];

    function visit(items: ViewGroup[]) {
      for (const group of items) {
        if (!(choices[group.key] ?? !group.collapsedByDefault)) {
          result.push({
            key: group.key,
            label: groupValueLabel(
              capabilityFor(capabilities, group.field),
              group.value,
              group.label,
            ),
            count: group.totalCount ?? group.count,
          });
        } else if (includeNested) visit(group.children ?? []);
      }
    }

    visit(groups);

    return result;
  }, [groups, choices, capabilities, includeNested]);

  const reported = useRef("");
  useEffect(() => {
    const serialized = JSON.stringify(notes);

    if (reported.current === serialized) return;
    reported.current = serialized;
    onChange?.(notes);
  }, [notes, onChange]);
}
