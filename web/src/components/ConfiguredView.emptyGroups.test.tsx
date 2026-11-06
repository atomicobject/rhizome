import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { ConfiguredView } from "./ConfiguredView";
import { baseView, execution } from "./ConfiguredView.testFixtures";

it.each(["empty", "failed", "missing"])(
  "clears collapsed controls after a %s result and restores them on recovery",
  (result) => {
    const data = execution();
    data.groups = [
      {
        key: "draft",
        label: "Draft",
        value: "draft",
        field: "frontmatter.spec-status",
        depth: 0,
        count: 2,
        rowStart: 0,
        rowEnd: 2,
        collapsedByDefault: true,
      },
    ];

    const props = {
      view: baseView,
      execution: data,
      loading: false,
      error: null,
      state: {},
      onStateChange: () => {},
      onRefresh: () => {},
      onOpenRow: () => {},
    };

    const { rerender } = render(<ConfiguredView {...props} />);
    expect(screen.getByRole("button", { name: /Expand Draft/ })).toBeVisible();
    rerender(
      <ConfiguredView
        {...props}
        execution={result === "missing" ? null : result === "empty" ? execution([]) : data}
        error={result === "failed" ? "Failed" : null}
      />,
    );
    expect(screen.queryByRole("button", { name: /Expand Draft/ })).toBeNull();
    expect(screen.queryByText(/1 group collapsed/)).toBeNull();
    rerender(<ConfiguredView {...props} />);
    fireEvent.click(screen.getByRole("button", { name: /Expand Draft/ }));
    expect(screen.queryByRole("button", { name: /Expand Draft/ })).toBeNull();
  },
);
