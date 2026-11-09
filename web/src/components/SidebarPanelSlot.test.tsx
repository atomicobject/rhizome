import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { withFakeFetch } from "../test/fakeFetch";
import {
  bridgePanelEscape,
  FOLDER_PANEL_IDENTITY,
  panelIdentity,
  panelPreferenceScope,
  type SidebarPanel,
  SidebarPanelSlot,
} from "./SidebarPanelSlot";

const http = withFakeFetch();

const PANEL: SidebarPanel = { identity: "experience:kb", title: "Contents" };

const FOLDER: SidebarPanel = {
  identity: FOLDER_PANEL_IDENTITY,
  title: "Folder",
  minimisedByDefault: true,
};

// Panel content with its own state, to see whether it stays mounted.
function Counter({ subject }: { subject: string }) {
  const [count, setCount] = useState(0);

  return (
    <button type="button" onClick={() => setCount(count + 1)}>
      {subject} {count}
    </button>
  );
}

function Slot({ panel = PANEL, subject = "a" }: { panel?: SidebarPanel | null; subject?: string }) {
  return (
    <SidebarPanelSlot
      vaultKey="/vault"
      panel={panel}
      navigation={
        <button type="button" data-testid="nav">
          Navigation
        </button>
      }
    >
      <Counter subject={subject} />
    </SidebarPanelSlot>
  );
}

const region = () => screen.getByRole("region", { name: "Contents panel" });

const title = () => screen.getByRole("button", { name: "Contents" });

const coverButton = () => screen.getByRole("button", { name: "Fill the sidebar with Contents" });

const divider = () => screen.getByRole("separator", { name: "Resize Contents panel" });

// SAFETY: the slot always wraps the navigation in its own element.
const navigation = () => screen.getByTestId("nav").parentElement as HTMLElement;

// SAFETY: the panel's content renders directly inside the panel body.
const body = () =>
  screen.getByRole("button", { name: /^a /, hidden: true }).parentElement as HTMLElement;

const patches = () =>
  http
    .requests()
    .filter((request) => request.method === "PATCH")
    .map((request) => JSON.parse(request.body ?? "{}"));

