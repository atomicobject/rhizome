import { screen, within } from "@testing-library/react";

/** The workspace view switcher's segment named `name`, within `scope`. */
export function viewSegment(name: string, scope: Pick<typeof screen, "getByRole"> = screen) {
  return within(scope.getByRole("toolbar", { name: "Workspace view" })).getByRole("button", {
    name,
  });
}
