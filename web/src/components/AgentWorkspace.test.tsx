import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
  AgentEvent,
  AgentHarnessCapabilities,
  AgentHarnessStatus,
  AgentSession,
  AgentSettingsResponse,
} from "../api/types";
import { withFakeEventSource } from "../test/fakeEventSource";
import { deferredReply, jsonReply, type FakeFetch, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { AgentWorkspace } from "./AgentWorkspace";

const capabilities: AgentHarnessCapabilities = {
  supportsAllowedTools: true,
  supportsAllowForSession: true,
  permissionModes: ["approval-required", "auto-accept-edits"],
};

const readyStatus: AgentHarnessStatus = {
  installed: true,
  loggedIn: true,
  version: "1.2.3",
  account: "drew@example.com",
  models: [
    {
      id: "gpt-standard",
      displayName: "GPT Standard",
      efforts: ["medium", "high"],
      default: true,
    },
    { id: "gpt-fast", displayName: "GPT Fast", efforts: ["low"] },
  ],
  capabilities: { ...capabilities, permissionModes: [...capabilities.permissionModes] },
};

function settingsWith(status: AgentHarnessStatus): AgentSettingsResponse {
  return {
    settings: {
      harness: "codex",
      harnesses: {
        codex: {
          model: "gpt-standard",
          effort: "medium",
          permissionMode: "approval-required",
        },
        claude: { permissionMode: "approval-required" },
      },
    },
    resolved: { harness: "codex", reason: "configured" },
    harnesses: [{ kind: "codex", status }],
  };
}

function session(overrides: Partial<AgentSession> = {}): AgentSession {
  return {
    id: "agt-1",
    title: "Harness chat",
    harness: "codex",
    createdAt: "2026-09-12T12:00:00Z",
    updatedAt: "2026-09-12T12:00:00Z",
    ...overrides,
  };
}

function setup(
  http: FakeFetch,
  options: {
    status?: AgentHarnessStatus;
    current?: AgentSession;
    events?: AgentEvent[];
  } = {},
) {
  const current = options.current;
  http
    .json("GET", "/api/agent/settings", settingsWith(options.status ?? readyStatus))
    .json("GET", "/api/agent/sessions", { sessions: current ? [current] : [] });

  if (current) {
    http.json("GET", `/api/agent/sessions/${current.id}`, {
      session: current,
      messages: [],
      events: options.events ?? [],
    });
  }
}

describe("AgentWorkspace", () => {
  const http = withFakeFetch();
  const sources = withFakeEventSource();

  it.each(["select another", "create another", "select the deleted chat"])(
    "applies delayed deletion to the current list and selection when users %s",
    async (action) => {
      const first = session({ id: "first", title: "First chat" });
      const second = session({ id: "second", title: "Second chat" });
      const third = session({ id: "third", title: "Third chat" });
      const created = session({ id: "created", title: "Created chat" });
      const deleted = action === "select the deleted chat" ? second : first;
      setup(http);
      http.json("GET", "/api/agent/sessions", { sessions: [first, second, third] });

      for (const item of [first, second, third, created]) {
        http.json("GET", `/api/agent/sessions/${item.id}`, {
          session: item,
          messages: [],
          events: [],
        });
      }

      const deletion = deferredReply<{ deleted: boolean }>();
      http.on("DELETE", `/api/agent/sessions/${deleted.id}`, () => deletion.promise);
      http.json("POST", "/api/agent/sessions", { session: created });
      render(<AgentWorkspace />);
      await screen.findByRole("heading", { name: first.title });
      fireEvent.click(screen.getByRole("button", { name: `Delete ${deleted.title}` }));
      fireEvent.click(screen.getByRole("button", { name: `Confirm delete ${deleted.title}` }));

      if (action === "create another") {
        fireEvent.click(screen.getByRole("button", { name: "New" }));
        await screen.findByRole("heading", { name: created.title });
      } else {
        const next = action === "select another" ? third : second;
        fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${next.title} `) }));
        await screen.findByRole("heading", { name: next.title });
      }

      await act(async () => deletion.resolve({ deleted: true }));

      const expected =
        action === "create another" ? created : action === "select another" ? third : first;

      await waitFor(() => {
        expect(
          screen.queryByRole("button", { name: new RegExp(`^${deleted.title} `) }),
        ).not.toBeInTheDocument();
        expect(screen.getByRole("heading", { name: expected.title })).toBeVisible();
        expect(
          screen.getByRole("button", { name: new RegExp(`^${expected.title} `) }),
        ).toBeVisible();
      });
    },
  );

  it.each<[string, AgentHarnessStatus, string]>([
    [
      "not installed",
      {
        installed: false,
        loggedIn: false,
        lastError: "exec: codex: executable file not found",
        capabilities,
      },
      "Install the Codex CLI to use chat.",
    ],
    [
      "not logged in",
      {
        installed: true,
        loggedIn: false,
        loginHint: "codex login --device-auth",
        capabilities,
      },
      "Login required",
    ],
    [
      "probe error before login",
      {
        installed: true,
        loggedIn: false,
        version: "0.149.0",
        lastError: "codex 0.149.0 is older than minimum 0.150.0",
        loginHint: "codex login",
        capabilities,
      },
      "codex 0.149.0 is older than minimum 0.150.0",
    ],
    ["ready", readyStatus, "Ready · 1.2.3 · drew@example.com"],
    [
      "degraded",
      {
        ...readyStatus,
        lastError: "status probe timed out",
      },
      "status probe timed out",
    ],
  ])("renders the %s harness state", async (_name, status, expected) => {
    setup(http, { status });
    render(<AgentWorkspace />);

    const heading = await screen.findByRole("heading", { name: "Rhizome agent" });
    await waitFor(() => expect(heading.closest("header")).toHaveTextContent(expected));
    expect(screen.getAllByText("Codex").length).toBeGreaterThan(0);

    if (status.loginHint && !status.lastError) {
      const command = screen.getByText(status.loginHint);
      expect(command).toBeVisible();
      expect(command.tagName).toBe("CODE");
      expect(screen.getByRole("button", { name: "Copy" })).toBeVisible();
    }

    if (!status.installed && status.lastError) {
      expect(screen.getByText(status.lastError)).toBeVisible();
    }

    if (status.installed && status.lastError && status.loginHint) {
      expect(screen.queryByText(status.loginHint)).not.toBeInTheDocument();
    }
  });

  it("shows the resolution reason when no harness is configured", async () => {
    const response = settingsWith(readyStatus);
    response.settings.harness = undefined;
    response.resolved.reason = "first available";
    http
      .json("GET", "/api/agent/settings", response)
      .json("GET", "/api/agent/sessions", { sessions: [] });

    render(<AgentWorkspace />);
    expect(await screen.findByText("Resolved: first available")).toBeVisible();
  });

  it("uses the Claude Code install hint", async () => {
    const response = settingsWith({ installed: false, loggedIn: false, capabilities });
    response.settings.harness = "claude";
    response.resolved.harness = "claude";
    response.harnesses = [
      { kind: "claude", status: { installed: false, loggedIn: false, capabilities } },
    ];
    http
      .json("GET", "/api/agent/settings", response)
      .json("GET", "/api/agent/sessions", { sessions: [] });

    render(<AgentWorkspace />);
    expect(await screen.findByText(/Install Claude Code to use chat/)).toBeVisible();
  });

  it("renders approval details, posts a decision, and disables the controls", async () => {
    const current = session({ turnRunning: true });

    const approval: AgentEvent = {
      id: 3,
      sessionId: current.id,
      type: "approval-requested",
      createdAt: "2026-09-12T12:01:00Z",
      result: {
        turnId: "turn-1",
        requestId: "approval-1",
        name: "Run command",
        command: "make check",
        paths: ["web/src/components/AgentWorkspace.tsx"],
        reason: "Verify the change",
        allowForSession: true,
        input: { command: "make check" },
      },
    };

    setup(http, { current, events: [approval] });
    http.json("POST", `/api/agent/sessions/${current.id}/approvals/approval-1`, {
      responded: true,
    });

    render(<AgentWorkspace />);
    const prompt = await screen.findByRole("region", { name: "Approval requested" });
    expect(prompt).toHaveTextContent("Run command");
    expect(prompt).toHaveTextContent("make check");
    expect(prompt).toHaveTextContent("AgentWorkspace.tsx");
    expect(prompt).toHaveTextContent("Verify the change");
    expect(within(prompt).getByRole("button", { name: "Allow for session" })).toBeVisible();

    fireEvent.click(within(prompt).getByRole("button", { name: "Allow" }));
    await waitFor(() =>
      expect(http.count("POST", `/api/agent/sessions/${current.id}/approvals/approval-1`)).toBe(1),
    );
    expect(await within(prompt).findByText("Decision: allow")).toBeVisible();

    for (const button of within(prompt).getAllByRole("button")) expect(button).toBeDisabled();
  });

  it("hides allow-for-session when the approval request does not offer it", async () => {
    const current = session({ turnRunning: true });
    setup(http, {
      current,
      events: [
        {
          id: 1,
          sessionId: current.id,
          type: "approval-requested",
          createdAt: "2026-09-12T12:01:00Z",
          result: { requestId: "approval-1", name: "Write file" },
        },
      ],
    });
    render(<AgentWorkspace />);

    await screen.findByRole("region", { name: "Approval requested" });
    expect(screen.queryByRole("button", { name: "Allow for session" })).not.toBeInTheDocument();
  });

  it("keeps the turn active across a retrying error until completion", async () => {
    const current = session({ turnRunning: true });
    setup(http, { current });
    render(<AgentWorkspace />);

    await waitFor(() => expect(sources.instances).toHaveLength(1));
    const composer = screen.getByLabelText("Message");
    expect(composer).toBeDisabled();
    expect(screen.getByRole("button", { name: "Interrupt" })).toBeVisible();

    act(() => {
      sources.latest().emitJSON(
        {
          id: 5,
          sessionId: current.id,
          type: "error",
          content: "temporary failure",
          result: { turnId: "turn-1", status: "retrying" },
          createdAt: "2026-09-12T12:01:02Z",
        },
        "agent_event",
      );
    });
    expect(await screen.findByText("temporary failure")).toBeVisible();
    expect(composer).toBeDisabled();
    expect(screen.getByRole("button", { name: "Interrupt" })).toBeVisible();

    http.json("GET", `/api/agent/sessions/${current.id}`, {
      session: { ...current, turnRunning: false },
      messages: [],
      events: [],
    });
    act(() => {
      sources.latest().emitJSON(
        {
          id: 6,
          sessionId: current.id,
          type: "turn-completed",
          result: { turnId: "turn-1" },
          createdAt: "2026-09-12T12:01:03Z",
        },
        "agent_event",
      );
    });
    await waitFor(() => expect(composer).toBeEnabled());
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Interrupt" })).not.toBeInTheDocument(),
    );
  });

  it("offers interrupt during a turn and renders the interruption event", async () => {
    const current = session({ turnRunning: true });
    setup(http, {
      current,
      events: [
        {
          id: 4,
          sessionId: current.id,
          type: "interrupt-requested",
          createdAt: "2026-09-12T12:02:00Z",
        },
      ],
    });
    http.json("POST", `/api/agent/sessions/${current.id}/interrupt`, { interrupted: true });

    render(<AgentWorkspace />);
    expect(await screen.findByText("Interrupt requested")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Interrupt" }));
    await waitFor(() =>
      expect(http.count("POST", `/api/agent/sessions/${current.id}/interrupt`)).toBe(1),
    );
  });

  it("renders pre-harness history as read-only", async () => {
    setup(http, { current: session({ harness: undefined, readOnly: true }) });
    render(<AgentWorkspace />);

    expect(await screen.findByText(/Read-only history from before harness sessions/)).toBeVisible();
    expect(screen.getByLabelText("Message")).toBeDisabled();
    expect(screen.getByText("This pre-harness session is read-only.")).toBeVisible();
  });

  it("couples effort choices to the selected model and saves both", async () => {
    setup(http);
    http.on("PUT", "/api/agent/settings", async (request) => {
      // SAFETY: the settings client always sends this documented request shape.
      const body = JSON.parse(request.body || "{}") as {
        settings: AgentSettingsResponse["settings"];
      };

      return jsonReply({ ...settingsWith(readyStatus), settings: body.settings });
    });
    render(<AgentWorkspace />);

    await screen.findByRole("option", { name: "GPT Fast" });
    fireEvent.change(screen.getByLabelText("Model"), { target: { value: "gpt-fast" } });

    // Switching models never invents an effort: the vendor default stays selected
    // until the user picks one, and only that model's efforts are offered.
    await waitFor(() => expect(screen.getByLabelText("Effort")).toHaveValue(""));
    expect(within(screen.getByLabelText("Effort")).getAllByRole("option")).toHaveLength(2);
    fireEvent.change(screen.getByLabelText("Effort"), { target: { value: "low" } });
    await waitFor(() => {
      // SAFETY: this is the request produced by the typed settings client above.
      const body = JSON.parse(String(http.requests("PUT", "/api/agent/settings").at(-1)?.body)) as {
        settings: AgentSettingsResponse["settings"];
      };

      expect(body.settings.harnesses.codex).toMatchObject({ model: "gpt-fast", effort: "low" });
    });
  });

  it("updates phased items in place and streams assistant text through the live region", async () => {
    const current = session({ turnRunning: true });
    setup(http, {
      current,
      events: [
        {
          id: 1,
          sessionId: current.id,
          type: "command-execution",
          createdAt: "2026-09-12T12:01:00Z",
          result: { turnId: "turn-1", itemId: "item-1", command: "go test", phase: "started" },
        },
        {
          id: 2,
          sessionId: current.id,
          type: "command-execution",
          createdAt: "2026-09-12T12:01:01Z",
          result: {
            turnId: "turn-1",
            itemId: "item-1",
            phase: "completed",
            exitCode: 0,
            output: "ok",
          },
        },
        {
          id: 3,
          sessionId: current.id,
          type: "file-change",
          createdAt: "2026-09-12T12:01:02Z",
          result: {
            turnId: "turn-1",
            itemId: "item-2",
            name: "Edit file",
            paths: ["web/src/components/AgentWorkspace.tsx"],
            input: { patch: "workspace" },
            phase: "started",
          },
        },
        {
          id: 4,
          sessionId: current.id,
          type: "file-change",
          createdAt: "2026-09-12T12:01:03Z",
          result: {
            turnId: "turn-1",
            itemId: "item-2",
            phase: "completed",
          },
        },
      ],
    });
    render(<AgentWorkspace />);
    expect(await screen.findAllByText(/go test/)).toHaveLength(1);
    expect(screen.getByText(/Edit file/)).toBeVisible();
    expect(screen.getByText("web/src/components/AgentWorkspace.tsx")).toBeInTheDocument();
    expect(screen.getByText(/workspace/)).toBeInTheDocument();

    await waitFor(() => expect(sources.instances).toHaveLength(1));
    act(() => {
      sources.latest().emitJSON(
        {
          id: 5,
          sessionId: current.id,
          type: "message_delta",
          role: "assistant",
          content: "pong",
          createdAt: "2026-09-12T12:01:02Z",
          result: { turnId: "turn-1" },
        },
        "agent_event",
      );
    });
    expect(await screen.findByRole("article", { name: "Assistant response" })).toHaveTextContent(
      "pong",
    );
  });

  it("resumes assistant events after an HTTP failure closes the stream", async () => {
    const current = session({ turnRunning: true });
    setup(http, { current });
    const { unmount } = render(<AgentWorkspace />);
    await waitFor(() => expect(sources.instances).toHaveLength(1));
    const failed = sources.latest();
    vi.useFakeTimers();

    try {
      act(() => {
        failed.emitJSON(
          {
            id: 9,
            sessionId: current.id,
            type: "message_delta",
            role: "assistant",
            content: "Before outage",
            createdAt: "2026-09-12T12:01:00Z",
            result: { turnId: "turn-2" },
          },
          "agent_event",
        );
        failed.fail();
        vi.advanceTimersByTime(3_000);
      });
      const reopened = sources.latest();
      expect(reopened).not.toBe(failed);
      expect(reopened.url).toContain(`/sessions/${current.id}/events`);
      expect(new URL(reopened.url, "http://localhost").searchParams.get("after")).toBe("9");
      act(() => {
        reopened.emitJSON(
          {
            id: 10,
            sessionId: current.id,
            type: "message_delta",
            role: "assistant",
            content: "Recovered answer",
            createdAt: "2026-09-12T12:02:00Z",
            result: { turnId: "turn-2" },
          },
          "agent_event",
        );
      });
      expect(screen.getByRole("article", { name: "Assistant response" })).toHaveTextContent(
        "Recovered answer",
      );
    } finally {
      unmount();
      vi.useRealTimers();
    }
  });

  it("closes the event stream when unmounted", async () => {
    setup(http, { current: session() });
    const { unmount } = render(<AgentWorkspace />);
    await waitFor(() => expect(sources.instances).toHaveLength(1));
    const source = sources.latest();
    const remove = vi.spyOn(source, "removeEventListener");
    vi.useFakeTimers();

    try {
      source.fail();
      unmount();
      vi.advanceTimersByTime(3_000);
      expect(remove).toHaveBeenCalledWith("agent_event", expect.any(Function));
      expect(source.closed).toBe(true);
      expect(sources.instances).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });
});
