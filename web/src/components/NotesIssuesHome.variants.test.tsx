import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

import type {
  ValidateEnvelope,
  ValidationActionSnapshot,
  ValidationDiagnostic,
  ValidationIssueGroup,
} from "../api/types";
import { emptyValidationSummariesReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { NotesIssuesHome } from "./NotesIssuesHome";

const http = withFakeFetch();

const coaching = { key: "CoachingSession,Meeting", label: "CoachingSession, Meeting" };

function group(
  code: string,
  issueCount: number,
  variant?: ValidationIssueGroup["variant"],
): ValidationIssueGroup {
  return {
    check: code === "broken_link" ? "broken_links" : "ontology",
    code,
    variant,
    issueCount,
    affectedFileCount: issueCount,
    applicableRepairCount: 0,
  };
}

// Server order: code by first diagnostic, then variants by count.
const groups = [
  group("type_ambiguous", 3, coaching),
  group("type_ambiguous", 1, { key: "Meeting,Note", label: "Meeting, Note" }),
  group("unknown_declared_type", 2, { key: "Mtg", label: "Mtg" }),
  group("broken_link", 4),
];

function ambiguous(path: string): ValidationDiagnostic {
  return {
    issueKey: `ambiguous:${path}`,
    check: "ontology",
    code: "type_ambiguous",
    message: `${path} matches multiple ontology types`,
    primaryPath: path,
    evidence: { candidateTypes: ["CoachingSession", "Meeting"] },
    variant: coaching,
  };
}

function envelope(actions: ValidationActionSnapshot[] = [], fingerprint = "plan-1") {
  return {
    status: "ok",
    health: "current_issues",
    generation: 1,
    publishedGeneration: 1,
    snapshot: {
      vaultIdentity: "vault",
      generation: 1,
      scope: "default",
      selectedChecks: ["ontology", "broken_links"],
      startedAt: 1,
      finishedAt: 2,
      durationMs: 1,
      completion: "complete",
      issueCount: 10,
      errorCount: 0,
      affectedFileCount: 10,
      affectedNoteCount: 10,
      repairActionCount: actions.length,
      repairPlanFingerprint: fingerprint || undefined,
      checks: [],
      issueCodes: ["type_ambiguous", "unknown_declared_type", "broken_link"],
      actions,
    },
  } satisfies ValidateEnvelope;
}

function page(diagnostics: ValidationDiagnostic[], nextCursor?: string) {
  return jsonReply({ generation: 1, total: diagnostics.length, diagnostics, nextCursor });
}

const diagnosticRequests = () => http.requests("GET", "/api/v1/validation/diagnostics");

beforeEach(() => {
  sessionStorage.clear();
  http.on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);
  http.json("POST", "/api/v1/validation/groups", { generation: 1, groups });
});

it("lists kinds with nested variants from one grouped read and collapses a single-variant kind", async () => {
  http.on("GET", "/api/v1/validation/diagnostics", () => page([ambiguous("a.md")]));
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope()} onOpenNote={vi.fn()} />);

  const kinds = screen.getByRole("group", { name: "Issue kinds" });

  const rows = () =>
    [...kinds.querySelectorAll<HTMLElement>("[data-kind-key]")].map((row) => [
      row.querySelector(".problems-kinds__label")?.textContent,
      row.querySelector(".problems-kind__count")?.textContent,
      row.querySelector(".problems-kinds__meta")?.textContent ?? null,
      row.classList.contains("is-variant"),
    ]);

  await waitFor(() => expect(rows()).toHaveLength(6));
  expect(rows()).toEqual([
    ["All issues", "10", null, false],
    ["Ambiguous type", "4", "Ontology", false],
    ["CoachingSession, Meeting", "3", null, true],
    ["Meeting, Note", "1", null, true],
    ["Unknown declared type", "2", "Mtg", false],
    ["Broken link", "4", "Broken links", false],
  ]);
  expect(http.count("POST", "/api/v1/validation/groups")).toBe(1);
});

