import { createRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { settingsWith, readyStatus } from "../AgentWorkspace.test-support";
import { AgentComposer } from "./AgentComposer";
import type { ChatAgent } from "./ChatAgentControls";
import { currentChoice } from "./chatAgent";

const response = settingsWith(readyStatus);

const agent: ChatAgent = {
  response,
  choice: currentChoice(response),
  blocker: undefined,
  readError: null,
  error: null,
  checking: false,
  fallback: undefined,
  newChat: false,
  choose: vi.fn(),
  recheck: vi.fn(),
};

const props = {
  runningID: "",
  onError: vi.fn(),
  draft: "",
  onChange: vi.fn(),
  onSubmit: vi.fn(),
  composerRef: createRef<HTMLTextAreaElement>(),
  busy: false,
  blocked: false,
  readOnly: false,
  agent,
};

afterEach(() => vi.restoreAllMocks());

describe("AgentComposer", () => {
  it("names the chosen model and effort beside Send", () => {
    render(<AgentComposer {...props} />);
    expect(screen.getByRole("button", { name: "GPT Standard · Medium" })).toHaveAttribute(
      "title",
      expect.stringContaining("Codex · GPT Standard · Medium"),
    );
    expect(screen.getByRole("button", { name: "Ask first" })).toBeVisible();
  });

  it("uses the standard disabled button until a message can be sent", () => {
    const { rerender } = render(<AgentComposer {...props} />);
    const send = screen.getByRole("button", { name: "Send" });
    expect(send).toBeDisabled();
    expect(send).not.toHaveClass("button--primary");
    rerender(<AgentComposer {...props} draft="Explain this" />);
    expect(send).toBeEnabled();
    expect(send).toHaveClass("button--primary");
    fireEvent.click(send);
    expect(props.onSubmit).toHaveBeenCalledWith("Explain this");
    rerender(<AgentComposer {...props} draft="Explain this" busy />);
    expect(send).toBeDisabled();
    expect(send).not.toHaveClass("button--primary");
  });
});
