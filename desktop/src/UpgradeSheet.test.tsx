import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { request, type SetupPlan, type SetupResult } from "./api";
import { UpgradeSheet, type UpgradeMode } from "./UpgradeSheet";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);
const plan: SetupPlan = {
  root: "/work/acme",
  name: "acme",
  configured: true,
  findings: { docs: "Markdown", code: "Go" },
  workflow: "none",
  workflows: [],
  addons: [],
  agents: [],
  ignoredRepositories: [],
  pin: "v0.52.0",
  search: { provider: "off", ready: true, providers: [] },
  scope: { builtInApplies: false, rules: [], keepIndexed: [] },
  writes: { summary: ["Update agent guidance"], files: ["AGENTS.md"], kept: ["CLAUDE.md"] },
};
const result: SetupResult = {
  root: plan.root,
  created: [".agents/skills/rhizome/SKILL.md"],
  updated: ["AGENTS.md"],
  summary: [],
  kept: ["CLAUDE.md"],
  warnings: ["One file needs manual comparison."],
  commit: [".rhizome/config.yml", "docs/Drew's notes.md", "$(touch nope).md"],
  pin: { version: "v0.52.0" },
};
const advance: UpgradeMode = {
  kind: "advance",
  offer: { state: "advance", pinned: "v0.51.0", available: "v0.52.0" },
};
function show(mode = advance) {
  const callbacks = {
    onPinned: vi.fn().mockResolvedValue(undefined),
    pinSaved: vi.fn().mockResolvedValue(false),
    onRestart: vi.fn().mockResolvedValue(undefined),
    onApplied: vi.fn(),
    onClose: vi.fn(),
  };
  render(
    <UpgradeSheet repository="repo" worktree={plan.root} name="acme" mode={mode} {...callbacks} />,
  );
  return callbacks;
}
beforeEach(() => {
  vi.resetAllMocks();
  call.mockImplementation(async (operation) => {
    if (operation === "upgrade-plan" || operation === "settings-report") return plan;
    if (operation === "pin-advance") return { pinnedVersion: "v0.52.0" };
    if (operation === "settings-apply") return result;
    throw new Error(operation);
  });
});

it("reviews files and kept edits before writing, then reports the result and copies quoted commit paths", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  const callbacks = show();
  expect(await screen.findByText("Update agent guidance")).toBeVisible();
  expect(screen.getByText("Kept edited files")).toBeVisible();
  fireEvent.click(screen.getByText("Show 1 files"));
  expect(screen.getByText("AGENTS.md")).toBeVisible();
  expect(call.mock.calls.map(([op]) => op)).toEqual(["upgrade-plan"]);
  fireEvent.click(screen.getByRole("button", { name: "Upgrade" }));
  expect(await screen.findByRole("heading", { name: "Upgraded to v0.52.0" })).toBeVisible();
  expect(screen.getByText("2 files changed")).toBeVisible();
  expect(screen.getByText(result.warnings[0])).toBeVisible();
  expect(callbacks.onRestart).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "Copy commit command" }));
  expect(writeText).toHaveBeenCalledWith(
    `git add -- '.rhizome/config.yml' 'docs/Drew'\\''s notes.md' '$(touch nope).md' && git commit -m "chore: upgrade Rhizome to v0.52.0"`,
  );
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  expect(callbacks.onClose).toHaveBeenCalledOnce();
});

it("copies a PowerShell commit command on Windows", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  window.__RHIZOME_PLATFORM__ = "windows";
  try {
    show();
    fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
    fireEvent.click(await screen.findByRole("button", { name: "Copy commit command" }));
    expect(writeText).toHaveBeenCalledWith(
      `git add -- '.rhizome/config.yml' 'docs/Drew''s notes.md' '$(touch nope).md'\ngit commit -m "chore: upgrade Rhizome to v0.52.0"`,
    );
  } finally {
    delete window.__RHIZOME_PLATFORM__;
  }
});

