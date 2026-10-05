import { expect, it } from "vitest";

import type { ValidationIssueGroup } from "../../api/types";
import { kindRows, kindSelectionKey } from "./IssueKindList";

it("shows the server's roll-up of extra variants as one row that selects the kind", () => {
  const base = { check: "broken_links", code: "broken_note_link", applicableRepairCount: 0 };

  const groups: ValidationIssueGroup[] = [
    ...Array.from({ length: 25 }, (_, index) => ({
      ...base,
      variant: { key: `Target ${index}`, label: `Target ${index}` },
      issueCount: 1,
      affectedFileCount: 1,
    })),
    { ...base, issueCount: 3, affectedFileCount: 2, otherVariants: 2 },
  ];

  const rows = kindRows(groups, 28);
  const kind = { check: "broken_links", code: "broken_note_link" };

  // All, the kind, 25 variants, and the roll-up.
  expect(rows).toHaveLength(28);
  expect(rows[1]).toMatchObject({ count: 28, selection: kind });
  expect(rows.at(-2)?.label).toBe("Target 24");

  expect(rows.at(-1)).toMatchObject({
    label: "2 more cases",
    count: 3,
    nested: true,
    selection: kind,
  });
  expect(rows.at(-1)?.key).not.toBe(kindSelectionKey(kind));
});
