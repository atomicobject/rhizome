import { afterEach, describe, expect, it } from "vitest";

import {
  getViewConfiguration,
  getViewContext,
  getViewInvocation,
  useViewContext,
} from "./viewContext";

afterEach(() => document.getElementById("rhizome-view-invocation")?.remove());

it("reads the same canonical invocation in HTML and TSX without URL overrides", () => {
  const script = document.createElement("script");
  script.id = "rhizome-view-invocation";
  script.type = "application/json";
  script.textContent = JSON.stringify({
    view: { id: "delivery", name: "Delivery", origin: "bundled" },
    context: { kind: "group", group: "Delivery" },
    configuration: { limit: 12 },
  });
  document.head.append(script);

  expect(getViewContext()).toEqual({ kind: "group", group: "Delivery" });
  expect(useViewContext()).toEqual(getViewContext());
  expect(getViewConfiguration()).toEqual({ limit: 12 });
  expect(getViewInvocation().view).toEqual({ id: "delivery", name: "Delivery", origin: "bundled" });
  script.textContent = JSON.stringify({
    view: { id: "delivery", name: "Delivery", origin: "elsewhere" },
    context: { kind: "standalone" },
  });
  expect(() => getViewInvocation()).toThrow("Invalid view origin");
});

it("reads a workspace invocation and rejects a workspace context with a subject", () => {
  const script = document.createElement("script");
  script.id = "rhizome-view-invocation";
  script.type = "application/json";
  script.textContent = JSON.stringify({
    view: { id: "workspace.overview", name: "Overview" },
    context: { kind: "workspace" },
  });
  document.head.append(script);

  expect(useViewContext()).toEqual({ kind: "workspace" });
  script.textContent = JSON.stringify({
    view: { id: "workspace.overview", name: "Overview" },
    context: { kind: "workspace", group: "Delivery" },
  });
  expect(() => getViewInvocation()).toThrow("Invalid server view invocation");
});

describe("unregistered standalone tools", () => {
  it("receive standalone context", () => expect(getViewContext()).toEqual({ kind: "standalone" }));
});
