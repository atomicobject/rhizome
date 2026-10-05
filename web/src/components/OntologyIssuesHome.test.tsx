import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ValidateEnvelope, ValidationDiagnostic } from "../api/types";
import { queryKeys } from "../api/queryKeys";
import {
  deferredReply,
  emptyValidationGroupsReply,
  emptyValidationSummariesReply,
  jsonReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { NotesIssuesHome } from "./NotesIssuesHome";

const http = withFakeFetch();

beforeEach(() => {
  sessionStorage.clear();
  http.on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);
  http.on("POST", "/api/v1/validation/groups", emptyValidationGroupsReply);
  http.json("GET", "/api/v1/validation/diagnostics", {
    generation: 1,
    filterIdentity: "all",
    sort: "diagnostic_order",
    returned: 0,
    total: 0,
    diagnostics: [],
  });
});

function cleanEnvelope(): ValidateEnvelope {
  return {
    status: "ok",
    health: "current_clean",
    generation: 1,
    publishedGeneration: 1,
    snapshot: {
      vaultIdentity: "vault",
      generation: 1,
      scope: "default",
      selectedChecks: ["broken_links"],
      startedAt: 1,
      finishedAt: 2,
      durationMs: 4,
      completion: "complete",
      issueCount: 0,
      errorCount: 0,
      affectedFileCount: 0,
      affectedNoteCount: 0,
      repairActionCount: 0,
      checks: [{ check: "broken_links", outcome: "completed", issueCount: 0, durationMs: 4 }],
    },
  };
}

const issueDiagnostics: ValidationDiagnostic[] = [
  {
    issueKey: "issue-1",
    check: "broken_links",
    code: "broken_link",
    message: "Target does not exist: Missing A",
    primaryPath: "docs/one.md",
    affectedPaths: ["docs/one.md"],
  },
  {
    issueKey: "issue-2",
    check: "broken_links",
    code: "broken_link",
    message: "Target does not exist: Missing B",
    primaryPath: "docs/two.md",
    affectedPaths: ["docs/two.md"],
    affectedNodeIds: ["REQ-2"],
  },
  {
    issueKey: "issue-3",
    check: "ontology",
    code: "missing_required_field",
    message: "Missing required field: status",
    primaryPath: "docs/one.md",
    affectedPaths: ["docs/one.md"],
    field: "status",
  },
];

function snapshotEnvelope(): ValidateEnvelope {
  return {
    status: "ok",
    health: "current_issues",
    generation: 7,
    publishedGeneration: 7,
    computedAt: "2026-09-11T15:00:00Z",
    snapshot: {
      vaultIdentity: "vault",
      generation: 7,
      scope: "default",
      selectedChecks: ["broken_links", "ontology"],
      startedAt: 1,
      finishedAt: 2,
      durationMs: 8,
      completion: "complete",
      issueCount: 2,
      errorCount: 0,
      affectedFileCount: 2,
      affectedNoteCount: 2,
      repairActionCount: 1,
      repairPlanFingerprint: "plan-7",
      issueCodes: ["broken_link", "missing_required_field"],
      checks: [
        { check: "broken_links", outcome: "completed", issueCount: 1, durationMs: 4 },
        { check: "ontology", outcome: "completed", issueCount: 1, durationMs: 4 },
      ],
      actions: [
        {
          id: "repair-1",
          check: "broken_links",
          issueCode: "broken_link",
          kind: "rewrite_broken_link_group",
          safety: "needs_confirmation",
          title: "Retarget the broken link",
          instanceCount: 1,
          affectedPaths: ["docs/one.md"],
          issueKeys: ["issue-1"],
        },
      ],
    },
  };
}

