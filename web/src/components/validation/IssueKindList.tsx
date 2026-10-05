import type { ValidationIssueGroup } from "../../api/types";
import { checkLabel, issueLabel } from "./issueLabels";

/** A kind (check and code) or one of its variants; null selects every issue. */
export type KindSelection = { check: string; code?: string; variant?: string } | null;

export type KindRow = {
  key: string;
  selection: KindSelection;
  label: string;
  /** Kind rows: the variant they collapsed into, else their check. */
  meta?: string;
  count: number;
  nested: boolean;
  /** The issue kind's label and code, for the findings header. */
  kindLabel: string;
  code?: string;
  check?: string;
  variantLabel?: string;
};

export function kindSelectionKey(selection: KindSelection) {
  return selection
    ? JSON.stringify([selection.check, selection.code ?? "", selection.variant ?? ""])
    : "all";
}

/**
 * Column rows in display order: All, then each kind with its variants nested.
 * A kind whose only issues share one variant collapses into a single row.
 */
export function kindRows(groups: ValidationIssueGroup[], total: number): KindRow[] {
  const kinds = new Map<string, ValidationIssueGroup[]>();

  for (const group of groups) {
    const id = `${group.check}\u0000${group.code ?? ""}`;
    kinds.set(id, [...(kinds.get(id) ?? []), group]);
  }

  const all: KindRow = {
    key: "all",
    selection: null,
    label: "All issues",
    kindLabel: "All issues",
    count: groups.length ? groups.reduce((sum, group) => sum + group.issueCount, 0) : total,
    nested: false,
  };

  return [
    all,
    ...[...kinds.values()].flatMap((members) => {
      const [first] = members;
      const { check, code } = first;
      // Code-less kinds group by check, as their issues have nothing finer.
      const kindLabel = issueLabel(code ?? check);
      const variants = members.filter((group) => group.variant);
      const count = members.reduce((sum, group) => sum + group.issueCount, 0);

      const row = (selection: NonNullable<KindSelection>, fields: Partial<KindRow>): KindRow => ({
        key: kindSelectionKey(selection),
        selection,
        label: kindLabel,
        kindLabel,
        code,
        check,
        count,
        nested: false,
        ...fields,
      });

      if (members.length === 1 && first.variant) {
        return [
          row(
            { check, code, variant: first.variant.key },
            { meta: first.variant.label, variantLabel: first.variant.label },
          ),
        ];
      }

      // The server rolls variants past its per-code limit into one row.
      const rollup = members.find((group) => group.otherVariants);

      return [
        row({ check, code }, { meta: checkLabel(check) }),
        ...variants.map((group) =>
          row(
            { check, code, variant: group.variant?.key },
            {
              label: group.variant?.label ?? "",
              variantLabel: group.variant?.label,
              count: group.issueCount,
              nested: true,
            },
          ),
        ),
        ...(rollup
          ? [
              row(
                { check, code },
                {
                  key: `more:${kindSelectionKey({ check, code })}`,
                  label: `${rollup.otherVariants} more cases`,
                  count: rollup.issueCount,
                  nested: true,
                },
              ),
            ]
          : []),
      ];
    }),
  ];
}

export function IssueKindList({
  rows,
  selectedKey,
  onSelect,
}: {
  rows: KindRow[];
  selectedKey: string;
  onSelect: (row: KindRow) => void;
}) {
  const select = (row: KindRow) => {
    onSelect(row);
    window.requestAnimationFrame(() =>
      document
        .querySelector<HTMLButtonElement>(`[data-kind-key="${CSS.escape(row.key)}"]`)
        ?.focus(),
    );
  };

  return (
    <div
      className="problems-kinds"
      role="group"
      aria-label="Issue kinds"
      onKeyDown={(event) => {
        if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;

        const focused = event.target instanceof HTMLElement ? event.target.dataset.kindKey : null;
        const current = rows.findIndex((row) => row.key === focused);

        if (current < 0) return;
        event.preventDefault();

        const next = rows[current + (event.key === "ArrowDown" ? 1 : -1)];

        if (next) select(next);
      }}
    >
      {rows.map((row) => {
        const selected = row.key === selectedKey;

        return (
          <button
            key={row.key}
            type="button"
            className={`problems-kinds__row${row.nested ? " is-variant" : ""}${selected ? " is-selected" : ""}`}
            aria-current={selected ? "true" : undefined}
            tabIndex={selected ? 0 : -1}
            data-kind-key={row.key}
            title={row.meta ? `${row.label} · ${row.meta}` : row.label}
            aria-label={`${row.nested ? `${row.kindLabel}: ` : ""}${row.label}${row.meta ? `, ${row.meta}` : ""}, ${row.count} ${row.count === 1 ? "issue" : "issues"}`}
            onClick={() => onSelect(row)}
          >
            <span className="problems-kinds__label">{row.label}</span>
            <span className="problems-kind__count">{row.count}</span>
            {row.meta && <span className="problems-kinds__meta">{row.meta}</span>}
          </button>
        );
      })}
    </div>
  );
}
