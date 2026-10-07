import { screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { FakeEventSource } from "../src/test/fakeEventSource";
import { renderView } from "./testing";
import { useViewContext } from "./viewContext";

it("renders the way mountView does: in StrictMode, on the page's own stream when not framed", async () => {
  let renders = 0;

  const View = () => {
    renders += 1;

    return <p>Rendered</p>;
  };

  await renderView(async () => ({ default: View }));

  expect(screen.getByText("Rendered")).toBeTruthy();
  expect(renders).toBe(2);
  expect(FakeEventSource.instances).toHaveLength(1);
});

it("runs the startup sequence, so a failed load still reloads when its folder changes", async () => {
  const view = await renderView(
    async () => {
      throw new Error("Unexpected token");
    },
    { view: { id: "board", name: "Board", origin: "repository", folder: "board" } },
  );

  expect(screen.getByRole("alert").textContent).toContain("Board failed to load");
  await view.emit("views.changed", JSON.stringify({ data: { folder: "board" } }));
  expect(view.reload).toHaveBeenCalledTimes(1);
});

it("starts a view at All notes with the workspace context", async () => {
  const View = () => <p>{JSON.stringify(useViewContext())}</p>;

  await renderView(async () => ({ default: View }), { context: { kind: "workspace" } });

  expect(screen.getByText('{"kind":"workspace"}')).toBeTruthy();
});