it("retries a failed plan without writing", async () => {
  call.mockRejectedValueOnce({
    code: "upgrade_plan_failed",
    message: "Could not plan the upgrade.",
  });
  show();
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not plan the upgrade.");
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(await screen.findByRole("button", { name: "Upgrade" })).toBeEnabled();
  expect(call.mock.calls.map(([op]) => op)).toEqual(["upgrade-plan", "upgrade-plan"]);
});

it("resumes a failed file update without advancing the pin again", async () => {
  let applies = 0;
  call.mockImplementation(async (operation) => {
    if (operation === "upgrade-plan") return plan;
    if (operation === "pin-advance") return null;
    if (++applies === 1) throw { code: "settings_failed", message: "Could not write AGENTS.md." };
    return result;
  });
  const callbacks = show();
  fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not write AGENTS.md.");
  expect(callbacks.onRestart).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByRole("heading", { name: "Upgraded to v0.52.0" });
  expect(call.mock.calls.map(([op]) => op)).toEqual([
    "upgrade-plan",
    "pin-advance",
    "settings-apply",
    "settings-apply",
  ]);
  expect(callbacks.onPinned).toHaveBeenCalledOnce();
  expect(callbacks.onRestart).toHaveBeenCalledOnce();
});

it("keeps the sheet open during an apply and retries only a failed restart", async () => {
  let finish!: (done: SetupResult) => void;
  call.mockImplementation(async (operation) => {
    if (operation === "upgrade-plan") return plan;
    if (operation === "pin-advance") return null;
    return new Promise<SetupResult>((resolve) => {
      finish = resolve;
    });
  });
  const callbacks = show();
  callbacks.onRestart.mockRejectedValueOnce(new Error("Runtime is attached."));
  fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
  await waitFor(() => expect(call).toHaveBeenCalledWith("settings-apply", expect.anything()));
  expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
  await act(async () => finish(result));
  expect(await screen.findByRole("alert")).toHaveTextContent("Runtime is attached.");
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByRole("button", { name: "Done" });
  expect(call.mock.calls.filter(([op]) => op === "settings-apply")).toHaveLength(1);
  expect(callbacks.onRestart).toHaveBeenCalledTimes(2);
});

it("refreshes files from the supplied plan without installing or restarting", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  const callbacks = show({ kind: "refresh", plan });
  expect(screen.getByRole("heading", { name: "Update Rhizome files for acme" })).toBeVisible();
  expect(screen.queryByRole("heading", { name: "Version" })).toBeNull();
  expect(call).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  await screen.findByRole("heading", { name: "Rhizome files updated" });
  expect(call.mock.calls.map(([op]) => op)).toEqual(["settings-apply"]);
  expect(callbacks.onRestart).not.toHaveBeenCalled();
  expect(callbacks.onApplied).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button", { name: "Copy commit command" }));
  expect(writeText.mock.calls[0][0]).toContain('git commit -m "chore: update Rhizome files"');
});

it("does not advance a saved pin again if clearing its executable override fails", async () => {
  const callbacks = show();
  callbacks.onPinned.mockRejectedValueOnce(new Error("Could not clear the override."));
  fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not clear the override.");
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByRole("button", { name: "Done" });
  expect(call.mock.calls.filter(([op]) => op === "pin-advance")).toHaveLength(1);
  expect(callbacks.onPinned).toHaveBeenCalledTimes(2);
});

it("continues with the files when an advance fails after saving the pin", async () => {
  call.mockImplementation(async (operation) => {
    if (operation === "upgrade-plan") return plan;
    if (operation === "pin-advance")
      throw { code: "pin_advance_failed", message: "Pin advance inspection stage failed." };
    if (operation === "settings-apply") return result;
    throw new Error(operation);
  });
  const callbacks = show();
  callbacks.pinSaved.mockResolvedValue(true);
  fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
  await screen.findByRole("heading", { name: "Upgraded to v0.52.0" });
  expect(call.mock.calls.map(([op]) => op)).toEqual([
    "upgrade-plan",
    "pin-advance",
    "settings-apply",
  ]);
  expect(callbacks.onPinned).toHaveBeenCalledOnce();
});