it("reads the findings of a selected variant or collapsed kind by code and variant", async () => {
  http.on("GET", "/api/v1/validation/diagnostics", () => page([ambiguous("a.md")]));
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope()} onOpenNote={vi.fn()} />);

  const kinds = screen.getByRole("group", { name: "Issue kinds" });
  fireEvent.click(
    await within(kinds).findByRole("button", {
      name: /^Ambiguous type: CoachingSession, Meeting,/,
    }),
  );

  await waitFor(() =>
    expect(
      diagnosticRequests().some(
        ({ query }) =>
          query.get("code") === "type_ambiguous" && query.get("variant") === coaching.key,
      ),
    ).toBe(true),
  );

  const findings = screen.getByText("CoachingSession, Meeting", {
    selector: ".problems-findings__variant",
  });

  expect(findings.closest("header")).toHaveTextContent("matches the selectors of more than one");

  // Arrow keys move the kind selection without a pointer.
  const variantRow = within(kinds).getByRole("button", {
    name: /^Ambiguous type: CoachingSession, Meeting,/,
  });

  variantRow.focus();
  fireEvent.keyDown(variantRow, { key: "ArrowDown" });
  fireEvent.keyDown(
    within(kinds).getByRole("button", { name: /^Ambiguous type: Meeting, Note,/ }),
    {
      key: "ArrowDown",
    },
  );
  await waitFor(() =>
    expect(within(kinds).getByRole("button", { name: /^Unknown declared type/ })).toHaveFocus(),
  );
  await waitFor(() =>
    expect(
      diagnosticRequests().some(
        ({ query }) =>
          query.get("code") === "unknown_declared_type" && query.get("variant") === "Mtg",
      ),
    ).toBe(true),
  );
});

it("falls back to every issue when filters remove the selected variant", async () => {
  http.on("POST", "/api/v1/validation/groups", (request) => {
    // SAFETY: the component posts a typed issue-group request.
    const body = JSON.parse(request.body || "{}") as { filter?: { code?: string } };

    return jsonReply({
      generation: 1,
      groups: groups.filter((item) => !body.filter?.code || item.code === body.filter.code),
    });
  });
  http.on("GET", "/api/v1/validation/diagnostics", () => page([ambiguous("a.md")]));
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope()} onOpenNote={vi.fn()} />);

  const kinds = screen.getByRole("group", { name: "Issue kinds" });
  fireEvent.click(
    await within(kinds).findByRole("button", {
      name: /^Ambiguous type: CoachingSession, Meeting,/,
    }),
  );
  await screen.findByText("CoachingSession, Meeting", { selector: ".problems-findings__variant" });

  fireEvent.change(screen.getByRole("combobox", { name: "Issue" }), {
    target: { value: "broken_link" },
  });

  expect(
    await within(kinds).findByRole("button", { name: /^Broken link, .*, 4 issues$/ }),
  ).toBeVisible();
  await waitFor(() =>
    expect(within(kinds).queryAllByRole("button", { name: /^Ambiguous type/ })).toHaveLength(0),
  );
  expect(
    await screen.findByText("All issues", { selector: ".problems-findings__title *" }),
  ).toBeVisible();
  await waitFor(() =>
    expect(
      diagnosticRequests().some(
        ({ query }) => query.get("code") === "broken_link" && !query.has("variant"),
      ),
    ).toBe(true),
  );
});

it("drops the selected case as soon as the check or kind filter changes", async () => {
  http.on("GET", "/api/v1/validation/diagnostics", () => page([ambiguous("a.md")]));
  renderWithQueryClient(
    <NotesIssuesHome validationEnvelope={envelope()} onOpenNote={vi.fn()} onStageOps={vi.fn()} />,
  );

  const kinds = screen.getByRole("group", { name: "Issue kinds" });

  fireEvent.click(
    await within(kinds).findByRole("button", {
      name: /^Ambiguous type: CoachingSession, Meeting,/,
    }),
  );
  await screen.findByRole("button", { name: "Set type: Meeting on 3 notes" });

  fireEvent.change(screen.getByRole("combobox", { name: "Issue" }), {
    target: { value: "broken_link" },
  });

  // The previous counts may stay on screen, but the old case is no longer actionable.
  expect(screen.queryByRole("button", { name: /^Set type:/ })).toBeNull();
  await waitFor(() => expect(diagnosticRequests().at(-1)?.query.get("code")).toBe("broken_link"));
  expect(diagnosticRequests().at(-1)?.query.has("variant")).toBe(false);
});

