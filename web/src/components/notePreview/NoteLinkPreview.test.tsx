import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NodePreview } from "../../api/types";
import { deferredReply, jsonReply, withFakeFetch } from "../../test/fakeFetch";
import { NoteLinkPreview, previewPosition } from "./NoteLinkPreview";

const preview: NodePreview = {
  ref: "notes/target.md",
  path: "notes/target.md",
  title: "Target note",
  typeLabel: "Note",
  format: "markdown",
  fragmentResolved: true,
  fields: [],
  hasIssues: false,
};

function setHover(enabled: boolean) {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: vi.fn().mockReturnValue({ matches: enabled }),
  });
}

function renderLink(open = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Wrapper({ children }: PropsWithChildren) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  return {
    open,
    ...render(
      <NoteLinkPreview target="notes/target.md" from="notes/source.md" open={open}>
        Target
      </NoteLinkPreview>,
      { wrapper: Wrapper },
    ),
  };
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe("NoteLinkPreview", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
    setHover(true);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not request a preview on a fast pass", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });
    fireEvent.mouseEnter(link, { clientY: 0 });
    await advance(99);
    fireEvent.mouseLeave(link);
    await advance(200);

    expect(http.count("GET", "/api/v1/nodes/preview")).toBe(0);
  });

  it("requests once at the 100ms dwell", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    fireEvent.mouseEnter(screen.getByRole("link", { name: "Target" }), { clientY: 0 });
    await advance(99);
    expect(http.count("GET", "/api/v1/nodes/preview")).toBe(0);
    await advance(1);
    expect(http.count("GET", "/api/v1/nodes/preview")).toBe(1);
  });

  it("opens at 300ms with ref and from in the request", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    fireEvent.mouseEnter(screen.getByRole("link", { name: "Target" }), { clientY: 0 });
    await advance(299);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    await advance(1);

    expect(screen.getByRole("region", { name: "Preview of Target note" })).toBeInTheDocument();
    const request = http.requests("GET", "/api/v1/nodes/preview")[0];
    expect(request?.query.get("ref")).toBe("notes/target.md");
    expect(request?.query.get("from")).toBe("notes/source.md");
  });

  it("shows loading at the hard cap while data is pending", async () => {
    const pending = deferredReply<NodePreview>();
    http.on("GET", "/api/v1/nodes/preview", () => pending.promise);
    renderLink();
    fireEvent.mouseEnter(screen.getByRole("link", { name: "Target" }), { clientY: 0 });
    await advance(699);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    await advance(1);

    expect(screen.getByText("Loading preview…")).toBeInTheDocument();
    await act(async () => pending.resolve(preview));
    await advance(0);
  });

  it("opens as soon as delayed data settles after the normal open delay", async () => {
    const pending = deferredReply<NodePreview>();
    http.on("GET", "/api/v1/nodes/preview", () => pending.promise);
    renderLink();
    fireEvent.mouseEnter(screen.getByRole("link", { name: "Target" }), { clientY: 0 });
    await advance(500);
    expect(http.count("GET", "/api/v1/nodes/preview")).toBe(1);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    await act(async () => pending.resolve(preview));
    await advance(1);
    expect(screen.getByRole("region", { name: "Preview of Target note" })).toBeInTheDocument();
  });

  it("keeps the card while the pointer enters it during close grace", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });
    fireEvent.mouseEnter(link, { clientY: 0 });
    await advance(100);
    await advance(200);
    const card = screen.getByRole("region", { name: "Preview of Target note" });
    fireEvent.mouseLeave(link);
    await advance(149);
    fireEvent.mouseEnter(card);
    await advance(151);
    expect(card).toBeInTheDocument();
    fireEvent.mouseLeave(card);
    await advance(149);
    expect(card).toBeInTheDocument();
    await advance(1);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });

  it("closes on click and preserves stack and beside modes", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    const first = renderLink();
    const link = screen.getByRole("link", { name: "Target" });
    fireEvent.mouseEnter(link, { clientY: 0 });
    await advance(301);
    await advance(0);
    fireEvent.click(link);
    expect(first.open).toHaveBeenCalledWith("notes/target.md", "stack");
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    first.unmount();

    const second = renderLink();
    fireEvent.click(screen.getByRole("link", { name: "Target" }), { metaKey: true });
    expect(second.open).toHaveBeenCalledWith("notes/target.md", "beside");
  });

  it("opens from focus and keeps the card when focus moves into it", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });
    act(() => link.focus());
    await advance(100);
    await advance(200);
    const card = screen.getByRole("region");
    fireEvent.blur(link, { relatedTarget: card });
    await advance(150);
    expect(card).toBeInTheDocument();
    fireEvent.blur(link);
    await advance(150);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });

  it("keeps a pending preview when the pointer leaves a focused trigger", async () => {
    const pending = deferredReply<NodePreview>();
    http.on("GET", "/api/v1/nodes/preview", () => pending.promise);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });

    fireEvent.mouseEnter(link, { clientY: 0 });
    act(() => link.focus());
    await advance(100);
    fireEvent.mouseLeave(link);
    await act(async () => pending.resolve(preview));
    await advance(200);

    expect(screen.getByRole("region", { name: "Preview of Target note" })).toBeInTheDocument();
    fireEvent.blur(link);
    await advance(150);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });

  it("keeps a hovered preview when keyboard focus leaves the trigger", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });

    fireEvent.mouseEnter(link, { clientY: 0 });
    act(() => link.focus());
    await advance(100);
    await advance(200);
    fireEvent.blur(link);
    await advance(150);

    expect(screen.getByRole("region", { name: "Preview of Target note" })).toBeInTheDocument();
    fireEvent.mouseLeave(link);
    await advance(150);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });

  it("returns focus to the trigger when Escape closes a keyboard-focused preview", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });
    act(() => link.focus());
    await advance(100);
    await advance(200);
    const card = screen.getByRole("region", { name: "Preview of Target note" });
    fireEvent.keyDown(link, { key: "Tab" });
    expect(card).toHaveFocus();

    fireEvent.keyDown(window, { key: "Escape" });

    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    expect(link).toHaveFocus();
  });

  it("does not move focus when Escape closes a pointer preview", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });
    fireEvent.mouseEnter(link, { clientY: 0 });
    await advance(100);
    await advance(200);

    fireEvent.keyDown(window, { key: "Escape" });

    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    expect(link).not.toHaveFocus();
  });

  it("lets the keyboard enter and leave a card that has no links", async () => {
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    const link = screen.getByRole("link", { name: "Target" });
    act(() => link.focus());
    await advance(100);
    await advance(200);
    const card = screen.getByRole("region");
    fireEvent.keyDown(link, { key: "Tab" });
    expect(card).toHaveFocus();
    await advance(150);
    expect(card).toBeInTheDocument();
    fireEvent.keyDown(card, { key: "Tab", shiftKey: true });
    expect(link).toHaveFocus();
    expect(card).toBeInTheDocument();
    fireEvent.keyDown(link, { key: "Tab" });
    expect(card).toHaveFocus();
    fireEvent.keyDown(card, { key: "Tab" });
    expect(link).toHaveFocus();
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    await advance(1000);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
    expect(fireEvent.keyDown(link, { key: "Tab" })).toBe(true);
  });

  it("opens a nested link while keeping its parent open and Escape closes only the nested card", async () => {
    const parent: NodePreview = {
      ...preview,
      fields: [
        {
          name: "related",
          label: "Related",
          kind: "link",
          importance: "NORMAL",
          values: [{ text: "Nested note", target: "notes/nested.md" }],
        },
      ],
    };

    const nested: NodePreview = {
      ...preview,
      ref: "notes/nested.md",
      path: "notes/nested.md",
      title: "Nested note",
    };

    http.on("GET", "/api/v1/nodes/preview", (request) =>
      jsonReply(request.query.get("ref") === "notes/nested.md" ? nested : parent),
    );
    renderLink();
    fireEvent.mouseEnter(screen.getByRole("link", { name: "Target" }), { clientY: 0 });
    await advance(100);
    await advance(200);
    fireEvent.mouseEnter(screen.getByRole("link", { name: "Nested note" }), { clientY: 0 });
    await advance(100);
    await advance(200);

    expect(screen.getAllByRole("region")).toHaveLength(2);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.getAllByRole("region")).toHaveLength(1);
    expect(screen.getByRole("region", { name: "Preview of Target note" })).toBeInTheDocument();
  });

  it("does not attach hover intent on a no-hover pointer", async () => {
    setHover(false);
    http.json("GET", "/api/v1/nodes/preview", preview);
    renderLink();
    fireEvent.mouseEnter(screen.getByRole("link", { name: "Target" }), { clientY: 0 });
    await advance(800);

    expect(http.count("GET", "/api/v1/nodes/preview")).toBe(0);
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });
});

describe("previewPosition", () => {
  const anchor = { left: 100, top: 120, bottom: 140 };
  const card = { width: 200, height: 160 };
  const viewport = { width: 900, height: 700 };

  it("places nested cards to the right of their parent when they fit", () => {
    const parent = new DOMRect(250, 80, 300, 400);

    expect(previewPosition(anchor, card, viewport, parent)).toEqual({ left: 556, top: 120 });
  });

  it("uses the left side when a nested card does not fit on the right", () => {
    const parent = new DOMRect(500, 80, 300, 400);

    expect(previewPosition(anchor, card, viewport, parent)).toEqual({ left: 294, top: 120 });
  });

  it("places a top-level card to the left when requested", () => {
    const railAnchor = { left: 700, top: 120, bottom: 140 };

    expect(previewPosition(railAnchor, card, viewport, null, "left")).toEqual({
      left: 494,
      top: 120,
    });
  });

  it("falls back above the anchor and clamps when neither side fits", () => {
    const parent = new DOMRect(100, 80, 700, 400);
    const lowerAnchor = { left: 850, top: 650, bottom: 670 };

    expect(previewPosition(lowerAnchor, card, viewport, parent)).toEqual({ left: 692, top: 484 });
  });
});
