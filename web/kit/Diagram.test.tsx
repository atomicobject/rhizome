import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderDiagram } from "../src/components/markdown/mermaid";
import { Diagram } from "./Diagram";
import { openNode } from "./index";

// oxlint-disable-next-line anti-slop/no-module-mocking -- Observe note navigation without posting messages to the host.
vi.mock("./index", () => ({ openNode: vi.fn() }));

// oxlint-disable-next-line anti-slop/no-module-mocking -- Defer real Pele rendering to verify pending and stale updates.
vi.mock("../src/components/markdown/mermaid", async (importOriginal) => {
  const original = await importOriginal<typeof import("../src/components/markdown/mermaid")>();

  return { ...original, renderDiagram: vi.fn(original.renderDiagram) };
});

const original = await vi.importActual<typeof import("../src/components/markdown/mermaid")>(
  "../src/components/markdown/mermaid",
);

const renderer = vi.mocked(renderDiagram);

const ref = { notePath: "Ideas/First.md", kind: "NOTE" };

const otherRef = { notePath: "Ideas/Second.md", kind: "NOTE" };

const first = 'flowchart LR\n A["First idea"]';

const second = 'flowchart LR\n A["Second idea"]';

function holdNextRender() {
  let release = () => {};

  renderer.mockImplementationOnce(
    (...args) =>
      new Promise((resolve, reject) => {
        release = () => void original.renderDiagram(...args).then(resolve, reject);
      }),
  );

  return () => release();
}

function anchorIn(container: HTMLElement) {
  const anchor = container.querySelector("a");

  if (!anchor) throw new Error("Expected a linked diagram node");

  return anchor;
}

