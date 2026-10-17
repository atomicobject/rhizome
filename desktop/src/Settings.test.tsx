import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { LinkInfo } from "./api";
import { Settings } from "./Settings";

const props = () => ({
  info: null,
  link: null,
  update: null,
  busy: false,
  onRefresh: vi.fn(),
  onLink: vi.fn(),
  onCheckUpdates: vi.fn(),
  onInstallUpdate: vi.fn(),
  onSelect: vi.fn(),
  onPick: vi.fn(),
  onClose: vi.fn(),
});
const link = (state: LinkInfo["state"], shell: LinkInfo["shell"] = { isApp: false }): LinkInfo => ({
  linkPath: "/usr/local/bin/rzm",
  state,
  shell,
});

it("replaces the editable path when Browse selects another executable", () => {
  const base = props();
  const { rerender } = render(<Settings {...base} selected="/previous/rzm" />);
  fireEvent.change(screen.getByLabelText("Global executable path"), {
    target: { value: "/typed/rzm" },
  });
  rerender(<Settings {...base} selected="/chosen/rzm" />);
  expect(screen.getByLabelText("Global executable path")).toHaveValue("/chosen/rzm");
  fireEvent.click(screen.getByRole("button", { name: "Use executable" }));
  expect(base.onSelect).toHaveBeenCalledWith("/chosen/rzm");
});

it("installs the command-line tool when it is missing or points elsewhere", () => {
  for (const [state, label] of [
    ["absent", "Install Command-Line Tool"],
    ["other-bundle", "Use This App's rzm"],
    ["dangling", "Repair Command-Line Tool"],
  ] as const) {
    const base = props();
    const { unmount } = render(<Settings {...base} link={link(state)} />);
    fireEvent.click(screen.getByRole("button", { name: label }));
    expect(base.onLink).toHaveBeenCalledWith("install");
    unmount();
  }
});

it("leaves a foreign rzm alone and offers nothing in a development build", () => {
  for (const state of ["foreign", "unsupported"] as const) {
    const { unmount } = render(<Settings {...props()} link={link(state)} />);
    expect(screen.queryByRole("button", { name: /Command-Line Tool|This App/ })).toBeNull();
    unmount();
  }
});

it("names a Homebrew rzm that comes before the installed link", () => {
  const base = props();
  render(
    <Settings
      {...base}
      link={link("installed", {
        path: "/opt/homebrew/bin/rzm",
        version: "rzm version v0.50.0",
        isApp: false,
      })}
    />,
  );
  expect(screen.getByText(/brew uninstall rhizome/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Remove Command-Line Tool" }));
  expect(base.onLink).toHaveBeenCalledWith("remove");
});

it("restarts into a ready update and checks otherwise", () => {
  const base = props();
  const { rerender } = render(
    <Settings {...base} update={{ state: "current", version: "0.51.0" }} />,
  );
  expect(screen.getByRole("status")).toHaveTextContent("Rhizome 0.51.0 is up to date.");
  fireEvent.click(screen.getByRole("button", { name: "Check for Updates" }));
  expect(base.onCheckUpdates).toHaveBeenCalled();
  rerender(<Settings {...base} update={{ state: "ready", version: "0.52.0" }} />);
  fireEvent.click(screen.getByRole("button", { name: "Restart to Update" }));
  expect(base.onInstallUpdate).toHaveBeenCalled();
});
