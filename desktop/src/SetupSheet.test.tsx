import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { request, type Scope, type SetupPlan, type SetupResult } from "./api";
import { ScopePage } from "./IndexScope";
import { SetupSheet } from "./SetupSheet";

vi.mock("./api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);

const id = "a".repeat(64);
const worktree = "/work/acme";
const scope: Scope = {
  builtInApplies: false,
  keepIndexed: [],
  rules: [
    { layer: "gitignore", source: "web/.gitignore", line: 1, pattern: "*.log", folder: "web" },
    {
      layer: "rhizome",
      source: ".rhizome/ignore",
      line: 16,
      pattern: "node_modules/",
      planned: true,
    },
    {
      layer: "rhizome",
      source: ".rhizome/ignore",
      line: 36,
      pattern: "/testdata/",
      reason: "test fixtures",
      planned: true,
    },
  ],
};
const plan: SetupPlan = {
  root: worktree,
  name: "acme",
  findings: {
    docs: "all Markdown (3 notes in docs/)",
    code: "Go (2 files)",
    skip: "testdata/ (1 file)",
  },
  workflow: "agentic-engineering",
  workflows: [
    {
      id: "agentic-engineering",
      label: "Agentic Engineering",
      description: "Specs and efforts",
      recommended: true,
    },
    {
      id: "none",
      label: "Search and agent guidance only",
      description: "No workflow docs",
      recommended: false,
    },
  ],
  addons: [
    {
      id: "action-items",
      label: "Action items",
      description: "Track commitments",
      defaultFor: ["agentic-engineering"],
      enabled: true,
    },
  ],
  agents: [
    { id: "claude", label: "Claude Code", enabled: true, detected: "installed" },
    { id: "codex", label: "Codex", enabled: false },
    { id: "cursor", label: "Cursor", enabled: false },
  ],
  search: {
    provider: "voyage",
    ready: false,
    providers: [
      {
        id: "voyage",
        label: "Voyage AI",
        ready: false,
        key: "VOYAGE_API_KEY",
        keyLabel: "Voyage API Key",
      },
      { id: "off", label: "Off", ready: true },
    ],
  },
  ignoredRepositories: [{ path: "tools/sdk", included: false, source: ".gitignore", line: 1 }],
  pin: "v0.50.5",
  scope,
  writes: {
    summary: [".rhizome/config.yml", "Rhizome guidance in AGENTS.md"],
    files: [".rhizome/config.yml", "AGENTS.md"],
  },
};
const done: SetupResult = {
  root: worktree,
  summary: [".rhizome/config.yml", "Voyage API Key saved to ~/.config/rhizome/config.yml"],
  commit: [".rhizome/", "AGENTS.md"],
  warnings: [],
  kept: [],
  pin: { version: "v0.50.5" },
};

function serve(overrides: Partial<Record<string, (args: unknown) => unknown>> = {}) {
  call.mockImplementation(async (operation, args) => {
    const handle = overrides[operation];
    if (handle) return handle(args) as never;
    if (operation === "setup-report") return plan;
    if (operation === "initialize") return { result: done, folder: {} };
    throw new Error(`Unexpected operation: ${operation}`);
  });
}
function sheet(onOpen = vi.fn(), onManageInstallation = vi.fn()) {
  render(
    <SetupSheet
      repository={id}
      worktree={worktree}
      onOpen={onOpen}
      onManageInstallation={onManageInstallation}
    />,
  );
  return { onOpen, onManageInstallation };
}
function calls(operation: string) {
  return call.mock.calls.filter(([name]) => name === operation).map(([, args]) => args);
}
beforeEach(() => vi.resetAllMocks());

describe("setup sheet", () => {
  it("sets up with the changed choices and sends the key only with setup", async () => {
    serve();
    const { onOpen } = sheet();
    expect(await screen.findByRole("heading", { name: "Set up Rhizome in acme" })).toBeVisible();
    expect(screen.getByText("all Markdown (3 notes in docs/)")).toBeVisible();
    expect(
      screen.getByText(
        "Pins Rhizome v0.50.5 for this repository and trusts this worktree to run it.",
      ),
    ).toBeVisible();

    fireEvent.click(screen.getByRole("checkbox", { name: /Action items/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Codex/ }));
    fireEvent.change(screen.getByLabelText("Voyage API Key"), { target: { value: "  pa-test  " } });
    await waitFor(() => expect(calls("setup-report")).toHaveLength(2));
    expect(calls("setup-report")[1]).toMatchObject({
      choices: { addons: [], agents: ["claude", "codex"] },
    });

    fireEvent.click(screen.getByRole("button", { name: "Set up Rhizome" }));
    expect(await screen.findByRole("heading", { name: "Rhizome is set up in acme" })).toBeVisible();
    expect(calls("initialize")).toEqual([
      {
        id,
        worktree,
        key: "pa-test",
        choices: {
          workflow: "agentic-engineering",
          addons: [],
          agents: ["claude", "codex"],
          search: "voyage",
          skip: [],
          keepIndexed: [],
          includeIgnored: [],
        },
      },
    ]);
    expect(JSON.stringify(calls("setup-report"))).not.toContain("pa-test");
    expect(screen.getByText("Voyage API Key saved to ~/.config/rhizome/config.yml")).toBeVisible();
    expect(
      screen.getByText("Commit .rhizome/ and AGENTS.md so your team shares this setup."),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Open workspace" }));
    expect(onOpen).toHaveBeenCalled();
  });

  it("follows a workflow's default add-ons when the workflow changes", async () => {
    serve();
    sheet();
    fireEvent.click(await screen.findByRole("radio", { name: /Search and agent guidance only/ }));
    expect(screen.getByRole("checkbox", { name: /Action items/ })).not.toBeChecked();
    fireEvent.click(screen.getByRole("radio", { name: /Agentic Engineering/ }));
    expect(screen.getByRole("checkbox", { name: /Action items/ })).toBeChecked();
  });

  it("holds What gets indexed edits until setup", async () => {
    serve();
    sheet();
    fireEvent.click(await screen.findByRole("button", { name: "What gets indexed…" }));
    expect(screen.getByRole("heading", { name: "What gets indexed in acme" })).toBeVisible();
    const inherited = screen.getByRole("region", { name: "Inherited from .gitignore" });
    expect(within(inherited).getByText("inside web/")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Keep indexed: node_modules/" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Keep indexed: /testdata/" }));
    fireEvent.change(screen.getByLabelText("Folder or file"), { target: { value: "web/gen" } });
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    fireEvent.change(screen.getByLabelText("Folder or file"), { target: { value: "tools/sdk" } });
    fireEvent.click(screen.getByRole("button", { name: "Index ignored folder" }));
    fireEvent.click(screen.getByRole("button", { name: "Back to setup" }));
    fireEvent.click(screen.getByRole("button", { name: "Set up Rhizome" }));

    await screen.findByRole("heading", { name: "Rhizome is set up in acme" });
    expect(calls("initialize")[0]).toMatchObject({
      choices: { skip: ["web/gen"], keepIndexed: ["testdata"], includeIgnored: ["tools/sdk"] },
    });
  });

  it("asks for trust before reporting when setup runs the worktree's own Rhizome", async () => {
    let trusted = false;
    serve({
      "setup-report": () => {
        if (!trusted) throw { code: "trust_required", message: "Confirm trust" };
        return plan;
      },
      trust: () => {
        trusted = true;
        return null;
      },
    });
    sheet();
    fireEvent.click(await screen.findByRole("button", { name: "Trust and continue" }));
    expect(await screen.findByRole("heading", { name: "Set up Rhizome in acme" })).toBeVisible();
    expect(calls("trust")).toEqual([{ id, worktree }]);
  });

  it("explains an executable that cannot set up from the app", async () => {
    serve({
      "setup-report": () => {
        throw {
          code: "setup_unsupported",
          message: "This Rhizome version cannot set up from the app.",
        };
      },
    });
    const { onManageInstallation } = sheet();
    expect(
      await screen.findByRole("heading", {
        name: "Rhizome needs an update to set up from the app",
      }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Manage installation" }));
    expect(onManageInstallation).toHaveBeenCalled();
  });

  it("reports a pinned install that failed after setup wrote its files", async () => {
    serve({
      initialize: () => ({
        result: { ...done, pin: { version: "v0.50.5", error: "404 Not Found" } },
        folder: {},
      }),
    });
    sheet();
    fireEvent.click(await screen.findByRole("button", { name: "Set up Rhizome" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Rhizome v0.50.5 could not be installed for this repository: 404 Not Found.",
    );
  });

  it("lets a refused held edit be undone", async () => {
    serve({
      "setup-report": (args) => {
        const { choices } = args as { choices?: { skip: string[] } };
        if (choices?.skip.includes("nope")) {
          throw {
            code: "setup_failed",
            message: "nope is not a folder or file in this repository",
          };
        }
        return plan;
      },
    });
    sheet();
    fireEvent.click(await screen.findByRole("button", { name: "What gets indexed…" }));
    fireEvent.change(screen.getByLabelText("Folder or file"), { target: { value: "nope" } });
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("nope is not a folder or file");

    fireEvent.click(screen.getByRole("button", { name: "Undo skip: nope" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Back to setup" }));
    expect(screen.getByRole("button", { name: "Set up Rhizome" })).toBeEnabled();
  });

  it("offers only Open workspace when setup wrote part of the configuration", async () => {
    serve({
      initialize: () => {
        throw { code: "setup_partial", message: "later write failed" };
      },
    });
    const { onOpen } = sheet();
    fireEvent.click(await screen.findByRole("button", { name: "Set up Rhizome" }));
    expect(await screen.findByText("Setup failed: later write failed")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(calls("setup-report")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Open workspace" }));
    expect(onOpen).toHaveBeenCalled();
  });

  it("keeps the sheet and its choices when setup fails", async () => {
    serve({
      initialize: () => {
        throw { code: "setup_failed", message: "disk full" };
      },
    });
    sheet();
    fireEvent.click(await screen.findByRole("checkbox", { name: /Action items/ }));
    fireEvent.click(screen.getByRole("button", { name: "Set up Rhizome" }));
    expect(await screen.findByText("Setup failed: disk full")).toBeVisible();
    expect(screen.getByRole("checkbox", { name: /Action items/ })).not.toBeChecked();
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
  });
});

describe("what gets indexed after setup", () => {
  const configured: Scope = {
    builtInApplies: false,
    keepIndexed: ["docs/generated"],
    rules: [
      {
        layer: "rhizome",
        source: ".rhizome/ignore",
        line: 6,
        pattern: "/testdata/",
        reason: "test fixtures",
      },
      { layer: "config", source: ".rhizome/config.yml", pattern: "drafts/**" },
    ],
  };

  it("writes each edit through the worktree's Rhizome and shows what it refuses", async () => {
    call.mockImplementation(async (operation, args) => {
      if (operation === "scope") return { ...configured, root: worktree };
      if (operation === "scope-edit") {
        const { edits } = args as { edits: { skip: string[] } };
        if (edits.skip.includes("nope"))
          throw { code: "scope_failed", message: "nope is not a folder or file" };
        return { ...configured, rules: [], root: worktree };
      }
      throw new Error(`Unexpected operation: ${operation}`);
    });
    render(
      <ScopePage
        repository={id}
        worktree={worktree}
        name="acme"
        onClose={vi.fn()}
        onManageInstallation={vi.fn()}
      />,
    );
    expect(await screen.findByText("docs/generated")).toBeVisible();
    const excludes = screen.getByRole("region", { name: "Notes excludes" });
    expect(within(excludes).queryByRole("button")).toBeNull();

    fireEvent.change(screen.getByLabelText("Folder or file"), { target: { value: "nope" } });
    fireEvent.click(screen.getByRole("button", { name: "Skip" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("nope is not a folder or file");
    expect(screen.getByText("/testdata/")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Index again: /testdata/" }));
    await waitFor(() => expect(screen.queryByText("/testdata/")).toBeNull());
    expect(calls("scope-edit")[1]).toEqual({
      id,
      worktree,
      edits: { skip: [], removeRules: ["/testdata/"], includeIgnored: [] },
    });
  });
});
