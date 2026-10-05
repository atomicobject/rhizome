import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";

import type { OntologyEditSessionResponse, ViewExecuteResponse } from "../api/types";
import {
  INITIAL_EDIT_READ_LIFECYCLE,
  useStagedQuery,
  type EditReadLifecycle,
} from "../staging/stagedQuery";
import { ConfiguredView } from "./ConfiguredView";
import { baseView, execution } from "./ConfiguredView.testFixtures";

it("keeps the displayed reorder when Save finishes before staged rows arrive", async () => {
  const rows = ["Alpha", "Bravo", "Charlie"].map((title, i) => ({
    ref: { notePath: `ideas/${title}.md`, kind: "NOTE" as const },
    path: `ideas/${title}.md`,
    title,
    fields: { rank: i + 1 },
  }));

  const stale = execution(rows);
  stale.groups = [];
  // SAFETY: the generated execution state has no assignable concrete shape.
  stale.state = {
    ...stale.state,
    sort: [{ field: "rank", direction: "asc" }],
  } as typeof stale.state;
  stale.capabilities = [
    ...(stale.capabilities ?? []),
    {
      key: "rank",
      label: "Rank",
      sortable: true,
      indexedSortable: true,
      groupable: false,
      filterOps: ["eq"],
      valueKind: "real",
      edit: { kind: "number", operation: "setField", field: "rank", valueKind: "real" },
    },
  ];

  const session: OntologyEditSessionResponse = {
    sessionId: "save-probe",
    revision: 1,
    status: "dirty",
    hasUncommittedChanges: true,
    createdAt: "2026-10-02T00:00:00Z",
    updatedAt: "2026-10-02T00:00:00Z",
    ops: [{ kind: "setField", path: "ideas/Alpha.md", field: "rank", value: "4" }],
  };

  let canonical!: (data: ViewExecuteResponse) => void;

  function View({
    session,
    lifecycle,
  }: {
    session: OntologyEditSessionResponse | null;
    lifecycle: EditReadLifecycle;
  }) {
    const query = useStagedQuery({
      subject: ["save-view"],
      session,
      lifecycle,
      queryFn: () =>
        lifecycle.revision
          ? new Promise<ViewExecuteResponse>((resolve) => {
              canonical = resolve;
            })
          : session
            ? new Promise<ViewExecuteResponse>(() => {})
            : Promise.resolve(stale),
    });

    return (
      <ConfiguredView
        view={baseView}
        execution={query.displayData ?? null}
        loading={query.isFetching}
        error={null}
        state={{}}
        onStateChange={() => {}}
        onRefresh={() => {}}
        onOpenRow={() => {}}
        editSession={query.displaySession}
        stagedEditsPending={query.savedEditsPending}
        onStageOps={async () => {}}
      />
    );
  }

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const view = (active: typeof session | null, lifecycle = INITIAL_EDIT_READ_LIFECYCLE) => (
    <QueryClientProvider client={client}>
      <View session={active} lifecycle={lifecycle} />
    </QueryClientProvider>
  );

  const { rerender } = render(view(null));

  const order = () =>
    screen
      .getAllByRole("row")
      .slice(1)
      .map((row) => rows.find((item) => row.textContent?.includes(item.title))?.title)
      .filter(Boolean);

  await waitFor(() => expect(order()).toEqual(["Alpha", "Bravo", "Charlie"]));
  rerender(view(session));
  expect(order()).toEqual(["Bravo", "Charlie", "Alpha"]);
  rerender(view(null, { revision: 1, outcome: "saved", savedSession: session }));
  expect(order()).toEqual(["Bravo", "Charlie", "Alpha"]);
  const saved = { ...stale, rows: [rows[1], rows[2], { ...rows[0], fields: { rank: 4 } }] };
  canonical(saved);
  await waitFor(() => expect(screen.queryByText("Refreshing…")).not.toBeInTheDocument());
  expect(order()).toEqual(["Bravo", "Charlie", "Alpha"]);
});
