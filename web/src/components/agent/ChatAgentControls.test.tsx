import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { AgentHarnessKind, AgentSettings, AgentSettingsResponse } from "../../api/types";
import { decodeJSONBody, jsonReply, withFakeFetch } from "../../test/fakeFetch";
import { renderWithQueryClient as render } from "../../test/renderWithQueryClient";
import { capabilities } from "../AgentWorkspace.test-support";
import { ChatAgentControls, resetChatAgentForTests, useChatAgent } from "./ChatAgentControls";
import { ChatAgentNotice } from "./ChatAgentNotice";

const allModes = {
  ...capabilities,
  permissionModes: ["auto", "approval-required", "auto-accept-edits", "full-access"] as const,
};

/** Claude Code ready and chosen; Codex installed but signed out. */
function twoAgents(): AgentSettingsResponse {
  return {
    settings: {
      harness: "claude",
      harnesses: {
        claude: { model: "opus", effort: "high", permissionMode: "auto" },
        codex: { permissionMode: "approval-required" },
      },
    },
    resolved: { harness: "claude", model: "opus", reason: "configured" },
    harnesses: [
      {
        kind: "codex",
        status: {
          installed: true,
          loggedIn: false,
          loginHint: "codex login",
          capabilities: { ...allModes, permissionModes: [...allModes.permissionModes] },
          models: [
            {
              id: "sol",
              displayName: "GPT Sol",
              efforts: ["low", "medium"],
              defaultEffort: "medium",
              default: true,
            },
            { id: "terra", displayName: "GPT Terra", efforts: ["low"], defaultEffort: "low" },
          ],
        },
      },
      {
        kind: "claude",
        status: {
          installed: true,
          loggedIn: true,
          capabilities: { ...allModes, permissionModes: [...allModes.permissionModes] },
          models: [
            { id: "opus", displayName: "Opus 5.5", efforts: ["low", "high", "max"], default: true },
            { id: "sonnet", displayName: "Sonnet 5.5", efforts: ["low", "high"] },
          ],
        },
      },
    ],
  };
}

const isSettingsBody = (value: unknown): value is { settings: AgentSettings } =>
  typeof value === "object" && value !== null && "settings" in value;

function Composer({ session }: { session?: { harness?: AgentHarnessKind } }) {
  const agent = useChatAgent(session);

  return (
    <div className="agent-composer__frame">
      <ChatAgentNotice agent={agent} />
      <ChatAgentControls agent={agent} />
      <button type="button" disabled={Boolean(agent.blocker)}>
        Send
      </button>
    </div>
  );
}

