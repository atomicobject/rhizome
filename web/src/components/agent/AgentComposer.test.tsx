import { createRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentComposer } from "./AgentComposer";

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
  disabledReason: "",
  harness: "claude" as const,
  status: undefined,
};

afterEach(() => vi.restoreAllMocks());

describe("AgentComposer", () => {
  it.each([
    ["MacIntel", "⌘"],
    ["Linux x86_64", "Ctrl"],
  ])("shows the %s shortcut in keycaps", (platform, modifier) => {
    vi.spyOn(navigator, "platform", "get").mockReturnValue(platform);
    render(<AgentComposer {...props} />);
    expect(screen.getByText(modifier).tagName).toBe("KBD");
    expect(screen.getByText("↵").tagName).toBe("KBD");
    expect(screen.getByText(/to send/)).toBeVisible();
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
