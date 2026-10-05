import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Markdown } from "./Markdown";
import { renderMermaid } from "./mermaid";

// oxlint-disable-next-line anti-slop/no-module-mocking -- The acceptance test requires a mocked renderMermaid boundary.
vi.mock("./mermaid", () => ({ renderMermaid: vi.fn() }));

const mockedRenderMermaid = vi.mocked(renderMermaid);

describe("Markdown", () => {
  afterEach(() => {
    mockedRenderMermaid.mockReset();
  });

  it("renders Mermaid fences through the lazy renderer", async () => {
    mockedRenderMermaid.mockResolvedValue('<svg aria-label="flow diagram"></svg>');

    const { container } = render(<Markdown>{"```mermaid\ngraph TD\nA-->B\n```"}</Markdown>);

    await waitFor(() => expect(container.querySelector(".mermaid-block svg")).not.toBeNull());
    expect(mockedRenderMermaid).toHaveBeenCalledWith("graph TD\nA-->B", expect.any(String));
    expect(container.querySelector("pre > .mermaid-block")).toBeNull();
  });

  it("shows the source and parse error when Mermaid rejects the diagram", async () => {
    mockedRenderMermaid.mockRejectedValue(new Error("Parse failed"));

    render(<Markdown>{"```mermaid\nnot a diagram\n```"}</Markdown>);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not render diagram: Parse failed",
    );
    expect(screen.getByText("not a diagram", { selector: "pre code" })).toBeInTheDocument();
  });

  it("keeps ordinary fenced code in pre and code elements", () => {
    const { container } = render(<Markdown>{"```ts\nconst value = 1;\n```"}</Markdown>);

    const code = container.querySelector("pre > code");
    expect(code).toHaveTextContent("const value = 1;");
    expect(mockedRenderMermaid).not.toHaveBeenCalled();
  });

  it("merges caller component overrides over the defaults", () => {
    render(
      <Markdown
        components={{ a: ({ children }) => <button type="button">Open {children}</button> }}
      >
        {"[the note](notes/example.md)"}
      </Markdown>,
    );

    expect(screen.getByRole("button", { name: "Open the note" })).toBeInTheDocument();
  });
});
