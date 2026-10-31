import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { request, type TeamStatus } from "./api";
import { TeamAccount, teamProgress } from "./TeamAccount";

/** The sign-in button once the status request has settled and enabled it. */
async function signInButton() {
  const button = await screen.findByRole("button", { name: "Sign in with Atomic Object" });
  await waitFor(() => expect(button).toBeEnabled());
  return button;
}

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  request: vi.fn(),
}));
const call = vi.mocked(request);
const origin = "https://team.example.test";
const signedIn: TeamStatus = {
  signed_in: true,
  email: "ada@example.test",
  origin,
  session_expires_at: "2026-11-07T12:00:00Z",
};
beforeEach(() => vi.resetAllMocks());

function serve(handlers: Record<string, () => unknown>) {
  call.mockImplementation(async (operation) => {
    const handle = handlers[operation];
    if (!handle) throw new Error(`Unexpected operation: ${operation}`);
    return handle() as never;
  });
}

it("signs in through the browser, shows progress, and then the account", async () => {
  let finish: (value: TeamStatus) => void = () => {};
  serve({
    "team-status": () => ({ signed_in: false, origin }),
    "team-sign-in": () => new Promise((resolve) => (finish = resolve)),
  });
  const onChange = vi.fn();
  render(<TeamAccount onChange={onChange} />);
  expect(await screen.findByText(/without storing provider keys on this Mac/)).toBeVisible();
  expect(screen.queryByText(/import your own keys/)).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Sign in with Atomic Object" }));
  expect(await screen.findByText("Waiting for the browser…")).toBeVisible();
  act(() => teamProgress("https://accounts.example.test/auth"));
  expect(screen.getByText("https://accounts.example.test/auth")).toBeVisible();
  await act(async () => finish(signedIn));
  expect(screen.getByText("ada@example.test")).toBeVisible();
  expect(screen.getByText(/Session expires/)).toBeVisible();
  expect(screen.getByRole("button", { name: "Sign out" })).toBeEnabled();
  expect(onChange).toHaveBeenCalledOnce();
});

it("cancels a sign-in without showing an error", async () => {
  let reject: (cause: unknown) => void = () => {};
  serve({
    "team-status": () => ({ signed_in: false, origin }),
    "team-sign-in": () => new Promise((_resolve, fail) => (reject = fail)),
    "team-cancel": () => {
      reject({ code: "canceled", message: "Sign-in canceled." });
      return null;
    },
  });
  render(<TeamAccount />);
  fireEvent.click(await signInButton());
  fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
  expect(await screen.findByText("Sign-in canceled.")).toBeVisible();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("button", { name: "Sign in with Atomic Object" })).toBeEnabled();
});

it("shows sign-in errors inline and allows retry", async () => {
  serve({
    "team-status": () => ({ signed_in: false, origin }),
    "team-sign-in": () => {
      throw { code: "not_eligible", message: "Your account is not eligible for team access." };
    },
  });
  render(<TeamAccount />);
  fireEvent.click(await signInButton());
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Your account is not eligible for team access.",
  );
  expect(screen.getByRole("button", { name: "Sign in with Atomic Object" })).toBeEnabled();
});

it("signs out and says when the server session could not be revoked", async () => {
  serve({
    "team-status": () => signedIn,
    "team-sign-out": () => ({ signed_out: true, remote_revoked: false }),
  });
  render(<TeamAccount />);
  fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
  expect(await screen.findByRole("button", { name: "Sign in with Atomic Object" })).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent("could not reach the team server");
});

it("cancels a running sign-in when it unmounts", async () => {
  serve({
    "team-status": () => ({ signed_in: false, origin }),
    "team-sign-in": () => new Promise(() => {}),
    "team-cancel": () => null,
  });
  const { unmount } = render(<TeamAccount />);
  fireEvent.click(await signInButton());
  await screen.findByText("Waiting for the browser…");
  unmount();
  expect(call).toHaveBeenCalledWith("team-cancel", {});
});