describe("the sidebar panel slot", () => {
  it("shows split with a divider, minimised to its title bar, and cover over the navigation", async () => {
    render(<Slot />);

    // Split: navigation, divider, and body all present and live.
    expect(region()).toBeInTheDocument();
    expect(divider()).toHaveAttribute("aria-orientation", "horizontal");
    expect(title()).toHaveAttribute("aria-expanded", "true");
    expect(navigation()).not.toHaveAttribute("inert");
    expect(body()).toBeVisible();

    // Minimised: the body hides and goes inert; the bar stays reachable.
    fireEvent.click(title());
    await waitFor(() => expect(title()).toHaveAttribute("aria-expanded", "false"));
    expect(body()).not.toBeVisible();
    expect(body()).toHaveAttribute("inert");
    expect(screen.queryByRole("separator")).toBeNull();
    expect(coverButton()).toBeEnabled();

    // Cover from minimised opens the panel over the navigation, which goes inert.
    fireEvent.click(coverButton());
    await waitFor(() => expect(coverButton()).toHaveAttribute("aria-pressed", "true"));
    expect(body()).toBeVisible();
    expect(navigation()).not.toBeVisible();
    expect(navigation()).toHaveAttribute("inert");
    expect(screen.queryByRole("separator")).toBeNull();

    // And back to split.
    fireEvent.click(coverButton());
    expect(coverButton()).toHaveAttribute("aria-pressed", "false");
    expect(navigation()).toBeVisible();
    expect(divider()).toBeInTheDocument();
  });

  it("moves focus to the surviving control before hiding the region that holds it", async () => {
    render(<Slot />);

    screen.getByRole("button", { name: "a 0" }).focus();
    fireEvent.click(title());
    await waitFor(() => expect(title()).toHaveFocus());

    fireEvent.click(title());
    await waitFor(() => expect(body()).toBeVisible());
    screen.getByTestId("nav").focus();
    fireEvent.click(coverButton());
    expect(coverButton()).toHaveFocus();
  });

  it("returns from cover to split on Escape, after a child menu has had the chance to consume it", () => {
    render(<Slot />);
    fireEvent.click(coverButton());
    const inner = screen.getByRole("button", { name: "a 0" });

    inner.addEventListener("keydown", (event) => event.preventDefault(), { once: true });
    fireEvent.keyDown(inner, { key: "Escape" });
    expect(coverButton()).toHaveAttribute("aria-pressed", "true");

    fireEvent.keyDown(inner, { key: "Escape" });
    expect(coverButton()).toHaveAttribute("aria-pressed", "false");
    expect(navigation()).toBeVisible();
  });

  it("returns from cover on an Escape its panel frame left unconsumed", () => {
    render(<Slot panel={{ ...PANEL, owns: "panel-frame-kb" }} />);
    const frame = document.createElement("iframe");
    frame.id = "panel-frame-kb";
    document.body.append(frame);
    bridgePanelEscape(frame);
    fireEvent.click(coverButton());
    const page = frame.contentDocument!.body;

    const escape = () =>
      act(() => {
        page.dispatchEvent(
          new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
        );
      });

    // A menu in the panel's page consumes the first.
    page.addEventListener("keydown", (event) => event.preventDefault(), { once: true });
    escape();
    expect(coverButton()).toHaveAttribute("aria-pressed", "true");

    escape();
    expect(coverButton()).toHaveAttribute("aria-pressed", "false");
    frame.remove();
  });

  it("resizes from the keyboard and remembers height and minimised state per vault and panel identity", async () => {
    const first = render(<Slot />);

    fireEvent.keyDown(divider(), { key: "ArrowUp" });
    await waitFor(() => expect(divider()).toHaveAttribute("aria-valuenow", "296"));
    expect(region().style.getPropertyValue("--panel-height")).toBe("296px");
    fireEvent.keyDown(divider(), { key: "ArrowDown" });
    fireEvent.keyDown(divider(), { key: "ArrowDown" });
    await waitFor(() => expect(divider()).toHaveAttribute("aria-valuenow", "264"));
    fireEvent.click(title());
    await waitFor(() => expect(title()).toHaveAttribute("aria-expanded", "false"));

    await waitFor(() =>
      expect(patches().at(-1)).toMatchObject({
        scope: { viewId: "experience:kb", context: { kind: "standalone" }, slot: "rhizome-panel" },
        set: { minimised: true },
      }),
    );
    expect(patches().some((patch) => patch.set?.height === 264)).toBe(true);

    first.unmount();
    render(<Slot />);
    await waitFor(() => expect(title()).toHaveAttribute("aria-expanded", "false"));
  });

  it("keys a panel by its experience or by builtin:folder, in a slot apart from the panel page's preferences", () => {
    expect(panelIdentity({ kind: "view", viewId: "contents", experienceKey: "kb" })).toBe(
      "experience:kb",
    );
    expect(panelIdentity({ kind: "folder" })).toBe("builtin:folder");
    expect(panelIdentity({ kind: "none" })).toBeNull();
    // web/kit/preferences.ts keeps a panel page's own values at this view ID with no slot.
    expect(panelPreferenceScope("experience:kb").slot).toBe("rhizome-panel");
  });

  it("starts Folder minimised until someone opens it, then remembers that", async () => {
    const first = render(<Slot panel={FOLDER} />);
    const folderTitle = () => screen.getByRole("button", { name: "Folder" });

    await waitFor(() => expect(folderTitle()).toHaveAttribute("aria-expanded", "false"));
    fireEvent.click(folderTitle());
    await waitFor(() => expect(folderTitle()).toHaveAttribute("aria-expanded", "true"));
    await waitFor(() =>
      expect(patches().at(-1)).toMatchObject({
        scope: { viewId: "builtin:folder", slot: "rhizome-panel" },
        set: { minimised: false },
      }),
    );
    first.unmount();

    render(<Slot panel={FOLDER} />);
    await waitFor(() => expect(folderTitle()).toHaveAttribute("aria-expanded", "true"));
  });

  it("keeps the panel's component mounted across subjects and states", async () => {
    const view = render(<Slot subject="a" />);

    fireEvent.click(screen.getByRole("button", { name: "a 0" }));
    view.rerender(<Slot subject="b" />);
    expect(screen.getByRole("button", { name: "b 1" })).toBeInTheDocument();

    fireEvent.click(title());
    await waitFor(() => expect(title()).toHaveAttribute("aria-expanded", "false"));
    fireEvent.click(title());
    await waitFor(() => expect(title()).toHaveAttribute("aria-expanded", "true"));
    fireEvent.click(coverButton());
    expect(screen.getByRole("button", { name: "b 1" })).toBeInTheDocument();
  });

  it("keeps the navigation mounted when the panel comes and goes", () => {
    const view = render(<Slot panel={null} />);
    const nav = screen.getByTestId("nav");

    expect(screen.queryByRole("region")).toBeNull();
    view.rerender(<Slot />);
    expect(screen.getByTestId("nav")).toBe(nav);
    view.rerender(<Slot panel={null} />);
    expect(screen.getByTestId("nav")).toBe(nav);
  });
});
