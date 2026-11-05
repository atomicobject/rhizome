import type {
  AgentEvent,
  AgentHarnessCapabilities,
  AgentHarnessStatus,
  AgentSession,
  AgentSettingsResponse,
} from "../api/types";
import type { FakeFetch } from "../test/fakeFetch";

export const capabilities: AgentHarnessCapabilities = {
  supportsAllowedTools: true,
  supportsAllowForSession: true,
  permissionModes: ["approval-required", "auto-accept-edits"],
};

export const readyStatus: AgentHarnessStatus = {
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

export function settingsWith(status: AgentHarnessStatus): AgentSettingsResponse {
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

export function session(overrides: Partial<AgentSession> = {}): AgentSession {
  return {
    id: "agt-1",
    title: "Harness chat",
    harness: "codex",
    createdAt: "2026-09-12T12:00:00Z",
    updatedAt: "2026-09-12T12:00:00Z",
    ...overrides,
  };
}

export function setup(
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