it("reports an advance that fails before saving the pin", async () => {
  call.mockImplementation(async (operation) => {
    if (operation === "upgrade-plan") return plan;
    if (operation === "pin-advance")
      throw { code: "pin_advance_failed", message: "Pin advance download stage failed." };
    throw new Error(operation);
  });
  const callbacks = show();
  fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Pin advance download stage failed.");
  expect(callbacks.onPinned).not.toHaveBeenCalled();
});

it("copies the changed pin when the new release has no generated file changes", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  call.mockImplementation(async (operation) => {
    if (operation === "upgrade-plan") return { ...plan, writes: { summary: [], files: [] } };
    if (operation === "pin-advance") return null;
    return { ...result, created: [], updated: [], commit: [] };
  });
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
  fireEvent.click(await screen.findByRole("button", { name: "Copy commit command" }));
  expect(writeText).toHaveBeenCalledWith(
    `git add -- '.rhizome/config.yml' && git commit -m "chore: upgrade Rhizome to v0.52.0"`,
  );
});

const decisions: NonNullable<SetupPlan["writes"]["decisions"]> = [
  {
    path: ".rhizome/modules/agentic-engineering/ontology/spec-driven.graphql",
    kind: "unknown",
    from: ".rhizome/ontology/spec-driven.graphql",
    diff: "--- current\n+++ desired\n- old\n+ new",
  },
  {
    path: ".agents/skills/rhizome/SKILL.md",
    kind: "edited",
    copies: [".claude/skills/rhizome/SKILL.md", ".cursor/skills/rhizome/SKILL.md"],
  },
  { path: "obsolete.md", kind: "remove" },
];
const decisionPlan: SetupPlan = {
  ...plan,
  writes: {
    summary: [],
    files: [],
    decisions,
    decisionSnapshot: "reviewed-snapshot",
    kept: decisions.map(({ path }) => path),
  },
};

it("defers every decision and groups mirrored copies with an expandable diff", async () => {
  show({ kind: "refresh", plan: decisionPlan });
  expect(screen.getByRole("button", { name: "Update files" })).toBeDisabled();
  for (const decision of decisions) {
    expect(screen.getByRole("combobox", { name: `Choice for ${decision.path}` })).toHaveValue(
      "later",
    );
  }
  expect(screen.getByText(`From ${decisions[0].from}`)).toBeVisible();
  expect(screen.getByText(/--- current/)).not.toBeVisible();
  fireEvent.click(screen.getByText("Show diff"));
  expect(screen.getByText(/--- current/)).toBeVisible();
  fireEvent.click(screen.getByText("Copies (2)"));
  expect(screen.getByText(decisions[1].copies![0])).toBeVisible();
  expect(screen.getByText("Rhizome's version removes this file.")).toBeVisible();
  expect(screen.queryByRole("heading", { name: "Kept edited files" })).toBeNull();
  expect(call).not.toHaveBeenCalled();
});