it("stages one type edit per note across every page in a single stageOps call", async () => {
  http.on("GET", "/api/v1/validation/diagnostics", ({ query }) => {
    if (query.get("limit") !== "200") return page([ambiguous("a.md")]);

    // Staging pages through the whole variant; the second page repeats a note.
    return query.get("cursor")
      ? page([ambiguous("c.md"), ambiguous("a.md")])
      : page([ambiguous("a.md"), ambiguous("b.md")], "next");
  });
  const stageOps = vi.fn().mockResolvedValue(undefined);
  const review = vi.fn();
  renderWithQueryClient(
    <NotesIssuesHome
      validationEnvelope={envelope()}
      onOpenNote={vi.fn()}
      onStageOps={stageOps}
      onReviewChanges={review}
    />,
  );

  const kinds = screen.getByRole("group", { name: "Issue kinds" });
  fireEvent.click(
    await within(kinds).findByRole("button", {
      name: /^Ambiguous type: CoachingSession, Meeting,/,
    }),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Set type: Meeting on 3 notes" }));

  await waitFor(() => expect(stageOps).toHaveBeenCalledTimes(1));
  expect(stageOps.mock.calls[0][0]).toEqual(
    ["a.md", "b.md", "c.md"].map((path) => ({
      id: `frontmatter:${encodeURIComponent(path)}:type`,
      kind: "setFrontmatter",
      path,
      property: "type",
      value: "Meeting",
    })),
  );
  expect(
    diagnosticRequests()
      .filter(({ query }) => query.get("limit") === "200")
      .map(({ query }) => [query.get("code"), query.get("variant"), query.get("cursor")]),
  ).toEqual([
    ["type_ambiguous", coaching.key, null],
    ["type_ambiguous", coaching.key, "next"],
  ]);

  expect(await screen.findByText(/on 3 notes\./)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Review changes" }));
  expect(review).toHaveBeenCalledTimes(1);
});

it("reports a failed type choice without staging part of the case", async () => {
  http.on("GET", "/api/v1/validation/diagnostics", ({ query }) => {
    if (query.get("limit") !== "200") return page([ambiguous("a.md")]);

    return query.get("cursor")
      ? jsonReply({ error: "Generation expired", code: "GENERATION_EXPIRED" }, 410)
      : page([ambiguous("a.md")], "next");
  });
  const stageOps = vi.fn().mockResolvedValue(undefined);
  renderWithQueryClient(
    <NotesIssuesHome validationEnvelope={envelope()} onOpenNote={vi.fn()} onStageOps={stageOps} />,
  );

  const kinds = screen.getByRole("group", { name: "Issue kinds" });
  fireEvent.click(
    await within(kinds).findByRole("button", {
      name: /^Ambiguous type: CoachingSession, Meeting,/,
    }),
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "Set type: CoachingSession on 3 notes" }),
  );

  expect(await screen.findByRole("alert")).toHaveTextContent("Could not stage the type change.");
  expect(stageOps).not.toHaveBeenCalled();
  expect(screen.queryByText(/^Staged/)).toBeNull();
});

it("stages exactly the safe repair actions through one repair review", async () => {
  const action = (id: string, safety: ValidationActionSnapshot["safety"]) => ({
    id,
    check: "broken_links",
    kind: "rewrite_broken_link_group",
    safety,
    title: id,
  });

  const actions = [
    action("safe-1", "safe"),
    action("confirm-1", "needs_confirmation"),
    action("safe-2", "safe"),
    action("agent-1", "agent_required"),
  ];

  http.on("GET", "/api/v1/validation/diagnostics", () => page([ambiguous("a.md")]));
  http.on("POST", "/api/v1/validation/repair-reviews", () =>
    jsonReply(
      {
        error: "Connected transaction",
        code: "REPAIR_TRANSACTION_INCOMPLETE",
        details: { requiredActionIds: ["confirm-1"] },
      },
      409,
    ),
  );

  // Without a repair plan fingerprint the browser cannot review repairs.
  const view = renderWithQueryClient(
    <NotesIssuesHome validationEnvelope={envelope(actions, "")} onOpenNote={vi.fn()} />,
  );

  expect(screen.queryByRole("button", { name: /safe fix/ })).toBeNull();
  view.rerender(<NotesIssuesHome validationEnvelope={envelope(actions)} onOpenNote={vi.fn()} />);

  fireEvent.click(screen.getByRole("button", { name: "Stage 2 safe fixes" }));
  expect(await screen.findByRole("note")).toHaveTextContent("connected transaction with 3 fixes");
  fireEvent.click(screen.getByRole("button", { name: "Include connected fixes and review" }));
  await waitFor(() => expect(http.count("POST", "/api/v1/validation/repair-reviews")).toBe(2));
  expect(
    http
      .requests("POST", "/api/v1/validation/repair-reviews")
      .map((request) => JSON.parse(request.body || "{}")),
  ).toEqual([
    { generation: 1, planFingerprint: "plan-1", actionIds: ["safe-1", "safe-2"] },
    { generation: 1, planFingerprint: "plan-1", actionIds: ["safe-1", "safe-2", "confirm-1"] },
  ]);
});
