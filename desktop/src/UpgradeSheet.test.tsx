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
    if (operation === "upgrade-plan") return plan;
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
  expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
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