it("forwards only chosen target paths and leaves deferred decisions reachable", async () => {
  call.mockImplementation(async (operation) => {
    if (operation === "settings-apply") return { ...result, decisions: [decisions[2]] };
    if (operation === "settings-report")
      return { ...decisionPlan, writes: { ...decisionPlan.writes, decisions: [decisions[2]] } };
    throw new Error(operation);
  });
  show({ kind: "refresh", plan: decisionPlan });
  fireEvent.change(screen.getByRole("combobox", { name: `Choice for ${decisions[0].path}` }), {
    target: { value: "take" },
  });
  fireEvent.change(screen.getByRole("combobox", { name: `Choice for ${decisions[1].path}` }), {
    target: { value: "keep" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  await screen.findByRole("heading", { name: "1 decision remaining" });
  expect(call).toHaveBeenCalledWith("settings-apply", {
    id: "repo",
    worktree: plan.root,
    choices: {
      decisionSnapshot: "reviewed-snapshot",
      take: [decisions[0].path],
      keep: [decisions[1].path],
    },
  });
  fireEvent.click(screen.getByRole("button", { name: "Review" }));
  await screen.findByRole("combobox", { name: `Choice for ${decisions[2].path}` });
  expect(call).toHaveBeenLastCalledWith("settings-report", { id: "repo", worktree: plan.root });
  expect(screen.getByRole("button", { name: "Update files" })).toBeDisabled();
});

it("can choose Rhizome's version for all and return a choice to deferred", async () => {
  show({ kind: "refresh", plan: decisionPlan });
  fireEvent.click(screen.getByRole("button", { name: "Use Rhizome's version for all" }));
  fireEvent.change(screen.getByRole("combobox", { name: `Choice for ${decisions[1].path}` }), {
    target: { value: "later" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  await screen.findByRole("button", { name: "Done" });
  expect(call).toHaveBeenCalledWith("settings-apply", {
    id: "repo",
    worktree: plan.root,
    choices: {
      decisionSnapshot: "reviewed-snapshot",
      take: [decisions[0].path, decisions[2].path],
      keep: [],
    },
  });
});

it("does not send decision flags to legacy binaries", async () => {
  show({ kind: "refresh", plan });
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  await screen.findByRole("button", { name: "Done" });
  expect(call).toHaveBeenCalledWith("settings-apply", {
    id: "repo",
    worktree: plan.root,
    choices: {},
  });
});

it("reviews remaining decisions through settings after advancing the pin", async () => {
  call.mockImplementation(async (operation) => {
    if (operation === "upgrade-plan") return decisionPlan;
    if (operation === "pin-advance") return null;
    if (operation === "settings-apply") return { ...result, decisions };
    if (operation === "settings-report") return decisionPlan;
    throw new Error(operation);
  });
  show();
  fireEvent.click(await screen.findByRole("button", { name: "Upgrade" }));
  await screen.findByRole("heading", { name: "3 decisions remaining" });
  fireEvent.click(screen.getByRole("button", { name: "Review" }));
  await screen.findByRole("combobox", { name: `Choice for ${decisions[0].path}` });
  expect(call).toHaveBeenLastCalledWith("settings-report", { id: "repo", worktree: plan.root });
  expect(call.mock.calls.filter(([op]) => op === "upgrade-plan")).toHaveLength(1);
  expect(screen.getByRole("button", { name: "Upgrade" })).toBeDisabled();
  fireEvent.change(screen.getByRole("combobox", { name: `Choice for ${decisions[0].path}` }), {
    target: { value: "take" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Upgrade" }));
  await screen.findByRole("heading", { name: "3 decisions remaining" });
  expect(call.mock.calls.filter(([op]) => op === "pin-advance")).toHaveLength(1);
});

it("keeps the commit paths from earlier updates when reviewing remaining decisions", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  let applies = 0;
  call.mockImplementation(async (operation) => {
    if (operation === "settings-report") return decisionPlan;
    if (operation === "settings-apply") {
      applies++;
      return {
        ...result,
        commit: [applies === 1 ? "first.md" : "second.md"],
        decisions: applies === 1 ? decisions : [],
      };
    }
    throw new Error(operation);
  });
  show({
    kind: "refresh",
    plan: { ...decisionPlan, writes: { ...decisionPlan.writes, files: ["first.md"] } },
  });
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  fireEvent.click(await screen.findByRole("button", { name: "Review" }));
  fireEvent.click(await screen.findByRole("button", { name: "Use Rhizome's version for all" }));
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  fireEvent.click(await screen.findByRole("button", { name: "Copy commit command" }));
  expect(writeText).toHaveBeenCalledWith(
    expect.stringContaining("git add -- 'first.md' 'second.md'"),
  );
});

it.each([
  { summary: [], files: [], kept: ["custom.md"], decisions },
  { summary: [], files: [], kept: ["custom.md"], decisionSnapshot: "token" },
])("uses the legacy fallback when either snapshot field is absent", (writes) => {
  show({ kind: "refresh", plan: { ...plan, writes } });
  expect(screen.queryByRole("combobox")).toBeNull();
  expect(screen.getByText(/Run/)).toHaveTextContent("rzm init");
  expect(screen.getByRole("button", { name: "Update files" })).toBeDisabled();
});

it("sends the reviewed snapshot even when no decisions need a choice", async () => {
  show({
    kind: "refresh",
    plan: {
      ...plan,
      writes: { ...plan.writes, decisions: [], decisionSnapshot: "empty-decisions" },
    },
  });
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  await screen.findByRole("button", { name: "Done" });
  expect(call).toHaveBeenCalledWith("settings-apply", {
    id: "repo",
    worktree: plan.root,
    choices: { decisionSnapshot: "empty-decisions", take: [], keep: [] },
  });
});

it.each(["advance", "refresh"] as const)(
  "reviews current files with fresh choices after a snapshot apply fails in %s mode",
  async (kind) => {
    let applies = 0;
    call.mockImplementation(async (operation) => {
      if (operation === "upgrade-plan") return decisionPlan;
      if (operation === "pin-advance") return { pinnedVersion: "v0.52.0" };
      if (operation === "settings-report") {
        return {
          ...decisionPlan,
          writes: { ...decisionPlan.writes, decisionSnapshot: "fresh-snapshot" },
        };
      }
      if (operation === "settings-apply") {
        if (++applies === 1) throw new Error("File decisions changed since review.");
        return { ...result, decisions: [] };
      }
      throw new Error(operation);
    });
    const callbacks = show(kind === "advance" ? advance : { kind: "refresh", plan: decisionPlan });
    const button = kind === "advance" ? "Upgrade" : "Update files";
    const firstChoice = await screen.findByRole("combobox", {
      name: `Choice for ${decisions[0].path}`,
    });
    fireEvent.change(firstChoice, { target: { value: "take" } });
    fireEvent.click(screen.getByRole("button", { name: button }));
    expect(await screen.findByRole("alert")).toHaveTextContent("File decisions changed");
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Review again" }));
    expect(
      await screen.findByRole("combobox", { name: `Choice for ${decisions[0].path}` }),
    ).toHaveValue("later");
    expect(call).toHaveBeenLastCalledWith("settings-report", {
      id: "repo",
      worktree: plan.root,
    });
    expect(screen.getByRole("button", { name: button })).toBeDisabled();
    fireEvent.change(screen.getByRole("combobox", { name: `Choice for ${decisions[0].path}` }), {
      target: { value: "keep" },
    });
    fireEvent.click(screen.getByRole("button", { name: button }));
    await screen.findByRole("button", { name: "Done" });
    expect(call).toHaveBeenLastCalledWith("settings-apply", {
      id: "repo",
      worktree: plan.root,
      choices: { decisionSnapshot: "fresh-snapshot", take: [], keep: [decisions[0].path] },
    });
    expect(call.mock.calls.filter(([op]) => op === "pin-advance")).toHaveLength(
      kind === "advance" ? 1 : 0,
    );
    expect(callbacks.onRestart).toHaveBeenCalledTimes(kind === "advance" ? 1 : 0);
  },
);

it("offers a fresh review after an automatic update with an empty decision list fails", async () => {
  const snapshotPlan = {
    ...plan,
    writes: { ...plan.writes, decisions: [], decisionSnapshot: "empty-decisions" },
  };
  call.mockImplementation(async (operation) => {
    if (operation === "settings-apply") throw new Error("A new file needs review.");
    if (operation === "settings-report") return decisionPlan;
    throw new Error(operation);
  });
  show({ kind: "refresh", plan: snapshotPlan });
  fireEvent.click(screen.getByRole("button", { name: "Update files" }));
  fireEvent.click(await screen.findByRole("button", { name: "Review again" }));
  expect(
    await screen.findByRole("combobox", { name: `Choice for ${decisions[0].path}` }),
  ).toHaveValue("later");
  expect(call.mock.calls.map(([operation]) => operation)).toEqual([
    "settings-apply",
    "settings-report",
  ]);
});
