import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { request, type ChatHarness, type ChatSettingsReport } from "./api";
import { ChatSettings } from "./ChatSettings";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);
let report: ChatSettingsReport;
beforeEach(() => {
  vi.resetAllMocks();
  report = {
    settings: { harness: "claude", harnesses: { claude: { model: "retired-model" }, codex: {} } },
    resolved: { harness: "claude", reason: "configured" },
    harnesses: [
      { kind: "codex", status: { installed: true, loggedIn: false, loginHint: "codex login" } },
      {
        kind: "claude",
        status: {
          installed: true,
          loggedIn: true,
          version: "2.1.0",
          models: [
            { id: "opus", displayName: "Opus" },
            { id: "sonnet", displayName: "Sonnet", default: true },
          ],
        },
      },
    ],
  };
  call.mockImplementation(async (operation, args) => {
    if (operation === "chat-settings") return report;
    if (operation === "chat-settings-set" && "harness" in args) {
      const { harness, model } = args as { harness: ChatHarness; model?: string };
      const saved = model ?? report.settings.harnesses[harness]?.model;
      report = {
        ...report,
        settings: {
          harness,
          harnesses: { ...report.settings.harnesses, [harness]: { model: saved } },
        },
      };
      return report;
    }
    if (operation === "open-chat-install") return null;
    throw new Error(`Unexpected operation: ${operation}`);
  });
});

const row = (name: string) => screen.getByLabelText(name).closest(".chat-agent") as HTMLElement;

it("shows each agent's status and saves the chosen agent's model", async () => {
  render(<ChatSettings />);
  const model = await screen.findByRole("combobox", { name: "Model" });
  expect(within(row("Claude Code")).getByText("Ready · 2.1.0")).toBeVisible();
  expect(model).toHaveValue("");
  expect(within(model).getByRole("option", { name: "Default (Sonnet)" })).toBeInTheDocument();

  fireEvent.change(model, { target: { value: "opus" } });
  await waitFor(() => expect(model).toHaveValue("opus"));
  expect(call).toHaveBeenCalledWith("chat-settings-set", { harness: "claude", model: "opus" });
});

it("folds Claude Code's own default model into the Default choice", async () => {
  report.settings.harnesses.claude = { model: "default" };
  report.harnesses[1].status.models = [
    { id: "default", displayName: "Default (recommended)" },
    { id: "opus", displayName: "Opus" },
  ];
  render(<ChatSettings />);
  const model = await screen.findByRole("combobox", { name: "Model" });
  expect(model).toHaveValue("");
  expect(
    within(model)
      .getAllByRole("option")
      .map((option) => option.textContent),
  ).toEqual(["Default (recommended)", "Opus"]);
});

it("copies a signed-out agent's login command and switches agents without a model", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
  render(<ChatSettings />);
  await screen.findByRole("combobox", { name: "Model" });
  const codex = row("Codex");
  expect(codex).toHaveTextContent("Signed out. Run codex login in a terminal.");
  fireEvent.click(within(codex).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("codex login"));

  fireEvent.click(screen.getByLabelText("Codex"));
  await waitFor(() => expect(screen.getByLabelText("Codex")).toBeChecked());
  expect(call).toHaveBeenCalledWith("chat-settings-set", { harness: "codex" });
  expect(screen.queryByRole("combobox", { name: "Model" })).toBeNull();
});

it("offers an install guide for a missing agent and shows a failed save", async () => {
  report.harnesses[0] = { kind: "codex", status: { installed: false, loggedIn: false } };
  render(<ChatSettings />);
  await screen.findByRole("combobox", { name: "Model" });
  expect(screen.getByLabelText("Codex")).toBeDisabled();
  fireEvent.click(within(row("Codex")).getByRole("button", { name: "Install Guide" }));
  expect(call).toHaveBeenCalledWith("open-chat-install", { harness: "codex" });

  call.mockRejectedValueOnce({ code: "chat_config", message: "Cannot save chat settings." });
  fireEvent.change(screen.getByRole("combobox", { name: "Model" }), { target: { value: "opus" } });
  expect(await screen.findByRole("alert")).toHaveTextContent("Cannot save chat settings.");
});
