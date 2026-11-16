import { describe, expect, it } from "vitest";

import type { AgentSettingsResponse } from "../../api/types";
import { capabilities, readyStatus, settingsWith } from "../AgentWorkspace.test-support";
import { agentGroups, blockerFor, currentChoice, hasMoreModels } from "./chatAgent";

function both(): AgentSettingsResponse {
  const response = settingsWith(readyStatus);
  response.harnesses.push({ kind: "claude", status: { ...readyStatus, models: [] } });

  return response;
}

describe("chat's agent choice", () => {
  it("leaves a saved agent that is turned off for the first one ready", () => {
    const response = both();
    response.settings.harnesses.codex.disabled = true;
    response.resolved = { harness: "claude", reason: "first available" };

    expect(currentChoice(response)?.harness).toBe("claude");
    expect(agentGroups(response).map((group) => group.harness)).toEqual(["claude"]);
  });

  it("offers an agent that reported no models as itself, on its default model", () => {
    expect(agentGroups(both()).find((group) => group.harness === "claude")?.rows).toEqual([
      { harness: "claude", id: "", label: "Claude Code" },
    ]);
  });

  it("finds an agent's models by the agent's name", () => {
    expect(
      agentGroups(both(), "codex").flatMap((group) => group.rows.map((row) => row.label)),
    ).toEqual(["GPT Standard", "GPT Fast"]);
  });

  it("lists featured models and the chosen one until all are shown or a search is typed", () => {
    const response = settingsWith({
      ...readyStatus,
      models: [
        { id: "sol", displayName: "GPT-6.1-Sol", featured: true, default: true },
        { id: "old-sol", displayName: "GPT-6-Sol" },
        { id: "luna", displayName: "GPT-6-Luna" },
      ],
    });

    const labels = (query = "", all = false) =>
      agentGroups(response, query, { all, chosen: currentChoice(response) }).flatMap((group) =>
        group.rows.map((row) => row.label),
      );

    expect(hasMoreModels(response)).toBe(true);
    expect(labels()).toEqual(["GPT-6.1-Sol"]);
    expect(labels("", true)).toEqual(["GPT-6.1-Sol", "GPT-6-Sol", "GPT-6-Luna"]);
    expect(labels("luna")).toEqual(["GPT-6-Luna"]);

    response.settings.harnesses.codex.model = "luna";
    expect(labels()).toEqual(["GPT-6.1-Sol", "GPT-6-Luna"]);
  });

  it("lists every model of an agent with none featured", () => {
    expect(hasMoreModels(both())).toBe(false);
    expect(
      agentGroups(both(), "", { all: false }).flatMap((group) => group.rows.map((r) => r.label)),
    ).toEqual(["Claude Code", "GPT Standard", "GPT Fast"]);
  });

  it("blocks with nothing installed, and says so differently when every agent is off", () => {
    const missing = settingsWith({ installed: false, loggedIn: false, capabilities });
    missing.settings.harness = undefined;
    missing.resolved = { reason: "none available" };
    expect(blockerFor(missing, currentChoice(missing))).toEqual({ kind: "none-installed" });

    missing.settings.harnesses.codex.disabled = true;
    expect(blockerFor(missing, currentChoice(missing))).toEqual({ kind: "all-off" });
  });

  it("does not block on a probe that failed, which the server still runs", () => {
    const failed = settingsWith({
      ...readyStatus,
      loggedIn: false,
      lastError: "status probe timed out",
    });

    expect(blockerFor(failed, currentChoice(failed))).toBeUndefined();
  });
});
