import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { request, type LinkInfo } from "./api";
import { Settings } from "./Settings";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);
beforeEach(() => {
  call.mockReset();
  call.mockResolvedValue({ enabled: false, always: false });
});

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
  appearance: { theme: "graphite", appearance: "auto" } as const,
  onAppearance: vi.fn(),
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

it("records render timings in new windows once turned on", async () => {
  call.mockImplementation(async (operation, args) =>
    operation === "set-render-profiling"
      ? { enabled: (args as { enabled: boolean }).enabled, always: false }
      : { enabled: false, always: false },
  );
  render(<Settings {...props()} />);
  const box = screen.getByRole("checkbox", { name: "Record render timings" });
  await waitFor(() => expect(box).toBeEnabled());
  expect(box).not.toBeChecked();
  expect(screen.getByText("Applies to windows opened after you change it.")).toBeVisible();
  fireEvent.click(box);
  await waitFor(() => expect(box).toBeChecked());
  expect(call).toHaveBeenCalledWith("set-render-profiling", { enabled: true });
});

it("keeps render timings on in Rhizome Dev", async () => {
  call.mockResolvedValue({ enabled: true, always: true });
  render(<Settings {...props()} />);
  expect(await screen.findByText("Always on in Rhizome Dev.")).toBeVisible();
  const box = screen.getByRole("checkbox", { name: "Record render timings" });
  expect(box).toBeChecked();
  expect(box).toBeDisabled();
});

vi.mock("./CredentialImport", () => ({ CredentialImport: () => null }));

it("shows the signed-in team account with its sign-out action", async () => {
  call.mockImplementation(async (operation) =>
    operation === "team-status"
      ? {
          signed_in: true,
          email: "ada@example.test",
          origin: "https://team.example.test",
          session_expires_at: "2026-11-07T12:00:00Z",
        }
      : { enabled: false, always: false },
  );
  render(<Settings {...props()} />);
  expect(await screen.findByText("ada@example.test")).toBeVisible();
  expect(screen.getByRole("heading", { name: "Team account" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Sign out" })).toBeEnabled();
});

it("chooses the app-wide appearance and theme", () => {
  const base = props();
  render(<Settings {...base} />);
  const appearance = screen.getByRole("radiogroup", { name: "Appearance" });
  expect(within(appearance).getByLabelText("Match System")).toBeChecked();
  fireEvent.click(within(appearance).getByLabelText("Dark"));
  expect(base.onAppearance).toHaveBeenCalledWith({ appearance: "dark" });
  const themes = screen.getByRole("radiogroup", { name: "Theme" });
  expect(within(themes).getAllByRole("radio")).toHaveLength(5);
  expect(within(themes).getByLabelText("Graphite")).toBeChecked();
  fireEvent.click(within(themes).getByLabelText("Spruce"));
  expect(base.onAppearance).toHaveBeenCalledWith({ theme: "spruce" });
});
