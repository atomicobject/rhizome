import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { useViewHeaderSlotRefs, ViewHeader, ViewHeaderSlotsProvider } from "./ConfiguredViewHeader";
import { TypeWorkspaceHeader } from "./TypeWorkspaceHeader";
import { typeView } from "./ConfiguredView.typeFixtures";

function Page() {
  const header = useViewHeaderSlotRefs();

  return (
    <ViewHeaderSlotsProvider value={header.slots}>
      <ViewHeader
        title="Specs"
        description="Contracts the product commits to."
        countRef={header.countRef}
        toolsRef={header.toolsRef}
        tabs={<span>Tabs</span>}
      />
      {typeView({ embedded: true })}
    </ViewHeaderSlotsProvider>
  );
}

describe("view header slots", () => {
  it("puts an embedded view's count, Find, Filter, and View on the page's title row", () => {
    render(<Page />);

    const header = screen.getByRole("banner");
    expect(within(header).getByRole("heading", { name: "Specs" })).toBeVisible();
    expect(within(header).getByRole("searchbox", { name: "Search view" })).toBeVisible();
    expect(within(header).getByRole("button", { name: "Filter" })).toBeVisible();
    expect(within(header).getByRole("button", { name: "Display options" })).toBeVisible();
    // The view's own row count, beside the title.
    expect(header.querySelector(".view-header__count")).toHaveTextContent(/^\d+$/);
    expect(document.querySelector(".configured-view__toolbar")).toBeNull();
  });

  it("keeps the tools in a row of their own when no page header encloses the view", () => {
    render(typeView({ embedded: true }));

    const toolbar = document.querySelector<HTMLElement>(".configured-view__toolbar");

    if (!toolbar) throw new Error("The view drew no toolbar row.");
    expect(within(toolbar).getByRole("button", { name: "Filter" })).toBeVisible();
  });
});

describe("type header schema information", () => {
  it.each(["Shared identity. More context.", ""])(
    "keeps the identifier in the info tooltip with description %s",
    (description) => {
      render(
        <TypeWorkspaceHeader
          typeName="Effort"
          label="Efforts"
          eyebrow="Interface"
          description={description}
          totalNotes={52}
          issueCount={0}
          meanRelations={0}
        />,
      );
      const info = screen.getByRole("link", { name: "View ontology type Effort" });
      expect(info).toHaveAttribute("title", expect.stringContaining("Interface: Effort"));
      expect(info).toHaveAttribute("href", "/ontology/type/Effort");
      expect(info).not.toHaveTextContent("Effort");
    },
  );
});
