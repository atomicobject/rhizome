import { screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { typeExecution, typeView } from "./ConfiguredView.typeFixtures";

it("keeps Cards and Board page ranges visible and hides only repeated totals", () => {
  for (const variant of ["card", "kanban"] as const) {
    for (const offset of [0, 2]) {
      const mounted = render(
        <div className="notes-shell">
          <span className="view-header__count">4</span>
          {typeView({
            state: { variant },
            execution: typeExecution({
              pageInfo: { total: 4, offset, first: 2, returned: 2, hasMore: offset === 0 },
            }),
          })}
        </div>,
      );

      expect(screen.getByText(`${offset + 1}–${offset + 2} of 4 rows`)).not.toHaveClass(
        "configured-view__duplicate-count",
      );
      mounted.unmount();
    }
  }

  render(
    <div className="notes-shell">
      <span className="view-header__count">2</span>
      {typeView({ state: { variant: "card" } })}
    </div>,
  );
  expect(screen.getByText("2 rows")).toHaveClass("configured-view__duplicate-count");
});
