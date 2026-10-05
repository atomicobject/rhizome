import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { ValidateEnvelope, ValidationDiagnostic } from "../api/types";
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

const diagnostic: ValidationDiagnostic = {
  issueKey: "issue",
  check: "broken_links",
  code: "broken_link",
  message: "Original finding",
  primaryPath: "a.md",
  actionIds: ["repair"],
};

function envelope(generation: number): ValidateEnvelope {
  return {
    status: "ok",
    health: "current_issues",
    generation,
    publishedGeneration: generation,
    snapshot: {
      vaultIdentity: "vault",
      generation,
      scope: "default",
      selectedChecks: ["broken_links"],
      startedAt: 1,
      finishedAt: 2,
      durationMs: 1,
      completion: "complete",
      issueCount: 201,
      errorCount: 0,
      affectedFileCount: 1,
      affectedNoteCount: 1,
      repairActionCount: 1,
      repairPlanFingerprint: `plan-${generation}`,
      checks: [],
      issueCodes: ["broken_link", "missing_required_field"],
      actions: [
        {
          id: "repair",
          check: "broken_links",
          kind: "rewrite_broken_link_group",
          safety: "safe",
          title: `Generation ${generation} repair`,
          issueKeys: ["issue"],
        },
      ],
    },
  };
}

beforeEach(() => {
  sessionStorage.clear();
  http.on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);
  http.on("POST", "/api/v1/validation/groups", emptyValidationGroupsReply);
});

it("retained details cannot select current-generation repairs and old selections are cleared", async () => {
  const next = deferredReply();
  http.on("GET", "/api/v1/validation/diagnostics", (request) =>
    request.query.get("generation") === "2"
      ? next.promise
      : jsonReply({ generation: 1, total: 201, diagnostics: [diagnostic] }),
  );

  const view = renderWithQueryClient(
    <NotesIssuesHome validationEnvelope={envelope(1)} onOpenNote={vi.fn()} />,
  );

  fireEvent.click(await screen.findByRole("checkbox"));
  expect(screen.getByRole("button", { name: "Stage 1 fix" })).toBeVisible();
  view.rerender(<NotesIssuesHome validationEnvelope={envelope(2)} onOpenNote={vi.fn()} />);
  expect(screen.getByText("Original finding")).toBeVisible();
  expect(screen.queryByText("Generation 2 repair")).toBeNull();
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect(screen.queryByRole("button", { name: "Stage 1 fix" })).toBeNull();
  next.resolve({
    generation: 2,
    total: 201,
    diagnostics: [{ ...diagnostic, message: "Current finding" }],
  });
  expect(await screen.findByText("Current finding")).toBeVisible();
  expect(screen.getByText("Generation 2 repair")).toBeVisible();
  expect(screen.getByRole("checkbox")).not.toBeChecked();
});

it("offers later-page issue codes before loading those pages and retains options when filtered", async () => {
  http.json("GET", "/api/v1/validation/diagnostics", {
    generation: 1,
    total: 201,
    nextCursor: "later",
    diagnostics: [diagnostic],
  });
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope(1)} onOpenNote={vi.fn()} />);
  await screen.findByText("Original finding");
  const select = screen.getByRole("combobox", { name: "Issue" });
  const later = within(select).getByRole("option", { name: "Missing required field" });
  expect(later).toHaveValue("missing_required_field");
  fireEvent.change(select, { target: { value: "missing_required_field" } });
  await waitFor(() =>
    expect(
      http
        .requests("GET", "/api/v1/validation/diagnostics")
        .some((request) => request.query.get("code") === "missing_required_field"),
    ).toBe(true),
  );
  expect(within(select).getAllByRole("option")).toHaveLength(3);
  expect(
    http
      .requests("GET", "/api/v1/validation/diagnostics")
      .every((request) => !request.query.has("cursor")),
  ).toBe(true);
});

it("Next on the last loaded issue loads the next page and selects its first issue", async () => {
  const second = { ...diagnostic, issueKey: "second", message: "Second finding" };

  http.on("GET", "/api/v1/validation/diagnostics", (request) =>
    jsonReply(
      request.query.get("cursor")
        ? { generation: 1, total: 2, diagnostics: [second] }
        : { generation: 1, total: 2, nextCursor: "later", diagnostics: [diagnostic] },
    ),
  );
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope(1)} onOpenNote={vi.fn()} />);

  await screen.findByText("Original finding");
  expect(screen.getByText("1 / 2")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Next issue" }));

  expect(await screen.findByText("Second finding")).toBeVisible();
  expect(screen.getByText("2 / 2")).toBeVisible();
});

