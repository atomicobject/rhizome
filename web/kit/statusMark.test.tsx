import { render } from "@testing-library/react";
import { expect, it } from "vitest";

import { StatusMark } from "./statusMark";

it("uses warning ink for the small warning mark even with its label hidden", () => {
  const { container } = render(
    <StatusMark value="unsaved" values={[{ name: "unsaved", tone: "warning" }]} hideLabel />,
  );

  expect(container.querySelector("svg")).toHaveStyle({ color: "var(--warning-ink)" });
});

it("uses the same blue half circle for Active across conflicting schema tones", () => {
  const { container } = render(
    <StatusMark value="active" values={[{ name: "active", label: "Active", tone: "success" }]} />,
  );

  expect(container.querySelector("[data-tone]")).toHaveAttribute("data-tone", "progress");
  expect(container.querySelector("svg")).toHaveStyle({ color: "var(--info)" });
  expect(container.querySelector("path")).not.toBeNull();
});
