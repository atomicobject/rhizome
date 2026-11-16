import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { request, type ChatHarness, type ChatSettingsReport } from "./api";
import { ChatSettings, chatSettingsChanged } from "./ChatSettings";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);
let report: ChatSettingsReport;
beforeEach(() => {
  vi.resetAllMocks();
  report = {
    settings: { harness: "claude", harnesses: { claude: { model: "opus" }, codex: {} } },
    resolved: { harness: "claude", reason: "configured" },
    harnesses: [
      { kind: "codex", status: { installed: true, loggedIn: false, loginHint: "codex login" } },
      { kind: "claude", status: { installed: true, loggedIn: true, version: "2.1.0" } },
    ],
  };
  call.mockImplementation(async (operation, args) => {
    if (operation === "chat-settings") return report;
    if (operation === "chat-settings-set" && "enabled" in args) {
      const { harness, enabled } = args as { harness: ChatHarness; enabled: boolean };
      report = {
        ...report,
        settings: {
          ...report.settings,
          harnesses: {
            ...report.settings.harnesses,
            [harness]: { ...report.settings.harnesses[harness], disabled: !enabled },
          },
        },
      };
      return report;
    }
    if (operation === "open-chat-install") return null;
    throw new Error(`Unexpected operation: ${operation}`);
  });
});

const row = (name: string) =>
  screen.getByText(name, { selector: ".chat-agent-name" }).closest(".chat-agent") as HTMLElement;

it("shows each agent's status, Claude Code first, with detected agents on", async () => {
  render(<ChatSettings />);
  const claude = await screen.findByRole("switch", { name: "Use Claude Code in chat" });
  expect(claude).toHaveAttribute("aria-checked", "true");
  expect(screen.getByRole("switch", { name: "Use Codex in chat" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  expect(
    [...document.querySelectorAll(".chat-agent-name")].map((name) => name.textContent),
  ).toEqual(["Claude Code", "Codex"]);
  expect(within(row("Claude Code")).getByText("Ready")).toBeVisible();
  expect(row("Codex")).toHaveTextContent(
    "Signed out. Run codex login in Terminal, then check again.",
  );
  expect(screen.queryByRole("radio")).toBeNull();
  expect(screen.queryByRole("combobox")).toBeNull();
});

it("saves an agent turned off and on", async () => {
  render(<ChatSettings />);
  const codex = await screen.findByRole("switch", { name: "Use Codex in chat" });
  fireEvent.click(codex);
  await waitFor(() => expect(codex).toHaveAttribute("aria-checked", "false"));
  expect(call).toHaveBeenCalledWith("chat-settings-set", { harness: "codex", enabled: false });

  fireEvent.click(codex);
  await waitFor(() => expect(codex).toHaveAttribute("aria-checked", "true"));
  expect(call).toHaveBeenLastCalledWith("chat-settings-set", { harness: "codex", enabled: true });
});

it("shows an agent another window turned off", async () => {
  render(<ChatSettings />);
  const codex = await screen.findByRole("switch", { name: "Use Codex in chat" });
  expect(codex).toHaveAttribute("aria-checked", "true");

  report = {
    ...report,
    settings: {
      ...report.settings,
      harnesses: { ...report.settings.harnesses, codex: { disabled: true } },
    },
  };
  act(() => chatSettingsChanged());
  await waitFor(() => expect(codex).toHaveAttribute("aria-checked", "false"));
});

it("offers the install steps for a missing agent, whose switch stays off, and shows a failed save", async () => {
  report.harnesses[0] = { kind: "codex", status: { installed: false, loggedIn: false } };
  render(<ChatSettings />);
  const codex = await screen.findByRole("switch", { name: "Use Codex in chat" });
  expect(codex).toHaveAttribute("aria-checked", "false");
  expect(codex).toBeDisabled();
  fireEvent.click(within(row("Codex")).getByRole("button", { name: "How to install" }));
  expect(call).toHaveBeenCalledWith("open-chat-install", { harness: "codex" });

  call.mockRejectedValueOnce({ code: "chat_config", message: "Cannot save chat settings." });
  fireEvent.click(screen.getByRole("switch", { name: "Use Claude Code in chat" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot save chat settings.");
});
