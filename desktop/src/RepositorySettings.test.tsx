import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { request, type SettingsChoices, type SetupPlan, type SetupResult } from "./api";
import { RepositorySettings } from "./RepositorySettings";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);

const id = "a".repeat(64);
const worktree = "/work/acme";

const current: SetupPlan = {
  root: worktree,
  name: "acme",
  configured: true,
  findings: { docs: "all Markdown (3 notes in docs/)", code: "Go" },
  workflow: "agentic-engineering",
  workflows: [
    {
      id: "agentic-engineering",
      label: "Agentic Engineering",
      description: "Specs, efforts, and engineering policy docs your agents follow",
      recommended: true,
    },
    {
      id: "none",
      label: "Search and agent guidance only",
      description: "Search, code navigation, and Rhizome guidance, with no workflow docs",
      recommended: false,
    },
  ],
  addons: [
    {
      id: "action-items",
      label: "Action items",
      description: "Track #action-item commitments in notes",
      defaultFor: ["agentic-engineering"],
      enabled: true,
    },
    {
      id: "compounding",
      label: "Compounding",
      description: "Review agent sessions for recurring friction",
      defaultFor: [],
      enabled: false,
    },
  ],
  agents: [
    { id: "claude", label: "Claude Code", enabled: true, detected: "marker" },
    { id: "codex", label: "Codex", enabled: false },
    { id: "cursor", label: "Cursor", enabled: false },
  ],
  search: {
    provider: "voyage",
    ready: true,
    providers: [
      {
        id: "voyage",
        label: "Voyage AI",
        ready: true,
        key: "VOYAGE_API_KEY",
        keyLabel: "Voyage API Key",
      },
      {
        id: "openai",
        label: "OpenAI",
        ready: false,
        key: "OPENAI_API_KEY",
        keyLabel: "OpenAI API Key",
      },
      { id: "ollama", label: "Ollama", ready: false, hint: "Install Ollama." },
      { id: "off", label: "Off", ready: true },
    ],
  },
  ignoredRepositories: [],
  pin: "v0.51.0",
  scope: { builtInApplies: false, keepIndexed: [], rules: [] },
  writes: { summary: [], files: [] },
};

function report(choices?: SettingsChoices): SetupPlan {
  if (!choices) return current;
  const addAddons = choices.addAddons ?? [];
  return {
    ...current,
    agents: current.agents.map((a) =>
      choices.agents ? { ...a, enabled: choices.agents.includes(a.id) } : a,
    ),
    addons: current.addons.map((a) => (addAddons.includes(a.id) ? { ...a, enabled: true } : a)),
    writes: {
      summary: ["Agents: Claude Code → Claude Code, Cursor", "+ 2 new Rhizome files"],
      files: [".cursor/rules/rhizome.mdc", ".rhizome/config.yml"],
      kept: [".agents/skills/rhizome/SKILL.md"],
    },
  };
}

const applied: SetupResult = {
  root: worktree,
  created: [".cursor/rules/rhizome.mdc"],
  updated: [".rhizome/config.yml"],
  summary: ["Agents: Claude Code, Cursor", "Created 1 Rhizome file"],
  commit: [".rhizome/", ".cursor/"],
  warnings: [],
  kept: [],
  pin: { version: "v0.51.0" },
};

function serve(overrides: Partial<Record<string, (args: unknown) => unknown>> = {}) {
  call.mockImplementation(async (operation, args) => {
    const handle = overrides[operation];
    if (handle) return handle(args) as never;
    if (operation === "settings-report")
      return report((args as { choices?: SettingsChoices }).choices) as never;
    if (operation === "settings-apply") return applied as never;
    throw new Error(`Unexpected operation: ${operation}`);
  });
}
function calls(operation: string) {
  return call.mock.calls.filter(([name]) => name === operation).map(([, args]) => args);
}
function tab(props: Partial<Parameters<typeof RepositorySettings>[0]> = {}) {
  const handlers = {
    onEditRules: vi.fn(),
    onManageInstallation: vi.fn(),
    onProviderSettings: vi.fn(),
    onChanged: vi.fn(),
  };
  render(<RepositorySettings repository={id} worktree={worktree} {...handlers} {...props} />);
  return handlers;
}
beforeEach(() => vi.resetAllMocks());

