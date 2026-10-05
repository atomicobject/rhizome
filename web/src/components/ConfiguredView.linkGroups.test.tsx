import { fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ViewExecuteResponse } from "../api/types";
import { execution, renderView } from "./ConfiguredView.testFixtures";

const link = "[[IP opportunity - AI in the products we build]]";

const title = "AI in the products we build";

function groupedByLink(): ViewExecuteResponse {
  const base = execution();

  return {
    ...base,
    // SAFETY: see execution(); these are the ViewExecuteRequest fields the view reads.
    state: {
      page: { offset: 0, first: 25 },
      search: "",
      group: { field: "opportunities" },
    } as ViewExecuteResponse["state"],
    capabilities: [
      ...(base.capabilities ?? []),
      {
        key: "opportunities",
        label: "Opportunities",
        valueKind: "relation",
        sortable: false,
        groupable: true,
        filterOps: ["eq", "in", "exists"],
        facets: { values: [link], labels: { [link]: title } },
      },
    ],
    groups: [
      {
        field: "opportunities",
        key: "opportunities=ai",
        label: title,
        value: link,
        count: 2,
        depth: 0,
        rowStart: 0,
        rowEnd: 2,
      },
    ],
  };
}

describe("Configured views grouped by a link field", () => {
  it("offers the link field in Group and names each group by its target's title", () => {
    renderView({ execution: groupedByLink() });

    const groupBy = screen.getByLabelText("Group by");
    expect(groupBy).toHaveValue("opportunities");
    expect(within(groupBy).getByRole("option", { name: "Opportunities" })).toBeInTheDocument();

    const header = screen.getByRole("button", { name: `${title} 2` });
    expect(header).not.toHaveTextContent("[[");
    // A note is not a status, so its group has no status marker.
    expect(header.querySelector(".status-mark")).toBeNull();
  });

  it("names link filter values by title", () => {
    renderView({
      execution: groupedByLink(),
      state: {
        page: { offset: 0, first: 25 },
        filters: [{ field: "opportunities", op: "eq", value: link }],
      },
    });

    expect(screen.getByText(`Opportunities is ${title}`)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
    fireEvent.change(screen.getAllByLabelText("Field").at(-1)!, {
      target: { value: "opportunities" },
    });
    expect(screen.getAllByRole("button", { name: title }).length).toBeGreaterThan(0);
  });
});
