import { render } from "@testing-library/react";
import { expect, it } from "vitest";

import { StatusMark } from "./statusMark";

it("uses warning ink for the small warning mark even with its label hidden", () => {
  const { container } = render(
    <StatusMark value="blocked" values={[{ name: "blocked", tone: "warning" }]} hideLabel />,
  );

  expect(container.querySelector("svg")).toHaveStyle({ color: "var(--warning-ink)" });
});
