import { describe, expect, it } from "vitest";
import {
  buildNoteWebHref,
  convertWikilinks,
  resolveLinkTarget,
  resolveRenderedLinkTarget,
  resolveKnownRenderedLinkTarget,
  stripFrontmatter,
} from "./content";
import { decodeURLFragment } from "./noteDeepLink";

describe("content utilities", () => {
  it("converts wikilinks into rhizome note links", () => {
    expect(convertWikilinks("See [[notes/task-flow|Task Flow]]")).toContain(
      "[Task Flow](rhizome://note/notes%2Ftask-flow)",
    );
  });

  it("leaves wikilinks inside code literal", () => {
    const source = "Use `[[Note]]` or\n\n```\n[[Other]]\n```\n\nthen [[Real]].";
    expect(convertWikilinks(source)).toBe(
      "Use `[[Note]]` or\n\n```\n[[Other]]\n```\n\nthen [Real](rhizome://note/Real).",
    );
  });

  it("closes fences in CRLF notes", () => {
    expect(convertWikilinks("```\r\n[[Kept]]\r\n```\r\n[[Link]]")).toBe(
      "```\r\n[[Kept]]\r\n```\r\n[Link](rhizome://note/Link)",
    );
  });

  it("keeps longer fences open past shorter inner fences", () => {
    const source =
      "````md\n```\n[[Inside]]\n```\n[[Still inside]]\n````\n[[Out]] and ``a ` [[Span]]``";

    expect(convertWikilinks(source)).toBe(
      "````md\n```\n[[Inside]]\n```\n[[Still inside]]\n````\n[Out](rhizome://note/Out) and ``a ` [[Span]]``",
    );
    expect(convertWikilinks("~~~~\n~~~\n[[Kept]]\n~~~~\n[[Link]]")).toBe(
      "~~~~\n~~~\n[[Kept]]\n~~~~\n[Link](rhizome://note/Link)",
    );
  });

  it("strips yaml frontmatter from note content", () => {
    const content = `---
title: Demo
---

# Body
`;

    expect(stripFrontmatter(content)).toBe("# Body\n");
  });

  it("resolves explicit markdown targets from the file link list", () => {
    const target = resolveLinkTarget("task-flow", {
      path: "notes/product-brief.md",
      kind: "note",
      links: [{ target: "notes/task-flow.md", text: "task-flow", kind: "wikilink" }],
    });

    expect(target).toBe("notes/task-flow.md");
  });

  it("preserves block anchors when resolving rendered note links", () => {
    const target = resolveRenderedLinkTarget("rhizome://note/notes%2Fspec.md%23%5EUS-001", {
      path: "notes/spec.md",
      title: "Spec",
      links: [
        {
          target: "notes/spec.md#^US-001",
          text: "notes/spec.md#^US-001",
          kind: "wikilink",
          anchor: "^US-001",
        },
      ],
    });

    expect(target).toBe("notes/spec.md#^US-001");
  });

  it("keeps rendered wikilinks internal without an indexed link list", () => {
    const target = resolveRenderedLinkTarget(
      "rhizome://note/ontology-browser-workspace%23%5ESPEC-0014-US10-AC1",
      null,
    );

    expect(target).toBe("ontology-browser-workspace#^SPEC-0014-US10-AC1");
  });

  it.each(["notes/100%.md#^progress", "notes/%GG.md", "notes/%E0%A4.md"])(
    "preserves malformed note URI payload %s",
    (raw) => {
      const href = `rhizome://note/${raw}`;
      expect(resolveLinkTarget(href, null)).toBe(raw);
      expect(resolveRenderedLinkTarget(href, { path: "index.md", title: "Index", links: [] })).toBe(
        raw,
      );
      expect(
        resolveKnownRenderedLinkTarget(href, { path: "index.md", title: "Index", links: [] }),
      ).toBeNull();
      expect(
        resolveKnownRenderedLinkTarget(href, {
          path: "index.md",
          title: "Index",
          links: [{ target: "canonical.md#^node", text: raw, kind: "wikilink" }],
        }),
      ).toBe("canonical.md#^node");
    },
  );

  it("builds copyable web urls for note targets on the current base view", () => {
    window.history.replaceState({}, "", "/notes/product-spec?view=table");
    expect(buildNoteWebHref("docs/spec.md#^story-a")).toBe(
      "/notes/product-spec?view=table&note=docs%2Fspec.md#%5Estory-a",
    );
  });

  it("keeps an HTML document query separate from application routing", () => {
    window.history.replaceState({}, "", "/notes?view=table");
    expect(buildNoteWebHref("reports/prototype.html?mode=wide#chart")).toBe(
      "/notes?view=table&note=reports%2Fprototype.html&noteQuery=mode%3Dwide#chart",
    );
  });

  it("percent-encodes a heading fragment containing a literal percent", () => {
    window.history.replaceState({}, "", "/notes");
    const href = buildNoteWebHref("docs/spec.md#20%20off");
    expect(href).toBe("/notes?note=docs%2Fspec.md#20%2520off");
    expect(decodeURLFragment("#20%2520off")).toBe("#20%20off");
  });

  it("keeps anchored note links valid when only the base note is indexed", () => {
    const target = resolveRenderedLinkTarget("rhizome://note/notes%2Fspec%23%5EUS-001", {
      path: "notes/plan.md",
      title: "Plan",
      links: [
        {
          target: "notes/spec.md",
          text: "notes/spec",
          kind: "wikilink",
        },
      ],
    });

    expect(target).toBe("notes/spec.md#^US-001");
  });

  it("preserves explicit HTML extensions in rendered-link fallbacks", () => {
    const rendered = {
      path: "reports/index.html",
      title: "Reports",
      links: [],
    };

    expect(resolveRenderedLinkTarget("reports/status.html", rendered)).toBe("reports/status.html");
    expect(resolveRenderedLinkTarget("reports/status.HTM#results", rendered)).toBe(
      "reports/status.HTM#results",
    );
    expect(resolveKnownRenderedLinkTarget("status.html", rendered)).toBeNull();
  });

  it("retains Markdown shorthand for extensionless and dotted note titles", () => {
    const rendered = {
      path: "notes/index.md",
      title: "Notes",
      links: [],
    };

    expect(resolveRenderedLinkTarget("Architecture.Draft", rendered)).toBe("Architecture.Draft.md");
    expect(resolveRenderedLinkTarget("ASP.NET#history", rendered)).toBe("ASP.NET.md#history");
  });
});
