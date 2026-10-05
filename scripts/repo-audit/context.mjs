import path from "node:path";
import { createHash } from "node:crypto";
import { documentMetadata } from "./triggers.mjs";

export const statusHeading = (s) =>
  /^(?:Status|Delivery Tracking|Spec Coverage Checklist)$/i.test(s.heading || "");
const navigationHeading =
  /^(?:Related(?: .*)?|Deep docs|See also|References|Contents|Table of contents)$/i;

export function substantive(source) {
  if (source.kind === "note" && navigationHeading.test(source.heading || "")) return false;
  const body = source.content
    .replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, "")
    .replace(/^#{1,6}\s.*$/gm, "")
    .replace(/^\s*(?:[-*]\s*)?(?:none\.?|n\/a|tbd)\s*$/gim, "")
    .trim();
  return /[a-z]{3,}/i.test(body);
}

// Bounded source hints, not an AST or ontology projection. Unknown scope stays unknown.
export function enrichChunks(chunks, content) {
  const lines = content.split("\n");
  const document = documentMetadata(chunks[0]?.path || "", content);
  const sourceHash = createHash("sha256").update(content).digest("hex");
  const declarations = [];
  const headings = [];
  let fence;
  for (let i = 0; i < lines.length; i++) {
    const f = /^\s*(`{3,}|~{3,})/.exec(lines[i]);
    if (f) {
      if (!fence) fence = f[1][0];
      else if (f[1][0] === fence) fence = undefined;
      continue;
    }
    if (fence) continue;
    const h = /^(#{1,6})\s+(.+)/.exec(lines[i]);
    if (h) headings.push({ line: i + 1, level: h[1].length, title: h[2] });
    if (
      /^(?:func\s|(?:async\s+)?def\s|(?:export\s+)?(?:async\s+)?function\s|(?:export\s+)?class\s|(?:export\s+)?(?:const|let)\s+\w+\s*=)/.test(
        lines[i],
      )
    )
      declarations.push({ line: i + 1, signature: lines[i].slice(0, 300) });
  }
  return chunks.map((chunk) => {
    const ancestors = [];
    for (const h of headings.filter((h) => h.line <= chunk.startLine)) {
      while (ancestors.length && ancestors.at(-1).level >= h.level) ancestors.pop();
      ancestors.push(h);
    }
    const declaration = declarations.filter((d) => d.line <= chunk.startLine).at(-1);
    return {
      ...chunk,
      sourceHash,
      context: {
        document,
        headingPath: ancestors.map((h) => h.title),
        opening: lines.slice(0, 28).join("\n").slice(0, 1800),
        buildConditions: lines.slice(0, 20).filter((l) => /^\/\/(?:go:build| \+build)/.test(l)),
        enclosingDeclarationHint: declaration,
        declarationExcerpt: declaration
          ? lines
              .slice(declaration.line - 1, Math.min(declaration.line + 23, chunk.startLine - 1))
              .join("\n")
              .slice(0, 1600)
          : undefined,
        precedingLines: lines
          .slice(Math.max(0, chunk.startLine - 9), chunk.startLine - 1)
          .join("\n")
          .slice(0, 900),
        limitations:
          "Declaration hint is lexical and may be out of scope. Open the enclosing symbol/test and inspect complete setup before verifying a defect.",
      },
    };
  });
}

export function focusExclusion(chunk) {
  if (!substantive(chunk)) return "empty_or_navigation";
  const m = chunk.context?.document;
  if (
    m?.generated ||
    m?.generatedRanges?.some(([a, b]) => chunk.startLine <= b && chunk.endLine >= a)
  )
    return "generated";
  if (["superseded", "archived", "retired"].includes(m?.status)) return "retired";
  if (m?.datedAnalysis) return "dated_analysis";
  if (
    chunk.kind === "code" &&
    /(?:_test\.go|(?:^|\/)(?:test|tests|testdata|fixtures)\/|\.(?:test|spec)\.[cm]?[jt]sx?$)/i.test(
      chunk.path,
    )
  )
    return "test_or_fixture_focus";
  if (m?.effort && !statusHeading(chunk)) return "effort_evidence_only";
  if (
    chunk.kind === "note" &&
    /^(?:docs\/performance\/|docs\/reference\/(?:analysis|coverage-catalogs)\/)/.test(chunk.path)
  )
    return "historical_evidence_only";
}

// Deterministic strata prevent a pilot from being only the first alphabetic paths.
export function sampleFocus(chunks, limit, seed = "pilot-v1") {
  if (!limit) return chunks;
  const key = (s) => createHash("sha256").update(`${seed}:${s.path}:${s.startLine}`).digest("hex");
  const buckets = [
    chunks.filter(statusHeading),
    chunks.filter((c) => c.kind === "note" && !statusHeading(c)),
    chunks.filter((c) => c.kind === "code"),
  ].map((xs) =>
    xs
      .map((s) => [key(s), s])
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map((x) => x[1]),
  );
  const out = [];
  while (out.length < limit && buckets.some((b) => b.length))
    for (const bucket of buckets) if (bucket.length && out.length < limit) out.push(bucket.shift());
  return out;
}

export function contextualCandidates(focus, shortlist, chunksByPath, exploration = false) {
  const sameFile = chunksByPath.get(focus.path) || [];
  if (!exploration && statusHeading(focus)) {
    const candidates = sameFile
      .filter(
        (c) =>
          c.id !== focus.id &&
          substantive(c) &&
          /^(?:Actual Delivered|Execution Notes|Delivery Tracking|Status|Spec Coverage Checklist)$/i.test(
            c.heading || "",
          ),
      )
      .sort(
        (a, b) =>
          (/^Actual Delivered$/i.test(b.heading) ? 1 : 0) -
            (/^Actual Delivered$/i.test(a.heading) ? 1 : 0) || b.startLine - a.startLine,
      )
      .slice(0, 4);
    return {
      candidates,
      candidateCoverage: {
        method: "same-document-status",
        selectedCount: candidates.length,
        limitation: "Bounded status and execution excerpts, not proof of complete delivery.",
      },
    };
  }
  const owners = [];
  let directory = path.posix.dirname(focus.path);
  while (true) {
    for (const name of ["CONTEXT.md", "README.md"]) {
      const file = path.posix.join(directory, name);
      const candidate = chunksByPath.get(file)?.find((c) => c.id !== focus.id && substantive(c));
      if (candidate && !owners.some((c) => c.path === file))
        owners.push({ ...candidate, connectionEvidence: "ancestor directory guidance" });
    }
    if (owners.length >= 2 || directory === ".") break;
    directory = path.posix.dirname(directory);
  }
  const candidates = [...owners.slice(0, 2), ...shortlist.candidates]
    .filter((c, i, all) => substantive(c) && all.findIndex((x) => x.id === c.id) === i)
    .slice(0, exploration ? 6 : 4);
  return {
    candidates,
    candidateCoverage: {
      ...shortlist.candidateCoverage,
      selectedCount: candidates.length,
      limitation:
        "Ancestor guidance and lexical/explicit file-link shortlist. Authored context is supplied; canonical coderef, glob and semantic resolution is unavailable, not evidence of a missing binding.",
    },
  };
}
