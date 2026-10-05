import { type ComponentProps, useMemo, useState, type ReactNode } from "react";

import type { ViewExecuteRequest, ViewExecuteResponse, ViewTableRow } from "../api/types";
import { ConfiguredView } from "./ConfiguredView";
import { baseView, execution, firstRow, secondRow } from "./ConfiguredView.testFixtures";
import { type RecordRail, RecordRailProvider } from "./recordRail";

export const STATUS = "frontmatter.spec-status";

export function typeExecution(
  overrides: Partial<ViewExecuteResponse> = {},
  rows: ViewTableRow[] = [summaryRow(firstRow, "Alpha defines the first contract."), secondRow],
): ViewExecuteResponse {
  const base = execution(rows);

  return {
    ...base,
    capabilities: (base.capabilities ?? []).map((capability) =>
      capability.key === STATUS
        ? {
            ...capability,
            valueKind: "enum",
            enumValues: [
              { value: "draft", label: "Draft", tone: "neutral", stage: "open" },
              { value: "active", label: "Active", tone: "progress", stage: "active" },
              { value: "archived", label: "Archived", tone: "muted", stage: "dropped" },
            ],
          }
        : capability.key === "frontmatter.id"
          ? { ...capability, importance: "KEY", policyReason: "Specs are cited by id." }
          : capability,
    ),
    profile: {
      // oxlint-disable-next-line anti-slop/no-shape-in-symbol-names -- TypeProfile.shape is the generated API contract field.
      shape: "workflow",
      lifecycleField: STATUS,
      summaryField: "summary",
      gapFields: ["frontmatter.id"],
    },
    stats: {
      total: 40,
      issueCount: 0,
      staleCount: 3,
      lifecycle: [
        { value: "active", count: 31 },
        { value: "draft", count: 9 },
      ],
      fields: [
        { field: "title", filled: 40 },
        { field: STATUS, filled: 40 },
        { field: "frontmatter.id", filled: 28 },
        { field: "updatedAt", filled: 40 },
      ],
    },
    ...overrides,
  };
}

export function summaryRow(row: ViewTableRow, summary: string): ViewTableRow {
  return { ...row, fields: { ...row.fields, summary } };
}

export function RailHarness({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const [views, setViews] = useState(0);

  const rail: RecordRail = useMemo(
    () => ({
      slot,
      acquire: () => setViews((count) => count + 1),
      release: () => setViews((count) => count - 1),
    }),
    [slot],
  );

  return (
    <RecordRailProvider value={rail}>
      {children}
      <aside aria-label="Rail" data-open={views > 0}>
        <div ref={setSlot} />
      </aside>
    </RecordRailProvider>
  );
}

export function monthKey(offset: number) {
  const now = new Date();
  const date = new Date(now.getFullYear(), now.getMonth() + offset, 1);

  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}`;
}

/** A type collection view element, for tests that rerender it with new props. */
export function typeView(props: Partial<ComponentProps<typeof ConfiguredView>> = {}) {
  return (
    <ConfiguredView
      view={baseView}
      execution={typeExecution()}
      loading={false}
      error={null}
      state={{}}
      onStateChange={() => {}}
      onRefresh={() => {}}
      onOpenRow={() => {}}
      {...props}
    />
  );
}

/** An execution that ran with this request state, as the server echoes it. */
export function executedWith(
  state: ViewExecuteRequest,
  overrides: Partial<ViewExecuteResponse> = {},
): ViewExecuteResponse {
  const base = typeExecution(overrides);

  // SAFETY: the generated state type intersects ViewExecuteRequest with
  // Record<string, never>, so no literal is assignable; these are its request fields.
  return { ...base, state: { ...base.state, ...state } as ViewExecuteResponse["state"] };
}
