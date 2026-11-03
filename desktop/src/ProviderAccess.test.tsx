import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { request, type CredentialStatus, type ProviderReport } from "./api";
import { ProviderAccess, providerAccessChanged } from "./ProviderAccess";
import { ProviderRuntimes, routeText, type ProviderRuntime } from "./ProviderRoutes";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);
const voyage: ProviderReport = { provider: "voyage", source: "direct", key: "saved", saved: true };
const jev: ProviderReport = { provider: "typesafe", source: "none", saved: false };
let status: CredentialStatus;
function serve() {
  call.mockImplementation(async (operation, args) => {
    if (operation === "credential-status") return status;
    if (operation === "team-status") return { signed_in: false };
    if (operation === "provider-preference-set" && "provider" in args && "preference" in args) {
      status = {
        ...status,
        providers: status.providers?.map((report) =>
          report.provider === args.provider
            ? {
                ...report,
                preference: args.preference || undefined,
                source: args.preference === "server" ? "none" : "direct",
              }
            : report,
        ),
      };
      return status;
    }
    if (operation === "provider-key-set" && "provider" in args) {
      status = {
        ...status,
        providers: status.providers?.map((report) =>
          report.provider === args.provider ? { ...report, saved: true } : report,
        ),
      };
      return status;
    }
    if (operation === "provider-key-clear" && "provider" in args) {
      status = {
        ...status,
        providers: status.providers?.map((report) =>
          report.provider === args.provider ? { ...report, saved: false } : report,
        ),
      };
      return status;
    }
    throw new Error(`Unexpected operation: ${operation}`);
  });
}
beforeEach(() => {
  vi.resetAllMocks();
  status = { available: true, ready: ["VOYAGE_API_KEY"], providers: [voyage, jev] };
  serve();
});

it("shows automatic routing without claiming a source was chosen, then writes an explicit choice", async () => {
  render(<ProviderAccess version="0.1.0" />);
  const group = await screen.findByRole("radiogroup", { name: "Voyage source" });
  expect(within(group).getByLabelText("Atomic Object server")).not.toBeChecked();
  expect(within(group).getByLabelText("Your key")).not.toBeChecked();
  expect(screen.getByText("Not chosen · Your saved key")).toBeVisible();
  expect(screen.getByText(/Reported by this app’s Rhizome 0.1.0/)).toBeVisible();
  fireEvent.click(within(group).getByLabelText("Atomic Object server"));
  await waitFor(() => expect(within(group).getByLabelText("Atomic Object server")).toBeChecked());
  expect(call).toHaveBeenCalledWith("provider-preference-set", {
    provider: "voyage",
    preference: "server",
  });
  expect(await screen.findByText("Unavailable: sign in to the Atomic Object server")).toBeVisible();
  expect(
    screen.getByText(/Choosing the server overrides saved and environment keys/),
  ).toBeVisible();
});

it("reverts an optimistic choice when saving fails", async () => {
  serve();
  const serving = call.getMockImplementation();
  call.mockImplementation(async (operation, args) => {
    if (operation === "provider-preference-set")
      throw new Error("Could not save the source. Try again.");
    return serving?.(operation, args);
  });
  render(<ProviderAccess />);
  const group = await screen.findByRole("radiogroup", { name: "Voyage source" });
  fireEvent.click(within(group).getByLabelText("Atomic Object server"));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Could not save the source. Try again.",
  );
  expect(within(group).getByLabelText("Atomic Object server")).not.toBeChecked();
});

it("focuses replacement input, clears it on Escape, and returns focus to Replace", async () => {
  const user = userEvent.setup();
  render(<ProviderAccess />);
  const replace = await screen.findByRole("button", { name: "Replace Voyage key…" });
  await user.click(replace);
  const input = screen.getByLabelText("New Voyage API key");
  expect(input).toHaveFocus();
  expect(input).toHaveAttribute("type", "password");
  await user.type(input, "synthetic-test-key");
  await user.keyboard("{Escape}");
  expect(screen.queryByLabelText("New Voyage API key")).toBeNull();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Replace Voyage key…" })).toHaveFocus(),
  );
  expect(call).not.toHaveBeenCalledWith("provider-key-set", expect.anything());
});

