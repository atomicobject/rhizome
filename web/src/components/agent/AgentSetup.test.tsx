import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { queryKeys } from "../../api/queryKeys";
import type { AgentSettingsResponse } from "../../api/types";
import { deferredReply, withFakeFetch } from "../../test/fakeFetch";
import { renderWithQueryClient as render } from "../../test/renderWithQueryClient";
import { capabilities, readyStatus } from "../AgentWorkspace.test-support";
import { AgentSetup } from "./AgentSetup";

const missingCodexError =
  'harness not installed: exec: "codex": executable file not found in $PATH';

function response(): AgentSettingsResponse {
  return {
    settings: {
      harnesses: {
        codex: { permissionMode: "auto" },
        claude: { permissionMode: "auto" },
      },
    },
    resolved: { reason: "none available" },
    harnesses: [
      {
        kind: "codex",
        status: { installed: false, loggedIn: false, lastError: missingCodexError, capabilities },
      },
      {
        kind: "claude",
        status: {
          installed: true,
          loggedIn: false,
          version: "2.1.0",
          loginHint: "claude auth login",
          capabilities,
        },
      },
    ],
  };
}

function row(name: string) {
  const element = screen.getByRole("radio", { name }).closest<HTMLElement>(".agent-setup__row");

  if (!element) throw new Error(`no row for ${name}`);

  return element;
}

describe("the chat setup card", () => {
  const http = withFakeFetch();

  it("tells a missing harness from a signed-out one, without raw errors", () => {
    render(<AgentSetup response={response()} blocked />);

    const codex = row("Codex");
    expect(within(codex).getByRole("radio")).toBeDisabled();
    expect(within(codex).getByText(/Not installed/)).toBeVisible();
    expect(within(codex).getByRole("link", { name: "How to install Codex" })).toHaveAttribute(
      "href",
      "https://github.com/openai/codex",
    );
    expect(within(codex).getByText(missingCodexError)).not.toBeVisible();

    const claude = row("Claude Code");
    expect(within(claude).getByRole("radio")).toBeEnabled();
    expect(within(claude).getByText("claude auth login").tagName).toBe("CODE");
    expect(within(claude).getByRole("button", { name: "Copy" })).toBeVisible();
    expect(within(claude).getByText("2.1.0")).toBeVisible();
  });

  it("shows a ready harness with its version and account, chosen when configured", () => {
    const ready = response();
    ready.settings.harness = "codex";
    ready.harnesses[0] = { kind: "codex", status: readyStatus };
    render(<AgentSetup response={ready} blocked={false} />);

    const codex = row("Codex");
    expect(within(codex).getByRole("radio")).toBeChecked();
    expect(within(codex).getByText("Ready")).toBeVisible();
    expect(within(codex).getByText("1.2.3 · drew@example.com")).toBeVisible();
    expect(screen.getByRole("radio", { name: "Claude Code" })).not.toBeChecked();
  });

  it("saves the chosen harness and keeps the other settings", async () => {
    const saved = response();
    saved.settings.harness = "claude";
    http.json("PUT", "/api/agent/settings", saved);
    render(<AgentSetup response={response()} blocked />);

    fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }));

    await waitFor(() => expect(http.count("PUT", "/api/agent/settings")).toBe(1));
    const body = JSON.parse(http.requests("PUT", "/api/agent/settings")[0]?.body ?? "{}");
    expect(body).toEqual({ settings: { ...response().settings, harness: "claude" } });
  });

  it("checks again past the server's cached probe", async () => {
    http.json("GET", "/api/agent/settings", response());
    render(<AgentSetup response={response()} blocked />);

    fireEvent.click(screen.getByRole("button", { name: "Check again" }));

    await waitFor(() => expect(http.count("GET", "/api/agent/settings")).toBe(1));
    expect(http.requests("GET", "/api/agent/settings")[0]?.query.get("refresh")).toBe("1");
  });

  it("keeps a choice saved while an earlier check was still running", async () => {
    const both = response();
    both.harnesses = [
      { kind: "codex", status: readyStatus },
      { kind: "claude", status: readyStatus },
    ];
    const check = deferredReply<AgentSettingsResponse>();
    http.on("GET", "/api/agent/settings", () => check.promise);
    http.json("PUT", "/api/agent/settings", {
      ...both,
      settings: { ...both.settings, harness: "claude" },
    });
    const { queryClient } = render(<AgentSetup response={both} blocked={false} />);

    fireEvent.click(screen.getByRole("button", { name: "Check again" }));
    fireEvent.click(screen.getByRole("radio", { name: "Claude Code" }));
    await act(() => new Promise((resolve) => setTimeout(resolve, 20)));
    check.resolve(both);

    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(http.count("PUT", "/api/agent/settings")).toBe(1);
    expect(
      queryClient.getQueryData<AgentSettingsResponse>(queryKeys.agent.settings())?.settings.harness,
    ).toBe("claude");
  });
});
