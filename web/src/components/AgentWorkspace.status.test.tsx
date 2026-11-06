import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { AgentHarnessStatus } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { AgentWorkspace } from "./AgentWorkspace";
import { capabilities, readyStatus, setup } from "./AgentWorkspace.test-support";

describe("AgentWorkspace harness status", () => {
  const http = withFakeFetch();

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
    ["ready", readyStatus, "Codex · GPT Standard · Ready"],
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

    // The version and account sit behind Details, not on the state line.
    if (status.account) {
      expect(screen.getByText(status.account)).not.toBeVisible();
      expect(screen.getByText("Details")).toBeVisible();
    }

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
});