it("Next from a later-kind issue continues into the next page in server order", async () => {
  const kindA = { ...diagnostic, issueKey: "a1", message: "First A" };

  const kindB = {
    ...diagnostic,
    issueKey: "b1",
    code: "missing_required_field",
    check: "ontology",
    message: "Only B",
  };

  const laterA = { ...diagnostic, issueKey: "a2", message: "Later A" };

  http.on("GET", "/api/v1/validation/diagnostics", (request) =>
    jsonReply(
      request.query.get("cursor")
        ? { generation: 1, total: 3, diagnostics: [laterA] }
        : { generation: 1, total: 3, nextCursor: "later", diagnostics: [kindA, kindB] },
    ),
  );
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope(1)} onOpenNote={vi.fn()} />);

  await screen.findByText("First A");
  fireEvent.click(document.querySelector('[data-issue-key="b1"]')!);
  expect(await screen.findByText("Only B")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Next issue" }));

  expect(await screen.findByText("Later A", { selector: "article *" })).toBeVisible();
});

it("shows server file totals for file groups in server order before every page loads", async () => {
  const inZ = { ...diagnostic, issueKey: "z1", primaryPath: "notes/z.md" };
  const inZAgain = { ...diagnostic, issueKey: "z2", primaryPath: "notes/z.md" };
  const vaultWide = { ...diagnostic, issueKey: "v1", primaryPath: undefined, affectedPaths: [] };

  const inA = {
    ...diagnostic,
    issueKey: "a1",
    primaryPath: undefined,
    affectedPaths: ["notes/a.md"],
  };

  http.on("GET", "/api/v1/validation/diagnostics", (request) =>
    jsonReply(
      request.query.get("sort") === "file"
        ? {
            generation: 1,
            total: 9,
            nextCursor: "later",
            diagnostics: [inZ, inZAgain, vaultWide, inA],
            fileTotals: { "notes/z.md": 4, "": 1, "notes/a.md": 1 },
          }
        : { generation: 1, total: 9, nextCursor: "later", diagnostics: [diagnostic] },
    ),
  );
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope(1)} onOpenNote={vi.fn()} />);

  await screen.findAllByText(diagnostic.message!);
  fireEvent.click(screen.getByRole("button", { name: "By file" }));

  const pathLabels = () =>
    [
      ...screen.getByRole("group", { name: "Issue list" }).querySelectorAll(".problems-row__path"),
    ].map((path) => path.textContent);

  await waitFor(() => expect(pathLabels()).toEqual(["notes/z.md", "Vault-wide", "notes/a.md"]));
  const zGroup = screen.getByText("z.md", { selector: "summary *" }).closest("summary");
  expect(zGroup?.querySelector(".problems-kind__count")).toHaveTextContent(/^4$/);
});

it("requests file order and repair applicability from the diagnostics API", async () => {
  const requests: URLSearchParams[] = [];

  http.on("GET", "/api/v1/validation/diagnostics", (request) => {
    requests.push(request.query);

    return jsonReply({ generation: 1, total: 1, diagnostics: [diagnostic], fileTotals: { "": 1 } });
  });
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope(1)} onOpenNote={vi.fn()} />);

  await screen.findAllByText(diagnostic.message!);
  fireEvent.click(screen.getByRole("button", { name: "By file" }));
  fireEvent.change(screen.getByRole("combobox", { name: "Repair" }), {
    target: { value: "applicable" },
  });

  await waitFor(() =>
    expect(
      requests.some(
        (query) => query.get("sort") === "file" && query.get("repairAvailability") === "applicable",
      ),
    ).toBe(true),
  );
  expect(screen.getByRole("option", { name: "Fixable here" })).toBeInTheDocument();
  expect(screen.getByRole("option", { name: "Not fixable here" })).toBeInTheDocument();
});

it("reports a failed issue load without claiming no issues match the filters", async () => {
  http.on("GET", "/api/v1/validation/diagnostics", () =>
    jsonReply({ error: "Rhizome backend proxy failed", code: "BAD_GATEWAY" }, 502),
  );
  renderWithQueryClient(<NotesIssuesHome validationEnvelope={envelope(1)} onOpenNote={vi.fn()} />);

  expect(await screen.findByText(/Could not load issue details/)).toBeVisible();
  expect(screen.queryByText("No issues match these filters.")).toBeNull();
});
