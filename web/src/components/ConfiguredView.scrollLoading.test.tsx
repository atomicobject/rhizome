import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type { ViewExecuteResponse, ViewTableRow } from "../api/types";
import {
  deferredReply,
  emptyValidationSummariesReply,
  jsonReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { baseView, execution, firstRow } from "./ConfiguredView.testFixtures";
import { ViewTab } from "./ViewTab";
import { viewStorageKey } from "./useViewState";

const http = withFakeFetch();

const EXECUTE = `/api/v1/views/${baseView.id}/execute`;

function spec(index: number): ViewTableRow {
  const path = `docs/specs/spec-${index}.md`;

  return { ...firstRow, ref: { notePath: path, kind: "NOTE" }, path, title: `Spec ${index}` };
}

/** The first `count` of six specs, as the server returns a window from offset 0. */
function window(count: number): ViewExecuteResponse {
  const rows = Array.from({ length: count }, (_, index) => spec(index + 1));

  return {
    ...execution(rows),
    groups: [],
    pageInfo: { total: 6, offset: 0, first: count, returned: count, hasMore: count < 6 },
  };
}

function setGeometry(element: Element, scrollHeight: number, clientHeight: number, top: number) {
  Object.defineProperties(element, {
    scrollHeight: { configurable: true, value: scrollHeight },
    clientHeight: { configurable: true, value: clientHeight },
    scrollTop: { configurable: true, writable: true, value: top },
  });
}

it("grows the table one request at a time, keeps its rows, and loads until they fill it", async () => {
  const grown = deferredReply<ViewExecuteResponse>();
  const last = deferredReply<ViewExecuteResponse>();
  const replies = [() => jsonReply(window(2)), () => grown.promise, () => last.promise];
  http
    .json("GET", "/api/v1/views", { views: [baseView] })
    .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
    .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply)
    .on("POST", EXECUTE, () => replies[http.count("POST", EXECUTE) - 1]());

  renderWithQueryClient(
    <ViewTab
      tab={{ id: `view:${baseView.id}`, kind: "view", viewId: baseView.id, title: "Specs" }}
      active
      editSession={{ session: null, vaultKey: "vault" }}
      onOpenNote={vi.fn()}
      onStageOps={async () => undefined}
      onOpenIssues={vi.fn()}
      onTitle={vi.fn()}
    />,
  );

  await screen.findByText("Spec 2");
  const wrap = document.querySelector(".configured-view__table-wrap")!;
  setGeometry(wrap, 2000, 600, 1300);
  fireEvent.scroll(wrap);
  await waitFor(() => expect(http.count("POST", EXECUTE)).toBe(2));
  expect(JSON.parse(http.requests("POST", EXECUTE)[1].body ?? "{}").page).toEqual({
    offset: 0,
    first: 202,
  });

  // The grown refetch keeps the loaded rows on screen, and scrolling waits for it.
  expect(await screen.findByText("Loading more rows…")).toBeVisible();
  expect(screen.getByText("Spec 1")).toBeVisible();
  expect(screen.queryByText("Refreshing view…")).toBeNull();
  fireEvent.scroll(wrap);
  expect(http.count("POST", EXECUTE)).toBe(2);

  // Four rows still leave the end in reach, so the next window loads without a scroll.
  setGeometry(wrap, 600, 600, 0);
  await act(async () => grown.resolve(window(4)));
  expect(await screen.findByText("Spec 4")).toBeVisible();
  await waitFor(() => expect(http.count("POST", EXECUTE)).toBe(3));

  await act(async () => last.resolve(window(6)));
  expect(await screen.findByText("Spec 6")).toBeVisible();
  fireEvent.scroll(wrap);
  expect(http.count("POST", EXECUTE)).toBe(3);
});

it("restores a saved later page as one window from the first row", async () => {
  globalThis.sessionStorage.setItem(
    viewStorageKey("state", "vault", baseView.id),
    JSON.stringify({ page: { offset: 4, first: 2 } }),
  );
  http
    .json("GET", "/api/v1/views", { views: [baseView] })
    .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
    .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply)
    .on("POST", EXECUTE, () => jsonReply(window(6)));

  try {
    renderWithQueryClient(
      <ViewTab
        tab={{ id: `view:${baseView.id}`, kind: "view", viewId: baseView.id, title: "Specs" }}
        active
        editSession={{ session: null, vaultKey: "vault" }}
        onOpenNote={vi.fn()}
        onStageOps={async () => undefined}
        onOpenIssues={vi.fn()}
        onTitle={vi.fn()}
      />,
    );

    expect(await screen.findByText("Spec 1")).toBeVisible();

    const pages = http
      .requests("POST", EXECUTE)
      .map((request) => JSON.parse(request.body ?? "{}").page);

    expect(pages).toEqual([{ offset: 0, first: 6 }]);
  } finally {
    globalThis.sessionStorage.clear();
  }
});
