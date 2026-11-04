import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { FileView } from "../api/types";
import { FilePanel } from "./FilePanel";

describe("FilePanel", () => {
  it("renders code HTML as text instead of injecting markup", () => {
    const file: FileView = {
      path: "src/example.ts",
      kind: "code",
      lang: "typescript",
      content: 'const html = "<img src=x onerror=alert(1)>";',
    };

    const { container } = render(
      <FilePanel file={file} onClose={vi.fn()} onSelectFile={vi.fn()} />,
    );

    expect(screen.getByText(/onerror=alert/)).toBeInTheDocument();
    expect(container.querySelector("img")).toBeNull();
  });

  it("marks a valid deep-linked source line and ignores an unavailable line", () => {
    const file: FileView = {
      path: "src/example.ts",
      kind: "code",
      lang: "typescript",
      content: "const first = 1;\nconst selected = 2;\n",
    };

    const props = {
      file,
      onClose: vi.fn(),
      onSelectFile: vi.fn(),
    };

    const { container, rerender } = render(<FilePanel {...props} line={2} />);

    expect(container.querySelector('[data-line="2"]')).toHaveAttribute("aria-current", "location");
    expect(screen.getByText(/line 2/)).toBeVisible();

    rerender(<FilePanel {...props} line={99} />);
    expect(container.querySelector('[aria-current="location"]')).toBeNull();
    expect(screen.queryByText(/line 99/)).toBeNull();
  });

  it("lets authored links resolve source targets while identifying related notes", () => {
    const onSelectFile = vi.fn();

    const props = {
      onClose: vi.fn(),
      onSelectFile,
    };

    const { rerender } = render(
      <FilePanel
        {...props}
        file={{
          path: "docs/guide.md",
          kind: "note",
          content: "[implementation](src/main.go)",
          links: [{ target: "src/main.go", text: "implementation", kind: "markdown" }],
        }}
      />,
    );

    screen
      .getAllByRole("button", { name: /implementation/ })
      .forEach((link) => fireEvent.click(link));
    expect(onSelectFile.mock.calls).toEqual([["src/main.go", undefined], ["src/main.go"]]);

    rerender(
      <FilePanel
        {...props}
        file={{
          path: "src/main.go",
          kind: "code",
          relatedNotes: [{ path: "docs/guide.md", title: "Guide" }],
        }}
      />,
    );
    const guide = screen.getByRole("button", { name: /Guide/ });

    expect(guide.querySelector("svg.lucide-file-text")).not.toBeNull();
    fireEvent.click(guide);
    expect(onSelectFile).toHaveBeenLastCalledWith("docs/guide.md", "note");
  });
});