describe("chat's agent picker", () => {
  const http = withFakeFetch();
  let response: AgentSettingsResponse;

  beforeEach(() => {
    resetChatAgentForTests();
    response = twoAgents();
    http
      .on("GET", "/api/agent/settings", () => jsonReply(response))
      .on("PUT", "/api/agent/settings", (request) => {
        response = { ...response, settings: decodeJSONBody(request, isSettingsBody).settings };

        return jsonReply(response);
      });
  });

  const saved = () =>
    decodeJSONBody(http.requests("PUT", "/api/agent/settings").at(-1)!, isSettingsBody).settings;

  const openPicker = async () => {
    fireEvent.click(await screen.findByRole("button", { name: "Opus 5.5 · High" }));

    return screen.getByRole("dialog", { name: "Choose a model" });
  };

  it("groups each agent's models, Claude Code first, with a faint note for a signed-out one", async () => {
    render(<Composer />);
    const picker = await openPicker();

    const groups = within(picker).getAllByRole("group");
    expect(groups.map((group) => group.getAttribute("aria-label"))).toEqual([
      "Claude Code",
      "Codex",
    ]);
    expect(groups[1]).toHaveTextContent(/^Codex· signed out/);
    expect(
      within(picker)
        .getAllByRole("option")
        .map((option) => option.textContent),
    ).toEqual(["✓Opus 5.5Ctrl+1", "Sonnet 5.5Ctrl+2", "GPT SolCtrl+3", "GPT TerraCtrl+4"]);
    expect(within(picker).getByRole("option", { name: /Opus 5.5/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // Claude Code names no default effort, so "Default" leaves it to the agent.
    const reasoning = within(picker).getByRole("radiogroup", { name: "Reasoning" });
    expect(
      within(reasoning)
        .getAllByRole("radio")
        .map((radio) => radio.textContent),
    ).toEqual(["Default", "Low", "High✓", "Max"]);
    expect(within(picker).getByRole("combobox", { name: "Search models" })).toHaveFocus();
  });

  it("chooses with ↑/↓ and Enter, keeping an effort the new model offers", async () => {
    render(<Composer />);
    const picker = await openPicker();
    const search = within(picker).getByRole("combobox", { name: "Search models" });

    fireEvent.keyDown(search, { key: "ArrowDown" });
    fireEvent.keyDown(search, { key: "Enter" });

    await waitFor(() => expect(http.count("PUT", "/api/agent/settings")).toBe(1));
    expect(saved().harness).toBe("claude");
    expect(saved().harnesses.claude).toMatchObject({ model: "sonnet", effort: "high" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(await screen.findByRole("button", { name: "Sonnet 5.5 · High" })).toBeVisible();
  });

  it("searches and chooses with ⌘ and a digit; a signed-out agent's model shows the notice and holds Send", async () => {
    render(<Composer />);
    const picker = await openPicker();
    const search = within(picker).getByRole("combobox", { name: "Search models" });

    fireEvent.change(search, { target: { value: "terra" } });
    expect(
      within(picker)
        .getAllByRole("option")
        .map((option) => option.textContent),
    ).toEqual(["GPT TerraCtrl+1"]);
    fireEvent.keyDown(search, { key: "1", ctrlKey: true });

    await waitFor(() => expect(http.count("PUT", "/api/agent/settings")).toBe(1));
    expect(saved()).toMatchObject({
      harness: "codex",
      harnesses: {
        codex: { model: "terra", effort: "", permissionMode: "approval-required" },
        claude: { model: "opus", effort: "high" },
      },
    });

    const notice = await screen.findByText(/Codex is signed out/);
    expect(notice).toHaveTextContent("Codex is signed out. Run codex login in Terminal.");
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "GPT Terra · Low" })).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Use Opus 5.5 instead" }));
    await waitFor(() => expect(http.count("PUT", "/api/agent/settings")).toBe(2));
    expect(saved()).toMatchObject({
      harness: "claude",
      harnesses: { claude: { model: "opus", effort: "high" } },
    });
    await waitFor(() => expect(screen.queryByText(/signed out/)).toBeNull());
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
  });

  it("opens on Reasoning with ⇧⌘E and saves the effort chosen there", async () => {
    render(<Composer />);
    await screen.findByRole("button", { name: "Opus 5.5 · High" });

    act(() => {
      fireEvent.keyDown(window, { key: "E", code: "KeyE", shiftKey: true, ctrlKey: true });
    });

    const high = within(screen.getByRole("radiogroup", { name: "Reasoning" })).getByRole("radio", {
      name: "High",
    });

    expect(high).toHaveFocus();
    fireEvent.keyDown(high, { key: "ArrowDown" });
    const max = screen.getByRole("radio", { name: "Max" });
    expect(max).toHaveFocus();
    fireEvent.click(max);

    await waitFor(() => expect(http.count("PUT", "/api/agent/settings")).toBe(1));
    expect(saved().harnesses.claude).toMatchObject({ model: "opus", effort: "max" });
    expect(await screen.findByRole("button", { name: "Opus 5.5 · Max" })).toBeVisible();

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("marks the agent's own default effort when it names one", async () => {
    response.settings.harness = "codex";
    response.settings.harnesses.codex.effort = "low";
    response.harnesses[0].status.loggedIn = true;
    render(<Composer />);
    fireEvent.click(await screen.findByRole("button", { name: "GPT Sol · Low" }));

    const reasoning = screen.getByRole("radiogroup", { name: "Reasoning" });
    expect(
      within(reasoning)
        .getAllByRole("radio")
        .map((radio) => radio.textContent),
    ).toEqual(["Low✓", "MediumDefault"]);
  });

  it("offers the permission modes on ⇧⌘A, in order, and saves the one chosen", async () => {
    render(<Composer />);
    await screen.findByRole("button", { name: "Auto" });

    act(() => {
      fireEvent.keyDown(window, { key: "A", code: "KeyA", shiftKey: true, ctrlKey: true });
    });
    const menu = screen.getByRole("menu", { name: "Permission mode" });
    const items = within(menu).getAllByRole("menuitemradio");
    expect(items.map((item) => item.querySelector(".chat-mode-menu__label")?.textContent)).toEqual([
      "Ask first",
      "Auto",
      "Accept edits",
      "Full access",
    ]);
    expect(items[1]).toHaveFocus();
    expect(items[1]).toHaveAttribute("aria-checked", "true");
    fireEvent.click(items[0]);

    await waitFor(() => expect(http.count("PUT", "/api/agent/settings")).toBe(1));
    expect(saved().harnesses.claude).toMatchObject({
      model: "opus",
      permissionMode: "approval-required",
    });
    expect(await screen.findByRole("button", { name: "Ask first" })).toBeVisible();
  });

  it("hides an agent turned off in Settings", async () => {
    response.settings.harnesses.codex.disabled = true;
    render(<Composer />);
    const picker = await openPicker();

    expect(
      within(picker)
        .getAllByRole("group")
        .map((group) => group.getAttribute("aria-label")),
    ).toEqual(["Claude Code"]);
    expect(within(picker).queryByText("GPT Sol")).toBeNull();
  });

  it("says the next message starts a new chat when the open one runs another agent", async () => {
    render(<Composer session={{ harness: "codex" }} />);

    expect(await screen.findByText("Sends in a new chat with Claude Code")).toBeVisible();
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
  });
});