describe("NotesIssuesHome", () => {
  it("keeps the selected diagnostic visible until a new generation proves it disappeared", async () => {
    const replacement = deferredReply<unknown>();
    http.on("GET", "/api/v1/validation/diagnostics", (request) => {
      if (request.query.get("generation") === "8") return replacement.promise;

      return jsonReply({
        generation: 7,
        filterIdentity: "all",
        sort: "diagnostic_order",
        returned: 1,
        total: 1,
        diagnostics: [issueDiagnostics[1]],
      });
    });

    const view = renderWithQueryClient(
      <NotesIssuesHome
        validationEnvelope={snapshotEnvelope()}
        selectedIssueKey="issue-2"
        onOpenNote={vi.fn()}
      />,
    );

    expect(await screen.findByText("Target does not exist: Missing B")).toBeVisible();

    const nextEnvelope = snapshotEnvelope();
    nextEnvelope.generation = 8;
    nextEnvelope.publishedGeneration = 8;
    nextEnvelope.snapshot = { ...nextEnvelope.snapshot!, generation: 8 };
    view.rerender(
      <NotesIssuesHome
        validationEnvelope={nextEnvelope}
        selectedIssueKey="issue-2"
        onOpenNote={vi.fn()}
      />,
    );
    expect(screen.getByText("Target does not exist: Missing B")).toBeVisible();

    replacement.resolve({
      generation: 8,
      filterIdentity: "all",
      sort: "diagnostic_order",
      returned: 1,
      total: 1,
      diagnostics: [issueDiagnostics[2]],
    });
    expect(await screen.findByText("Missing required field: status")).toBeVisible();
    expect(screen.queryByText("Target does not exist: Missing B")).toBeNull();
  });

  it("uses a compact neutral clean state without empty controls", () => {
    renderWithQueryClient(
      <NotesIssuesHome validationEnvelope={cleanEnvelope()} onOpenNote={vi.fn()} />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("No issues found");
    expect(screen.getByRole("heading", { name: "Problems" })).toBeVisible();
    expect(screen.queryByLabelText("Problem filters")).toBeNull();
    expect(screen.queryByText(/needs attention/i)).toBeNull();
    expect(screen.queryByText(/vault is clean/i)).toBeNull();
  });

  it("keeps never checked, running, failed, and incomplete states distinct", () => {
    const { rerender } = renderWithQueryClient(
      <NotesIssuesHome
        validationEnvelope={{
          status: "never_ran",
          health: "never_checked",
          generation: 0,
          publishedGeneration: 0,
        }}
        onOpenNote={vi.fn()}
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Not checked yet");

    rerender(
      <NotesIssuesHome
        validationEnvelope={{
          status: "running",
          health: "running",
          generation: 1,
          publishedGeneration: 0,
        }}
        loading
        onOpenNote={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Checking vault");

    rerender(
      <NotesIssuesHome
        validationEnvelope={{
          status: "error",
          health: "failed",
          generation: 1,
          publishedGeneration: 0,
          error: "Read failed",
        }}
        onOpenNote={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Validation unavailable");
    expect(screen.getByText("Read failed")).toBeVisible();

    const clean = cleanEnvelope();

    const incomplete: ValidateEnvelope = {
      ...clean,
      health: "incomplete",
      snapshot: {
        ...clean.snapshot!,
        completion: "incomplete",
        errorCount: 1,
        checks: [{ check: "broken_links", outcome: "failed", issueCount: 0, durationMs: 4 }],
      },
    };

    rerender(<NotesIssuesHome validationEnvelope={incomplete} onOpenNote={vi.fn()} />);
    expect(screen.getByRole("status")).toHaveTextContent("Validation incomplete");
  });

  it("retains and labels a published result while a refresh runs or fails", () => {
    const envelope = snapshotEnvelope();

    const { rerender } = renderWithQueryClient(
      <NotesIssuesHome
        validationEnvelope={{ ...envelope, status: "running", health: "running" }}
        loading
        onOpenNote={vi.fn()}
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Checking vault");
    expect(screen.getByText("Showing the last published result while checks run.")).toBeVisible();
    expect(screen.getByRole("heading", { name: "2 issues in 2 files" })).toBeVisible();

    rerender(
      <NotesIssuesHome
        validationEnvelope={{
          ...envelope,
          status: "error",
          health: "failed",
          error: "Network unavailable",
        }}
        onOpenNote={vi.fn()}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("Refresh failed");
    expect(screen.getByText("Network unavailable")).toBeVisible();
    expect(screen.getByRole("heading", { name: "2 issues in 2 files" })).toBeVisible();
  });

  it("lists kinds from the grouped read, filters, switches to files, and opens detail", async () => {
    const linkGroup = {
      check: "broken_links",
      code: "broken_link",
      issueCount: 2,
      affectedFileCount: 2,
      applicableRepairCount: 0,
    };

    const fieldGroup = {
      check: "ontology",
      code: "missing_required_field",
      issueCount: 1,
      affectedFileCount: 1,
      applicableRepairCount: 0,
    };

    http.on("POST", "/api/v1/validation/groups", (request) => {
      // SAFETY: the component posts a typed issue-group request.
      const body = JSON.parse(request.body || "{}") as { filter?: { text?: string } };

      return jsonReply({
        generation: 7,
        groups: body.filter?.text === "status" ? [fieldGroup] : [linkGroup, fieldGroup],
      });
    });
    http.on("GET", "/api/v1/validation/diagnostics", ({ query }) => {
      const text = query.get("text")?.toLowerCase();
      const diagnostics = text === "status" ? [issueDiagnostics[2]] : issueDiagnostics;

      return jsonReply({
        generation: 7,
        filterIdentity: text ? `text:${text}` : "all",
        sort: "diagnostic_order",
        returned: diagnostics.length,
        total: diagnostics.length,
        diagnostics,
      });
    });
    const open = vi.fn();
    const envelope = snapshotEnvelope();
    renderWithQueryClient(
      <NotesIssuesHome
        validationEnvelope={{
          ...envelope,
          snapshot: { ...envelope.snapshot!, issueCount: 3, repairActionCount: 0, actions: [] },
        }}
        onOpenNote={open}
      />,
    );

    const kinds = screen.getByRole("group", { name: "Issue kinds" });
    expect(
      await within(kinds).findByRole("button", { name: /^Broken link, .*, 2 issues$/ }),
    ).toBeVisible();
    expect(
      within(kinds).getByRole("button", { name: /^Missing required field, .*, 1 issue$/ }),
    ).toBeVisible();
    fireEvent.click(
      within(screen.getByRole("group", { name: "Issue list" })).getByRole("button", {
        name: /docs\/two\.md/,
      }),
    );
    fireEvent.click(
      within(screen.getByRole("article", { name: "Issue detail" })).getByRole("button", {
        name: /Open at issue/i,
      }),
    );
    expect(open).toHaveBeenCalledWith("docs/two.md#issue:node=REQ-2");

    fireEvent.change(screen.getByPlaceholderText("Search issues…"), {
      target: { value: "status" },
    });
    expect(
      await within(kinds).findByRole("button", { name: /^Missing required field, .*, 1 issue$/ }),
    ).toBeVisible();
    await waitFor(() =>
      expect(within(kinds).queryByRole("button", { name: /^Broken link/ })).toBeNull(),
    );
    expect(
      http
        .requests("POST", "/api/v1/validation/groups")
        .some((request) => JSON.parse(request.body || "{}").filter?.text === "status"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));

    fireEvent.click(screen.getByRole("button", { name: "By file" }));

    const firstFile = (await screen.findByText("one.md", { selector: "summary span" })).closest(
      "details",
    );

    expect(within(firstFile!).getAllByRole("button")).toHaveLength(2);
    // A file with a single issue collapses to one row instead of a group.
    expect(screen.getByRole("button", { name: /docs\/two\.md\s*Broken link/ })).toBeVisible();
  });

  it("keeps the issue list to one Tab stop that follows arrow keys", async () => {
    http.on("GET", "/api/v1/validation/diagnostics", () =>
      jsonReply({
        generation: 7,
        filterIdentity: "all",
        sort: "diagnostic_order",
        returned: issueDiagnostics.length,
        total: issueDiagnostics.length,
        diagnostics: issueDiagnostics,
      }),
    );
    const envelope = snapshotEnvelope();
    renderWithQueryClient(
      <NotesIssuesHome
        validationEnvelope={{
          ...envelope,
          snapshot: { ...envelope.snapshot!, issueCount: 3, repairActionCount: 0, actions: [] },
        }}
        onOpenNote={vi.fn()}
      />,
    );

    const list = await screen.findByRole("group", { name: "Issue list" });
    const rows = () => [...list.querySelectorAll<HTMLButtonElement>("[data-issue-key]")];
    await waitFor(() => expect(rows()).toHaveLength(3));
    const tabbable = () => rows().filter((row) => row.tabIndex === 0);

    expect(tabbable()).toEqual([rows()[0]]);
    rows()[0].focus();
    fireEvent.keyDown(rows()[0], { key: "ArrowDown" });
    await waitFor(() => expect(rows()[1]).toHaveFocus());
    expect(tabbable()).toEqual([rows()[1]]);
  });

  it("loads generation-bound pages and exposes check details", async () => {
    http.on("GET", "/api/v1/validation/diagnostics", ({ query }) => {
      expect(query.get("generation")).toBe("7");

      return jsonReply({
        generation: 7,
        filterIdentity: "all",
        sort: "diagnostic_order",
        returned: 2,
        total: 2,
        diagnostics: [
          {
            issueKey: "issue-1",
            check: "broken_links",
            code: "broken_link",
            message: "Target missing",
            primaryPath: "docs/one.md",
            affectedPaths: ["docs/one.md"],
            actionIds: ["repair-1"],
          },
          {
            issueKey: "issue-2",
            check: "ontology",
            code: "missing_required_field",
            primaryPath: "docs/two.md",
            affectedPaths: ["docs/two.md"],
            field: "status",
          },
        ],
      });
    });

    renderWithQueryClient(
      <NotesIssuesHome validationEnvelope={snapshotEnvelope()} onOpenNote={vi.fn()} />,
    );
    expect(
      await screen.findByRole("button", { name: /docs\/one\.md\s*Broken link/ }),
    ).toBeVisible();
    fireEvent.click(screen.getByText("Check details"));
    expect(screen.getByText("default")).toBeVisible();
    expect(screen.getAllByText("completed")).toHaveLength(2);
  });

  it("shows repair membership once and preserves selection after staging fails", async () => {
    http.on("GET", "/api/v1/validation/diagnostics", () =>
      jsonReply({
        generation: 7,
        filterIdentity: "all",
        sort: "diagnostic_order",
        returned: 1,
        total: 1,
        diagnostics: [
          {
            issueKey: "issue-1",
            check: "broken_links",
            code: "broken_link",
            primaryPath: "docs/one.md",
            affectedPaths: ["docs/one.md"],
            actionIds: ["repair-1"],
          },
        ],
      }),
    );
    http.on("POST", "/api/v1/validation/repair-reviews", () =>
      jsonReply({ error: "Review expired", code: "REPAIR_REVIEW_EXPIRED" }, 409),
    );
    renderWithQueryClient(
      <NotesIssuesHome validationEnvelope={snapshotEnvelope()} onOpenNote={vi.fn()} />,
    );

    const repair = await screen.findByRole("checkbox");
    fireEvent.click(repair);
    fireEvent.click(screen.getByRole("button", { name: "Stage 1 fix" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Review expired");
    expect(repair).toBeChecked();
  });

  it("presents agent-required remediation as guidance instead of a stageable fix", async () => {
    http.json("GET", "/api/v1/validation/diagnostics", {
      generation: 7,
      filterIdentity: "all",
      sort: "diagnostic_order",
      returned: 1,
      total: 1,
      diagnostics: [
        {
          ...issueDiagnostics[0],
          actionIds: ["repair-1"],
        },
      ],
    });
    const envelope = snapshotEnvelope();
    renderWithQueryClient(
      <NotesIssuesHome
        validationEnvelope={{
          ...envelope,
          snapshot: {
            ...envelope.snapshot!,
            actions: envelope.snapshot!.actions?.map((action) => ({
              ...action,
              safety: "agent_required" as const,
            })),
          },
        }}
        onOpenNote={vi.fn()}
      />,
    );

    expect(await screen.findByRole("note")).toHaveTextContent("Agent task");
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.queryByRole("button", { name: /Stage/ })).toBeNull();
  });

  it("reviews the canonical connected transaction and requires candidate confirmation before save", async () => {
    http.json("GET", "/api/v1/validation/diagnostics", {
      generation: 7,
      filterIdentity: "all",
      sort: "diagnostic_order",
      returned: 1,
      total: 1,
      diagnostics: [
        {
          issueKey: "issue-1",
          check: "broken_links",
          code: "broken_link",
          primaryPath: "docs/one.md",
          affectedPaths: ["docs/one.md"],
          actionIds: ["repair-1"],
        },
      ],
    });

    const review = {
      id: "review-1",
      vaultIdentity: "vault",
      generation: 7,
      planFingerprint: "plan-7",
      selectionFingerprint: "selection-1",
      actionIds: ["repair-1", "repair-connected"],
      transactionIds: ["transaction-1"],
      affectedPaths: ["docs/one.md", "docs/target.md"],
      preview: [{ path: "docs/one.md", kind: "write", diff: "@@ -1 +1 @@\n-old\n+new\n" }],
      requiredConfirmations: [
        {
          actionId: "repair-1",
          question: "Use this target?",
          candidatePath: "docs/target.md",
          affectedPaths: ["docs/one.md"],
        },
      ],
      state: "pending",
      createdAt: "2026-09-11T12:00:00Z",
      lastAccessedAt: "2026-09-11T12:00:00Z",
      expiresAt: "2026-09-11T12:15:00Z",
    } as const;

    http.on("POST", "/api/v1/validation/repair-reviews", (request) => {
      const body = JSON.parse(request.body || "{}");

      return body.actionIds.includes("repair-connected")
        ? jsonReply(review, 201)
        : jsonReply(
            {
              error: "Connected transaction",
              code: "REPAIR_TRANSACTION_INCOMPLETE",
              details: { requiredActionIds: ["repair-1", "repair-connected"] },
            },
            409,
          );
    });
    http.json("POST", "/api/v1/validation/repair-reviews/review-1/apply", {
      review: { ...review, state: "applied" },
      result: { ok: true, issueCount: 0, results: [] },
    });
    renderWithQueryClient(
      <NotesIssuesHome validationEnvelope={snapshotEnvelope()} onOpenNote={vi.fn()} />,
    );

    fireEvent.click(await screen.findByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Stage 1 fix" }));
    expect(await screen.findByRole("note")).toHaveTextContent("connected transaction");
    expect(screen.queryByRole("button", { name: "Save fixes" })).toBeNull();
    expect(http.count("POST", "/api/v1/validation/repair-reviews")).toBe(1);
    fireEvent.click(screen.getByRole("button", { name: "Include connected fixes and review" }));
    const save = await screen.findByRole("button", { name: "Save fixes" });
    expect(http.count("POST", "/api/v1/validation/repair-reviews")).toBe(2);
    expect(save).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: /Use this target/ }));
    fireEvent.click(save);
    expect(await screen.findByText(/Saved 2 of 2 planned file changes/)).toBeVisible();
  });

  it("does not present filtered-empty as clean", async () => {
    http.on("GET", "/api/v1/validation/diagnostics", () =>
      jsonReply({
        generation: 7,
        filterIdentity: "none",
        sort: "diagnostic_order",
        returned: 0,
        total: 0,
        diagnostics: [],
      }),
    );
    renderWithQueryClient(
      <NotesIssuesHome validationEnvelope={snapshotEnvelope()} onOpenNote={vi.fn()} />,
    );
    fireEvent.change(screen.getByPlaceholderText("Search issues…"), {
      target: { value: "no match" },
    });
    await waitFor(() => expect(screen.getByText("No issues match these filters.")).toBeVisible());
    expect(screen.getByText("2 issues exist in the current validation result.")).toBeVisible();
    expect(screen.queryByText("No issues found")).toBeNull();
    expect(screen.getAllByRole("button", { name: "Clear filters" })).toHaveLength(1);
  });
});

describe("validation refresh", () => {
  it("shows a watcher-queued refresh while the prior result is still published", () => {
    const current = cleanEnvelope();

    const view = renderWithQueryClient(
      <NotesIssuesHome
        validationEnvelope={{ ...current, refreshPending: true }}
        onOpenNote={vi.fn()}
      />,
    );

    expect(screen.getByRole("status")).toHaveTextContent("No issues found");
    expect(screen.getByRole("status")).toHaveTextContent("Checking…");
    expect(screen.getByRole("button", { name: "Refreshing validation…" })).toBeDisabled();
    expect(http.count("POST", "/api/v2/validate/refresh")).toBe(0);

    view.rerender(<NotesIssuesHome validationEnvelope={current} onOpenNote={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Refresh validation" })).toBeEnabled();
  });

  it("queues a refresh, shows progress, and permits another refresh after publication", async () => {
    let current = cleanEnvelope();
    http.on("GET", "/api/v2/validate", () => jsonReply(current));
    const pending = deferredReply<{ accepted: boolean }>();
    http.on("POST", "/api/v2/validate/refresh", () => pending.promise);

    const view = renderWithQueryClient(
      <NotesIssuesHome validationEnvelope={current} onOpenNote={vi.fn()} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Refresh validation" }));
    await waitFor(() => expect(http.count("POST", "/api/v2/validate/refresh")).toBe(1));
    expect(screen.getByRole("button", { name: "Refreshing validation…" })).toBeDisabled();
    current = { ...current, generation: 2, publishedGeneration: 2 };
    pending.resolve({ accepted: true }, 202);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Refresh validation" })).toBeEnabled(),
    );
    current = { ...current, generation: 3, health: "stale" };
    await view.queryClient.invalidateQueries({ queryKey: queryKeys.validationAll() });
    view.rerender(<NotesIssuesHome validationEnvelope={current} onOpenNote={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Refresh validation" })).toBeEnabled();
    view.unmount();
  });

  it("allows retry when invalidation cancels a pending refresh without a replacement", async () => {
    let current = cleanEnvelope();
    http.on("GET", "/api/v2/validate", () => jsonReply(current));
    const pending = deferredReply<{ accepted: boolean }>();
    http.on("POST", "/api/v2/validate/refresh", () => pending.promise);
    renderWithQueryClient(<NotesIssuesHome validationEnvelope={current} onOpenNote={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Refresh validation" }));
    await waitFor(() => expect(http.count("POST", "/api/v2/validate/refresh")).toBe(1));
    current = { ...current, generation: 3, health: "stale" };
    pending.resolve({ accepted: true }, 202);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Refresh validation" })).toBeEnabled(),
    );
  });

  it.each(["published", "invalidated"] as const)(
    "keeps a queued manual refresh busy when an unrelated generation is %s",
    async (state) => {
      let current = cleanEnvelope();
      http.on("GET", "/api/v2/validate", () => jsonReply(current));
      const pending = deferredReply<{ accepted: boolean }>();
      http.on("POST", "/api/v2/validate/refresh", () => pending.promise);

      const view = renderWithQueryClient(
        <NotesIssuesHome validationEnvelope={current} onOpenNote={vi.fn()} />,
      );

      fireEvent.click(screen.getByRole("button", { name: "Refresh validation" }));
      await waitFor(() => expect(http.count("POST", "/api/v2/validate/refresh")).toBe(1));
      current = {
        ...current,
        generation: 2,
        publishedGeneration: state === "published" ? 2 : 1,
        health: state === "published" ? "current_clean" : "stale",
        refreshPending: true,
      };
      pending.resolve({ accepted: true }, 202);
      await waitFor(() =>
        expect(
          view.queryClient.getQueryData<ValidateEnvelope>(queryKeys.validation())?.generation,
        ).toBe(2),
      );
      expect(screen.getByRole("button", { name: "Refreshing validation…" })).toBeDisabled();

      current = { ...current, generation: 3, publishedGeneration: 3, refreshPending: false };
      await view.queryClient.invalidateQueries({ queryKey: queryKeys.validationAll() });
      await waitFor(() =>
        expect(screen.getByRole("button", { name: "Refresh validation" })).toBeEnabled(),
      );
    },
  );

  it("shows request failures and leaves refresh available to retry", async () => {
    http.json("GET", "/api/v2/validate", cleanEnvelope());
    http.on("POST", "/api/v2/validate/refresh", () =>
      jsonReply({ error: "Runtime unavailable" }, 503),
    );
    renderWithQueryClient(
      <NotesIssuesHome validationEnvelope={cleanEnvelope()} onOpenNote={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Refresh validation" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Runtime unavailable"));
    expect(screen.getByRole("button", { name: "Refresh validation" })).toBeEnabled();
  });
});
