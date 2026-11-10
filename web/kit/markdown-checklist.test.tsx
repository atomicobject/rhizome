import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import { Markdown, markdownChecklist } from "./index";

it.each([
  ["- [x] Done\n  - [ ] Pending", 1, 2],
  ["    - [x] Code example", 0, 0],
  ["- - -\n\n    - [x] Code example", 0, 0],
  ["* * *\n\n    - [x] Code example", 0, 0],
  ["```md\n- [x] Code example\n```", 0, 0],
  ["> - [x] Recorded quoted task", 1, 1],
  ["Text [x] claim\n- [ ] Real task", 0, 1],
  ["- Context\n    - [x] Nested\n\n          - [x] Code", 1, 1],
  ["- Context\n    - [x] Nested\n\n        - [x] Deeper task", 2, 2],
  ["- [x] ", 0, 0],
] as const)("counts rendered task items in %j", (source, checked, total) => {
  expect(markdownChecklist(source)).toEqual({ checked, total });
  const view = render(<Markdown skipHtml>{source}</Markdown>);
  const boxes = [...view.container.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
  expect(boxes).toHaveLength(total);
  expect(boxes.filter((box) => box.checked)).toHaveLength(checked);
  view.unmount();
});
