import { render } from "pele";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { mermaid } from "./mermaid";

const title = 'V/TO "and" Q4: rocks, (draft) [x] #1 a&b <i>';

describe("mermaid escape helpers", () => {
  beforeAll(() => {
    // Use Pele's real font metrics because jsdom has no canvas text measurement.
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
  });

  afterAll(() => vi.restoreAllMocks());

  it.each([
    ["flowchart", `flowchart LR\n a["${mermaid.text(title)}"]`],
    ["timeline", `timeline\n ${mermaid.text(title)} : event`],
    ["gantt", `gantt\n dateFormat YYYY-MM-DD\n ${mermaid.text(title)} :a, 2026-01-01, 1d`],
    ["xychart", `xychart-beta\n x-axis ["${mermaid.text(title)}"]\n bar [1]`],
    ["sankey", `sankey-beta\n${mermaid.csv(title)},Other,1`],
  ])("preserves hostile label text in %s", (type, source) => {
    const result = render(source, { images: false });
    expect(result.type).toBe(type);

    const document = new DOMParser().parseFromString(result.svg, "image/svg+xml");
    expect(document.querySelector("parsererror")).toBeNull();

    const labels = Array.from(document.querySelectorAll("text"), (text) =>
      Array.from(text.childNodes, (node) => node.textContent)
        .join(" ")
        .replace(/\s+/g, " "),
    );

    expect(labels.some((label) => label.includes(title))).toBe(true);
  });

  it("escapes every reserved label character without interpreting existing entities", () => {
    expect(mermaid.text('&"#<>:;,[](){}|')).toBe(
      "#38;#34;#35;#60;#62;#58;#59;#44;#91;#93;#40;#41;#123;#125;#124;",
    );
    expect(mermaid.text("#34;")).toBe("#35;34#59;");
  });

  it("preserves a leading backtick without starting a Mermaid Markdown string", () => {
    const label = "`rzm init` fix";
    expect(mermaid.text(label)).toBe("#96;rzm init#96; fix");
    const result = render(`flowchart LR\n a["${mermaid.text(label)}"]`);
    const document = new DOMParser().parseFromString(result.svg, "image/svg+xml");
    expect(document.querySelector("text")?.textContent).toBe(label);
  });

  it("quotes CSV fields and doubles embedded quotes", () => {
    expect(mermaid.csv('a,"b"')).toBe('"a,""b"""');
    expect(mermaid.csv("")).toBe('""');
  });

  it("uses stable FNV-1a hashes", () => {
    expect(mermaid.id("")).toBe(`n${(0x811c9dc5).toString(36)}`);
    expect(mermaid.id("hello")).toBe(`n${(0x4f9f2cab).toString(36)}`);
    expect(mermaid.id("Notes/Planning.md")).toBe(mermaid.id("Notes/Planning.md"));
    expect(mermaid.id("Notes/Planning.md")).not.toBe(mermaid.id("Notes/Other.md"));
  });

  it.each(["", "123", "Notes/My note [draft].md", "日本語/🌱.md", '\n"; click A href #x'])(
    "creates syntax-safe ids from %j",
    (key) => {
      expect(mermaid.id(key)).toMatch(/^n[0-9a-z]+$/);
    },
  );
});
