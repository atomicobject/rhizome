import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { App } from "./App";
import { request } from "./api";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  attach: vi.fn(async () => ({})),
  request: vi.fn(async (op: string) => {
    if (op === "list") return { version: 3, repositories: [], revision: 1 };
    if (op === "settings") return { theme: "graphite", appearance: "auto" };
    if (op === "update-status") return { state: "idle" };
    if (op === "cli-link-status") return { state: "absent", shell: {} };
    return null;
  }),
}));
afterEach(() => {
  delete window.__RHIZOME_PLATFORM__;
  vi.clearAllMocks();
});

it("reasserts native layout on resize even when the DOM rectangle is unchanged", async () => {
  render(<App />);
  await screen.findByRole("button", { name: "Add Repository…" });
  const layouts = () => vi.mocked(request).mock.calls.filter(([op]) => op === "layout");
  await waitFor(() => expect(layouts().length).toBeGreaterThan(0));
  const before = layouts().length;
  fireEvent(window, new Event("resize"));
  await waitFor(() => expect(layouts().length).toBeGreaterThan(before));
});

it("opens the native application menu from the Linux toolbar", async () => {
  window.__RHIZOME_PLATFORM__ = "linux";
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Application menu" }));
  expect(request).toHaveBeenCalledWith("application-menu", { x: 0, y: 0 });
});
