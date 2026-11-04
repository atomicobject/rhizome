import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { RecordBriefHeader } from "./ConfiguredViewBrief";
import { firstRow } from "./ConfiguredView.testFixtures";
import { STATUS, typeExecution } from "./ConfiguredView.typeFixtures";

it("keeps an editable status the card's group names, since it is the only editor", () => {
  const execution = typeExecution();
  const profile = execution.profile;

  if (!profile) throw new Error("fixture has no profile");

  const header = (onStageOps?: () => Promise<void>) => (
    <RecordBriefHeader
      row={firstRow}
      profile={profile}
      capabilities={execution.capabilities ?? []}
      omitField={STATUS}
      onStageOps={onStageOps}
    />
  );

  const { rerender } = render(header());
  expect(screen.queryByText("Active")).toBeNull();

  rerender(header(async () => {}));
  expect(screen.getByRole("button", { name: "Edit Status" })).toBeVisible();
});