it("sends a new key only in the save request and removes it from the UI", async () => {
  render(<ProviderAccess />);
  fireEvent.click(await screen.findByRole("button", { name: "Add TypeSafe key…" }));
  fireEvent.change(screen.getByLabelText("New TypeSafe API key"), {
    target: { value: "synthetic-typesafe-value" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save key" }));
  expect(await screen.findByRole("button", { name: "Replace TypeSafe key…" })).toBeVisible();
  expect(call).toHaveBeenCalledWith("provider-key-set", {
    provider: "typesafe",
    key: "synthetic-typesafe-value",
  });
  expect(screen.queryByDisplayValue("synthetic-typesafe-value")).toBeNull();
  expect(document.body).not.toHaveTextContent("synthetic-typesafe-value");
});

it("asks before clearing a saved key, focusing Cancel", async () => {
  render(<ProviderAccess />);
  fireEvent.click(await screen.findByRole("button", { name: "Clear Voyage key…" }));
  expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
  expect(call).not.toHaveBeenCalledWith("provider-key-clear", expect.anything());
  fireEvent.click(screen.getByRole("button", { name: "Clear key" }));
  expect(await screen.findByRole("button", { name: "Add Voyage key…" })).toBeVisible();
  expect(call).toHaveBeenCalledWith("provider-key-clear", { provider: "voyage" });
});

it("refreshes provider reports when another window changes settings", async () => {
  render(<ProviderAccess />);
  await screen.findByText("Not chosen · Your saved key");
  status = {
    ...status,
    providers: [
      { ...voyage, preference: "server", source: "team", origin: "https://team.example.test" },
      jev,
    ],
  };
  act(() => providerAccessChanged());
  expect(await screen.findByText("https://team.example.test")).toBeVisible();
  expect(
    within(screen.getByRole("radiogroup", { name: "Voyage source" })).getByLabelText(
      "Atomic Object server",
    ),
  ).toBeChecked();
});

const runtime = (overrides: Partial<ProviderRuntime> = {}): ProviderRuntime => ({
  repository: "repo",
  name: "Project / main",
  worktree: "/synthetic/project",
  runtime: {
    state: "running",
    mode: "headless",
    version: "0.1.0",
    providerAccess: [
      {
        provider: "voyage",
        active: voyage,
        next: { ...voyage, preference: "server", source: "team" },
        restartRequired: true,
      },
      {
        provider: "typesafe",
        next: { ...jev, preference: "server", source: "team" },
        restartRequired: false,
      },
    ],
  },
  ...overrides,
});
it("distinguishes running Voyage from its next start and never invents a Jev evaluation", async () => {
  const onRestart = vi.fn().mockResolvedValue(undefined);
  render(<ProviderRuntimes repositories={[runtime()]} onRestart={onRestart} />);
  expect(screen.getByText("Active connection: Your saved key")).toBeVisible();
  expect(screen.getByText("Next start: Atomic Object server")).toBeVisible();
  expect(screen.getByText("No evaluation connection yet")).toBeVisible();
  expect(screen.getByText("Next evaluation: Atomic Object server")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Restart Project / main" }));
  await waitFor(() => expect(onRestart).toHaveBeenCalledWith("repo", "/synthetic/project"));
});
it("shows older runtimes as unknown and attached runtimes require external restart", () => {
  const onRestart = vi.fn();
  const old = runtime({
    name: "Older",
    worktree: "/older",
    runtime: { state: "running", mode: "attached" },
  });
  const attached = runtime({ runtime: { ...runtime().runtime, mode: "attached" } });
  render(
    <ProviderRuntimes
      repositories={[old, attached]}

      onRestart={onRestart}
    />,
  );
  expect(screen.getByText(/Unknown: this runtime does not report provider sources/)).toBeVisible();
  expect(screen.getByText(/Restart it where it was started/)).toBeVisible();
  expect(screen.queryByRole("button", { name: /Restart/ })).toBeNull();
});
it("names each reported key provenance without requesting a key value", () => {
  expect(routeText({ ...voyage, key: "environment", envName: "VOYAGE_API_KEY" })).toBe(
    "VOYAGE_API_KEY from the environment",
  );
  expect(routeText({ ...voyage, key: "flag" })).toBe("Key supplied on the command line");
  expect(routeText({ ...voyage, source: "bundle" })).toBe("Legacy Atomic key bundle");
  expect(routeText({ ...jev, preference: "server", reason: "session_expired" })).toBe(
    "Unavailable: sign in again",
  );
});

it("keeps saved-key restart guidance available across other-window changes and remounts", async () => {
  const item = runtime({
    runtime: {
      ...runtime().runtime,
      providerAccess: [
        { provider: "voyage", active: voyage, next: voyage, restartRequired: false },
      ],
    },
  });
  const onRestart = vi
    .fn()
    .mockRejectedValueOnce(new Error("Restart failed. Try again."))
    .mockResolvedValue(undefined);
  const { unmount } = render(<ProviderRuntimes repositories={[item]} onRestart={onRestart} />);
  fireEvent.click(screen.getByRole("button", { name: "Restart Project / main" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Restart failed. Try again.");
  fireEvent.click(screen.getByRole("button", { name: "Restart Project / main" }));
  await waitFor(() => expect(onRestart).toHaveBeenCalledTimes(2));
  expect(
    screen.getByText(
      "After replacing a saved Voyage key, restart this repository to use the new key.",
    ),
  ).toBeVisible();
  unmount();
  render(<ProviderRuntimes repositories={[item]} onRestart={onRestart} />);
  expect(screen.getByRole("button", { name: "Restart Project / main" })).toBeVisible();
});

it("reports configuration and session failures without suggesting an unrelated key or sign-in fix", () => {
  expect(routeText({ ...jev, reason: "configuration_unavailable" })).toBe(
    "Unavailable: repair your Rhizome user configuration",
  );
  expect(routeText({ ...jev, reason: "invalid_preference" })).toBe(
    "Unavailable: choose a valid provider source",
  );
  expect(routeText({ ...jev, preference: "server", reason: "session_unavailable" })).toBe(
    "Unavailable: check your server session and unlock your keychain",
  );
});