beforeEach(() => {
  renderer.mockReset().mockImplementation(original.renderDiagram);
  vi.mocked(openNode).mockClear();
  // Use Pele's font metrics because jsdom has no canvas text measurement.
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Diagram", () => {
  it("shows its accessible name and initial rendering state", async () => {
    const release = holdNextRender();
    const view = render(<Diagram source={first} label="Portfolio" className="custom" />);
    expect(view.getByRole("group", { name: "Portfolio" })).toHaveClass("custom");
    expect(view.getByText("Rendering diagram…")).toBeVisible();

    release();
    await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
    expect(view.queryByText("Rendering diagram…")).toBeNull();
    expect(view.getByRole("img", { name: "Portfolio" })).toBeVisible();
  });

  it("keeps the old SVG and its targets until the new source finishes", async () => {
    const view = render(<Diagram source={first} label="Portfolio" links={{ A: ref }} />);
    await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
    const oldSvg = view.container.querySelector("svg");
    const release = holdNextRender();

    view.rerender(<Diagram source={second} label="Portfolio" links={{ A: otherRef }} />);
    expect(view.container.querySelector("svg")).toBe(oldSvg);
    expect(view.queryByText("Rendering diagram…")).toBeNull();
    fireEvent.click(anchorIn(view.container));
    expect(openNode).toHaveBeenLastCalledWith(ref, { beside: false });

    release();
    await waitFor(() => expect(view.container.querySelector("svg")).not.toBe(oldSvg));
    expect(view.container.querySelector("svg")).toHaveTextContent("Second idea");
    fireEvent.click(anchorIn(view.container));
    expect(openNode).toHaveBeenLastCalledWith(otherRef, { beside: false });
  });

  it("skips equal sources and equal fresh link objects, regardless of key order", async () => {
    const source = `${first}\n B["Other idea"]`;

    const view = render(
      <Diagram source={source} label="Portfolio" links={{ A: ref, B: otherRef }} />,
    );

    await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
    const svg = view.container.querySelector("svg");

    view.rerender(
      <Diagram source={source} label="Renamed" links={{ B: { ...otherRef }, A: { ...ref } }} />,
    );
    expect(renderer).toHaveBeenCalledTimes(1);
    expect(view.container.querySelector("svg")).toBe(svg);
    expect(view.getByRole("group", { name: "Renamed" })).toBeVisible();
  });

  it.each([
    { ...ref, notePath: "Ideas/Changed.md" },
    { ...ref, kind: "HEADING" },
    { ...ref, nodeId: "heading-1" },
  ])("renders again when a serialized target changes to %j", async (target) => {
    const view = render(<Diagram source={first} label="Portfolio" links={{ A: ref }} />);
    await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
    view.rerender(<Diagram source={first} label="Portfolio" links={{ A: target }} />);
    await waitFor(() => expect(renderer).toHaveBeenCalledTimes(2));
  });

  it.each([
    [{}, false],
    [{ metaKey: true }, true],
    [{ ctrlKey: true }, true],
  ])("opens linked notes with click modifiers %j", async (modifiers, beside) => {
    const view = render(<Diagram source={first} label="Portfolio" links={{ A: ref }} />);
    await waitFor(() => expect(view.container.querySelector("a")).not.toBeNull());
    const anchor = anchorIn(view.container);
    expect(anchor).toHaveAttribute("tabindex", "0");
    expect(view.getByRole("group", { name: "Portfolio" })).toBeVisible();
    expect(fireEvent.click(anchor, modifiers)).toBe(false);
    expect(openNode).toHaveBeenCalledWith(ref, { beside });
  });

  it.each([
    ["flowchart", first],
    ["gantt", "gantt\n dateFormat YYYY-MM-DD\n Task :A, 2026-01-01, 1d"],
    ["class", "classDiagram\n class A"],
    ["state", "stateDiagram-v2\n A"],
    ["agentflow", 'agentflow-beta\n A["Agent"]'],
  ])("renders clickable notes in supported type %s", async (_type, source) => {
    const view = render(<Diagram source={source} label="Linked chart" links={{ A: ref }} />);
    await waitFor(() => expect(view.container.querySelector("a")).not.toBeNull());
    fireEvent.click(anchorIn(view.container));
    expect(openNode).toHaveBeenCalledWith(ref, { beside: false });
  });

  it("intercepts middle clicks and opens known notes beside", async () => {
    const view = render(
      <Diagram
        source={`${first}\n B[Fragment]\n click B href "#unknown"`}
        label="Portfolio"
        links={{ A: ref }}
      />,
    );

    await waitFor(() => expect(view.container.querySelectorAll("a")).toHaveLength(2));
    const known = view.container.querySelector('a[href="#rzm-link-0"]')!;
    const unknown = view.container.querySelector('a[href="#unknown"]')!;
    expect(
      fireEvent(known, new MouseEvent("auxclick", { button: 1, bubbles: true, cancelable: true })),
    ).toBe(false);
    expect(openNode).toHaveBeenCalledWith(ref, { beside: true });
    vi.mocked(openNode).mockClear();
    expect(
      fireEvent(
        unknown,
        new MouseEvent("auxclick", { button: 1, bubbles: true, cancelable: true }),
      ),
    ).toBe(false);
    expect(openNode).not.toHaveBeenCalled();
  });

  it("warns for whitespace ids without breaking valid links or their target index", async () => {
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});

    const view = render(
      <Diagram source={first} label="Portfolio" links={{ " bad id": otherRef, A: ref }} />,
    );

    await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("bad id"));
    fireEvent.click(anchorIn(view.container));
    expect(openNode).toHaveBeenCalledWith(ref, { beside: false });
  });

  it.each(["α", "Foo.Bar", "a:b", "a/b"])("preserves valid authored node id %s", async (id) => {
    const view = render(
      <Diagram source={`flowchart LR\n ${id}[Alpha]`} label="Portfolio" links={{ [id]: ref }} />,
    );

    await waitFor(() => expect(view.container.querySelector("a")).not.toBeNull());
    fireEvent.click(anchorIn(view.container));
    expect(openNode).toHaveBeenCalledWith(ref, { beside: false });
  });

  it("drops author web and script links and prevents unknown fragments", async () => {
    const source = [
      first,
      ' B["Script"]',
      ' C["Fragment"]',
      ' D["Relative"]',
      ' click A href "https://example.com"',
      ' click B href "javascript:alert(1)"',
      ' click C href "#unknown"',
      ' click D href "other.html"',
    ].join("\n");

    const view = render(<Diagram source={source} label="Author links" />);
    await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
    expect(view.container.querySelectorAll("a")).toHaveLength(2);
    const anchor = anchorIn(view.container);
    expect(anchor).toHaveAttribute("href", "#unknown");
    expect(fireEvent.click(anchor)).toBe(false);
    const relative = view.container.querySelector('a[href="other.html"]')!;
    expect(fireEvent.click(relative)).toBe(false);
    expect(
      fireEvent(
        relative,
        new MouseEvent("auxclick", { button: 1, bubbles: true, cancelable: true }),
      ),
    ).toBe(false);
    expect(view.getByRole("group", { name: "Author links" })).toBeVisible();
    expect(openNode).not.toHaveBeenCalled();
  });

  it.each(["a;b", "a-->b", 'bad"id'])(
    "keeps valid source visible if generated id %s breaks parsing",
    async (id) => {
      const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
      const view = render(<Diagram source={first} label="Portfolio" links={{ [id]: ref }} />);
      await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
      expect(warning).toHaveBeenCalledWith(expect.stringContaining(id));
      expect(view.container.querySelector("a")).toBeNull();
      expect(view.queryByText(/^Diagram error:/)).toBeNull();
    },
  );

  it("warns once per source about missing ids and names the diagram type", async () => {
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    const view = render(<Diagram source={first} label="Portfolio" links={{ missing: ref }} />);
    await waitFor(() => expect(warning).toHaveBeenCalledTimes(1));
    expect(warning.mock.calls[0]?.[0]).toMatch(/flowchart.*missing/);

    view.rerender(<Diagram source={first} label="Portfolio" links={{ missing: otherRef }} />);
    await waitFor(() => expect(renderer).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(warning).toHaveBeenCalledTimes(1);

    view.rerender(<Diagram source={second} label="Portfolio" links={{ missing: ref }} />);
    await waitFor(() => expect(warning).toHaveBeenCalledTimes(2));
  });

  it("renders unsupported link types without injected click syntax and warns once", async () => {
    const source = 'C4Context\n Person(user, "Reader", $link="#author-fragment")';
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    const view = render(<Diagram source={source} label="Readers" links={{ user: ref }} />);
    await waitFor(() => expect(view.container.querySelector("svg")).not.toBeNull());
    expect(view.container.querySelector("a")).toBeNull();
    expect(warning).toHaveBeenCalledTimes(1);
    expect(warning.mock.calls[0]?.[0]).toMatch(/c4/i);
    expect(warning.mock.calls[0]?.[0]).toMatch(/flowchart.*gantt/);

    view.rerender(<Diagram source={source} label="Readers" links={{ user: otherRef }} />);
    await waitFor(() => expect(renderer).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(warning).toHaveBeenCalledTimes(1);
  });

  it("contains Pele errors with source details and logs once per source", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const view = render(<Diagram source="not a diagram" label="Broken" />);
    await waitFor(() => expect(view.getByText(/^Diagram error:/)).toBeVisible());
    expect(view.getByRole("group", { name: "Broken" })).toBeVisible();
    expect(view.getByText("Source").tagName).toBe("SUMMARY");
    expect(view.container.querySelector("details pre")).toHaveTextContent("not a diagram");
    expect(error).toHaveBeenCalledTimes(1);

    view.rerender(<Diagram source="not a diagram" label="Broken" links={{ A: ref }} />);
    await waitFor(() => expect(renderer).toHaveBeenCalledTimes(2));
    await act(async () => {});
    expect(error).toHaveBeenCalledTimes(1);

    view.rerender(<Diagram source="still not a diagram" label="Broken" />);
    await waitFor(() => expect(error).toHaveBeenCalledTimes(2));
  });

  it.each(["", " \n\t"])("renders no content or request for empty source %j", (source) => {
    const view = render(<Diagram source={source} label="Empty" />);
    expect(view.container).toBeEmptyDOMElement();
    expect(renderer).not.toHaveBeenCalled();
  });

  it("gives simultaneous diagrams distinct SVG title ids", async () => {
    const source = "flowchart LR\n accTitle: Accessible chart\n A --> B";

    const view = render(
      <>
        <Diagram source={source} label="First" />
        <Diagram source={source} label="Second" />
      </>,
    );

    await waitFor(() => expect(view.container.querySelectorAll("svg title")).toHaveLength(2));
    const ids = Array.from(view.container.querySelectorAll("svg title"), (title) => title.id);
    expect(ids[0]).not.toBe(ids[1]);
    expect(ids.every(Boolean)).toBe(true);
  });

  it("ignores stale render results after a newer source finishes", async () => {
    const release = holdNextRender();
    const view = render(<Diagram source={first} label="Portfolio" />);
    view.rerender(<Diagram source={second} label="Portfolio" />);
    await waitFor(() =>
      expect(view.container.querySelector("svg")).toHaveTextContent("Second idea"),
    );
    const svg = view.container.querySelector("svg");

    await act(async () => release());
    expect(view.container.querySelector("svg")).toBe(svg);
    expect(view.container.querySelector("svg")).toHaveTextContent("Second idea");
  });
});
