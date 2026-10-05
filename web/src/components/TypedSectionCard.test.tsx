import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TypedSectionCard } from "./TypedSectionCard";

describe("TypedSectionCard", () => {
  it("renders its preview through the shared Markdown component overrides", () => {
    const section = {
      id: "section-1",
      title: "Fallback",
      level: "H2",
      content: "## Fallback\n\nBody",
      previewTemplate: "[Linked](custom:target)",
      properties: {},
      collapsed: false,
      children: [],
    };

    const { container } = render(
      <TypedSectionCard
        section={section}
        markdownComponents={{
          a: ({ href, children }) => <a href={href}>{children}</a>,
        }}
      >
        <p>Body</p>
      </TypedSectionCard>,
    );

    expect(screen.getByRole("link", { name: "Linked" })).toHaveAttribute("href", "custom:target");
    expect(container.querySelector(".typed-section-card__preview > p")).toBeNull();
  });
});