describe("repository settings", () => {
  it("shows the repository's settings as they are, with nothing to apply", async () => {
    serve();
    const { onEditRules } = tab();
    expect(await screen.findByRole("radio", { name: /Agentic Engineering/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /Action items/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /Compounding/ })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /Claude Code/ })).toBeChecked();
    expect(screen.getByLabelText("Search")).toHaveValue("voyage");
    expect(screen.getByText(/Rhizome v0.51.0, pinned for this repository/)).toBeVisible();
    expect(screen.queryByRole("region", { name: "Unapplied changes" })).toBeNull();
    expect(calls("settings-report")).toEqual([{ id, worktree }]);
    fireEvent.click(screen.getByRole("button", { name: "Edit rules…" }));
    expect(onEditRules).toHaveBeenCalled();
  });

  it("stages changes, lists what they write, and applies only what changed", async () => {
    serve();
    const { onChanged } = tab();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Cursor/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Compounding/ }));
    const bar = screen.getByRole("region", { name: "Unapplied changes" });
    expect(within(bar).getByText("2 changes not applied")).toBeVisible();
    expect(screen.getAllByRole("img", { name: "Changed" }).length).toBeGreaterThanOrEqual(2);
    expect(onChanged).toHaveBeenLastCalledWith(true);

    await waitFor(() => expect(within(bar).getByText("Writes 2 files")).toBeInTheDocument());
    expect(calls("settings-report").at(-1)).toEqual({
      id,
      worktree,
      choices: { agents: ["claude", "cursor"], addAddons: ["compounding"] },
    });
    expect(within(bar).getByText(".cursor/rules/rhizome.mdc")).toBeInTheDocument();
    expect(
      within(bar).getByText(/Left as you edited them: .agents\/skills\/rhizome\/SKILL.md/),
    ).toBeInTheDocument();

    fireEvent.click(within(bar).getByRole("button", { name: "Apply changes" }));
    expect(await screen.findByText("Created 1 Rhizome file")).toBeVisible();
    expect(
      screen.getByText("Commit .rhizome/ and .cursor/ so your team shares this setup."),
    ).toBeVisible();
    expect(calls("settings-apply")).toEqual([
      { id, worktree, choices: { agents: ["claude", "cursor"], addAddons: ["compounding"] } },
    ]);
    // Applying reloads the settings as they now are.
    await waitFor(() => expect(calls("settings-report").at(-1)).toEqual({ id, worktree }));
  });

  it("undoes one row and discards every change", async () => {
    serve();
    tab();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Cursor/ }));
    fireEvent.change(screen.getByLabelText("Search"), { target: { value: "off" } });
    expect(screen.getByText("2 changes not applied")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Undo agents" }));
    expect(screen.getByRole("checkbox", { name: /Cursor/ })).not.toBeChecked();
    expect(screen.getByText("1 change not applied")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.getByLabelText("Search")).toHaveValue("voyage");
    expect(screen.queryByRole("region", { name: "Unapplied changes" })).toBeNull();
  });

  it("sends a provider without a key to the app's provider settings", async () => {
    serve();
    const { onProviderSettings } = tab();
    fireEvent.change(await screen.findByLabelText("Search"), { target: { value: "openai" } });
    expect(screen.getByText("Needs a key on this Mac")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Save a key in Rhizome on this Mac" }));
    expect(onProviderSettings).toHaveBeenCalled();
  });

  it("offers to update files Rhizome has newer versions of with nothing changed", async () => {
    serve({
      "settings-report": () => ({
        ...current,
        writes: { summary: ["Update 3 Rhizome files"], files: ["a", "b", "c"] },
      }),
    });
    tab();
    const bar = await screen.findByRole("region", { name: "Unapplied changes" });
    expect(within(bar).getByText("Rhizome has updates for this repository's files")).toBeVisible();
    expect(within(bar).queryByRole("button", { name: "Discard" })).toBeNull();
    fireEvent.click(within(bar).getByRole("button", { name: "Update files" }));
    await waitFor(() => expect(calls("settings-apply")).toEqual([{ id, worktree, choices: {} }]));
  });

  it("keeps the choices and shows the error when applying fails", async () => {
    serve({
      "settings-apply": () => {
        throw { code: "settings_failed", message: "unknown add-on" };
      },
    });
    tab();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Cursor/ }));
    await waitFor(() => expect(screen.getByText("Writes 2 files")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Apply changes" }));
    expect(await screen.findByText("Could not apply: unknown add-on")).toBeVisible();
    expect(screen.getByRole("checkbox", { name: /Cursor/ })).toBeChecked();
  });

  it("asks for trust and explains an executable without settings support", async () => {
    let trusted = false;
    serve({
      "settings-report": () => {
        if (!trusted) throw { code: "trust_required", message: "Confirm trust." };
        throw {
          code: "setup_unsupported",
          message: "This Rhizome version cannot change repository settings from the app.",
        };
      },
      trust: () => {
        trusted = true;
        return null;
      },
    });
    const { onManageInstallation } = tab();
    fireEvent.click(await screen.findByRole("button", { name: "Trust and continue" }));
    expect(
      await screen.findByRole("heading", {
        name: "Rhizome needs an update to change settings from the app",
      }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Manage installation" }));
    expect(onManageInstallation).toHaveBeenCalled();
    expect(calls("trust")).toEqual([{ id, worktree }]);
  });
});
