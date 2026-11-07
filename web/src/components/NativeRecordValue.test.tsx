import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { isNativeSourceContent } from "../api/parse";
import type { NativeSourceContent } from "../api/nativeSource";
import { workspaceWithEmbeddedRelation } from "./body/noteInfoFixtures.test-support";
import { NativeRecordValue } from "./NativeRecordValue";

const source: NativeSourceContent = {
  representation: "source_value",
  mediaType: "application/json",
  text: "",
  address: "/rows/0",
  value: {
    kind: "object",
    entries: [
      { key: "name", value: { kind: "string", scalar: "Booking" } },
      {
        key: "details",
        value: {
          kind: "object",
          entries: [
            { key: "nullable", value: { kind: "null" } },
            { key: "blank", value: { kind: "string", scalar: "" } },
            { key: "list", value: { kind: "array" } },
            { key: "map", value: { kind: "object" } },
            { key: "enabled", value: { kind: "boolean", scalar: false } },
            {
              key: "literal",
              value: { kind: "string", scalar: "Literal **text**, never Markdown" },
            },
            {
              key: "ordered",
              value: {
                kind: "array",
                items: [
                  { kind: "integer", scalar: 1 },
                  { kind: "null" },
                  { kind: "integer", scalar: 1 },
                ],
              },
            },
            { key: 1, value: { kind: "string", scalar: "Integer key" } },
            {
              key: 9007199254740992,
              keyText: "9007199254740993",
              value: { kind: "integer", scalar: 9007199254740992, scalarText: "9007199254740993" },
            },
            { key: "1", value: { kind: "string", scalar: "String key" } },
          ],
        },
      },
    ],
  },
};

it("renders native nested values faithfully and discloses repeated bound values", async () => {
  const workspace = workspaceWithEmbeddedRelation();
  workspace.fields = [
    {
      name: "name",
      kind: "scalar",
      present: true,
      values: ["Booking"],
      range: { start: 0, end: 0 },
      status: workspace.status,
    },
  ];
  const { container } = render(<NativeRecordValue source={source} workspace={workspace} />);
  expect(screen.getByRole("heading", { name: "Additional properties" })).toBeVisible();
  expect(screen.queryByText("Booking")).toBeNull();
  expect(screen.getByLabelText("Empty string")).toBeVisible();
  expect(screen.getByLabelText("Empty list")).toBeVisible();
  expect(screen.getByLabelText("Empty map")).toBeVisible();
  expect(screen.getAllByText("null")[0]).toBeVisible();
  expect(screen.getByText("false")).toBeVisible();
  expect(screen.getByText("Literal **text**, never Markdown")).toBeVisible();
  expect(container.querySelector("strong")).toBeNull();
  expect(screen.queryByText("missing")).toBeNull();
  expect(screen.getByText('"1"')).toBeVisible();
  expect(screen.getAllByText("9007199254740993")).toHaveLength(2);
  expect(screen.getByRole("list")).toHaveTextContent("1null1");
  fireEvent.click(screen.getByText("All record properties"));
  expect(await screen.findByText("Booking")).toBeVisible();
  expect(screen.getByText("/rows/0")).toBeVisible();
});

it("rejects malformed native values at the API boundary", () => {
  expect(isNativeSourceContent(source)).toBe(true);
  expect(
    isNativeSourceContent({
      ...source,
      value: { kind: "object", entries: [{ key: "bad", value: { kind: "string", scalar: null } }] },
    }),
  ).toBe(false);
  expect(isNativeSourceContent({ ...source, value: { kind: "missing" } })).toBe(false);
});
