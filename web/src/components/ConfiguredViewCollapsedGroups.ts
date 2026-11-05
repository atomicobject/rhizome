import { useEffect, useMemo, useRef } from "react";
import type { ViewFieldCapability } from "../api/types";
import { capabilityFor, groupValueLabel } from "./ConfiguredViewModel";
import type { ConfiguredTableGroup } from "./ConfiguredViewTableModel";

export type CollapsedGroupNote = { key: string; label: string; count: number };

export function useCollapsedGroupNotes(
  groups: ConfiguredTableGroup[],
  choices: Record<string, boolean>,
  capabilities: ViewFieldCapability[],
  onChange?: (groups: CollapsedGroupNote[]) => void,
) {
  const notes = useMemo(() => {
    const result: CollapsedGroupNote[] = [];

    function visit(items: ConfiguredTableGroup[]) {
      for (const group of items) {
        if (!(choices[group.key] ?? !group.collapsedByDefault)) {
          result.push({
            key: group.key,
            label: groupValueLabel(
              capabilityFor(capabilities, group.field),
              group.value,
              group.label,
            ),
            count: group.count,
          });
        } else visit(group.children);
      }
    }

    visit(groups);

    return result;
  }, [groups, choices, capabilities]);

  const reported = useRef("");
  useEffect(() => {
    const serialized = JSON.stringify(notes);

    if (reported.current === serialized) return;
    reported.current = serialized;
    onChange?.(notes);
  }, [notes, onChange]);
}
