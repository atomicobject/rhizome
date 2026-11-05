import { expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { createElement } from "react";
import { LinkSummary } from "./overview-side-parts.tsx";

it("retains every destination and its count", () => {
  const text = "Reference docs 53, Technical specs 41";
  expect(renderToStaticMarkup(createElement(LinkSummary, { text }))).toContain(text);
});
