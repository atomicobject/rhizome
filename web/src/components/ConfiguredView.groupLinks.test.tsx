import { render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { ConfiguredViewCards } from "./ConfiguredViewCards";
import { ConfiguredViewBoard } from "./ConfiguredViewBoard";
import { ideaBoard } from "./ConfiguredView.profileFixtures";
import { rendererProps } from "./ConfiguredView.testFixtures";

it.each(["cards", "board"])("keeps both relation links on grouped %s", (mode) => {
  const execution = ideaBoard();
  execution.groups = [
    {
      depth: 0,
      key: "agents",
      field: "opportunities",
      value: "agents",
      label: "Agents",
      count: 3,
      rowStart: 0,
      rowEnd: 3,
    },
  ];
  const props = rendererProps({ execution });
  render(
    mode === "cards" ? <ConfiguredViewCards {...props} /> : <ConfiguredViewBoard {...props} />,
  );

  for (const title of screen.getAllByRole("button", { name: "Bravo" })) {
    const card = title.closest("article")!;
    expect(within(card).getByRole("link", { name: "Agents" })).toBeVisible();
    expect(within(card).getByRole("link", { name: "Products" })).toBeVisible();
  }
});
