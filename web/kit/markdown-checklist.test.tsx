import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import { Markdown, markdownChecklist, convertWikilinks } from "@rhizome/kit/markdown";

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

it("preserves authored links and code through the public Markdown entry", () => {
  const source = "[[notes/Plan#Scope|Plan]] and `[[literal]]`";

  const converted = convertWikilinks(source);

  const view = render(
    <Markdown skipHtml urlTransform={(url) => url}>
      {converted}
    </Markdown>,
  );

  expect(view.getByRole("link", { name: "Plan" }).getAttribute("href")).toBe(
    "rhizome://note/notes%2FPlan%23Scope",
  );
  expect(view.container.querySelector("code")?.textContent).toBe("[[literal]]");
});
