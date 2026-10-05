import * as fs from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { enrichChunks } from "./context.mjs";
import { documentMetadata } from "./triggers.mjs";

export const hash = (value) =>
  createHash("sha256")
    .update(typeof value === "string" ? value : JSON.stringify(value))
    .digest("hex");
const noteExtensions = new Set([".md", ".markdown"]);
const codeExtensions = new Set([
  ".go",
  ".js",
  ".mjs",
  ".cjs",
  ".ts",
  ".tsx",
  ".jsx",
  ".py",
  ".cs",
  ".java",
  ".rs",
  ".rb",
  ".php",
  ".c",
  ".h",
  ".cpp",
  ".hpp",
  ".sh",
  ".sql",
  ".swift",
  ".kt",
  ".vue",
  ".svelte",
  ".astro",
]);

// Ignore directories before descending and check files before opening them.
// Symlinks are recorded but never followed. Coverage includes excluded subtrees
// as subtrees, not invented counts of their unseen descendants.
export async function inventory(rzm, root, outputDir) {
  const files = [],
    chunks = [];
  async function visit(directory) {
    let entries;
    try {
      entries = await fs.readdir(path.join(root, directory), { withFileTypes: true });
    } catch (error) {
      files.push({
        path: directory,
        kind: "directory",
        status: "read_failed",
        error: error.message,
      });
      return;
    }
    entries.sort((a, b) => a.name.localeCompare(b.name));
    for (let offset = 0; offset < entries.length; offset += 500) {
      const page = entries.slice(offset, offset + 500);
      const inputs = page.map((entry) => ({
        path: path.posix.join(directory, entry.name),
        isDir: entry.isDirectory(),
      }));
      const checked = await rzm.checkPaths({ paths: inputs });
      if (!checked.ok || checked.payload?.paths?.length !== inputs.length)
        throw new Error("Path checking failed or returned incomplete coverage");
      for (let i = 0; i < page.length; i++) {
        const entry = page[i],
          relative = inputs[i].path,
          decision = checked.payload.paths[i];
        if (decision.input !== relative) throw new Error("Path check correlation mismatch");
        const record = {
          path: relative,
          kind: entry.isDirectory() ? "directory" : "file",
          ignore: decision,
        };
        const absolute = path.join(root, relative);
        if (relative === ".git" || relative === ".rhizome/audits" || absolute === outputDir) {
          files.push({ ...record, status: "administrative" });
          continue;
        }
        if (entry.isSymbolicLink()) {
          files.push({ ...record, status: "symlink_skipped" });
          continue;
        }
        if (decision.status !== "allowed") {
          files.push({ ...record, status: decision.status });
          continue;
        }
        if (entry.isDirectory()) {
          await visit(relative);
          continue;
        }
        if (!entry.isFile()) {
          files.push({ ...record, status: "unsupported" });
          continue;
        }
        const extension = path.extname(relative).toLowerCase();
        const kind = noteExtensions.has(extension)
          ? "note"
          : codeExtensions.has(extension)
            ? "code"
            : undefined;
        if (!kind) {
          files.push({ ...record, status: "unsupported" });
          continue;
        }
        try {
          const stat = await fs.stat(absolute);
          if (stat.size > 1024 * 1024) {
            files.push({ ...record, status: "oversized", bytes: stat.size });
            continue;
          }
          const content = await fs.readFile(absolute, "utf8");
          if (content.includes("\0")) {
            files.push({ ...record, status: "binary" });
            continue;
          }
          const selected = splitSource(relative, kind, content);
          chunks.push(...enrichChunks(selected, content));
          files.push({
            ...record,
            kind,
            status: "inventoried",
            hash: hash(content),
            metadata: documentMetadata(relative, content),
            chunks: selected.length,
          });
        } catch (error) {
          files.push({ ...record, status: "read_failed", error: error.message });
        }
      }
    }
  }
  await visit("");
  return { files, chunks };
}

export function splitSource(file, kind, content) {
  const lines = content.split("\n"),
    chunks = [];
  let start = 0,
    heading = "",
    text = "",
    bytes = 0;
  const flush = (end) => {
    if (text.trim())
      chunks.push({
        id: hash([file, start + 1, end, text]),
        path: file,
        kind,
        heading,
        startLine: start + 1,
        endLine: end,
        content: text,
        contentTruncated: bytes > 7000,
      });
    start = end;
    text = "";
    bytes = 0;
  };
  for (let i = 0; i < lines.length; i++) {
    const match = kind === "note" && /^(#{1,6})\s+(.+)/.exec(lines[i]);
    if (match) {
      flush(i);
      heading = match[2];
    }
    if (i - start >= 100 || (bytes + Buffer.byteLength(lines[i]) > 7000 && text)) flush(i);
    // Extremely long lines retain an explicit truncation marker.
    bytes += Buffer.byteLength(lines[i]) + 1;
    text += Buffer.from(lines[i]).subarray(0, 7000).toString("utf8") + "\n";
  }
  flush(lines.length);
  return chunks;
}

const stopWords = new Set([
  "this",
  "that",
  "with",
  "from",
  "return",
  "const",
  "func",
  "string",
  "true",
  "false",
  "null",
  "error",
  "type",
  "import",
  "package",
  "test",
  "should",
  "function",
  "using",
  "have",
  "will",
  "when",
  "into",
  "then",
]);
export function terms(text) {
  return [
    ...new Set(
      (text.toLowerCase().match(/[a-z][a-z0-9_]{3,}/g) || []).filter(
        (word) => !stopWords.has(word),
      ),
    ),
  ];
}

// Candidate discovery is lexical plus explicit file links. The manifest covers
// every eligible file; candidate relationships are a bounded shortlist.
export function candidateIndex(chunks) {
  const postings = new Map(),
    byPath = new Map();
  for (let i = 0; i < chunks.length; i++) {
    for (const term of terms(chunks[i].path + " " + chunks[i].heading + " " + chunks[i].content)) {
      if (!postings.has(term)) postings.set(term, []);
      postings.get(term).push(i);
    }
    if (!byPath.has(chunks[i].path)) byPath.set(chunks[i].path, []);
    byPath.get(chunks[i].path).push(i);
  }
  return (focus) => {
    const scores = new Map(),
      linked = new Set();
    const query = terms(focus.path + " " + focus.heading + " " + focus.content)
      .filter((term) => postings.has(term))
      .sort((a, b) => postings.get(a).length - postings.get(b).length)
      .slice(0, 40);
    for (const term of query) {
      const hits = postings.get(term);
      if (hits.length > Math.max(1000, chunks.length / 4)) continue;
      const weight = Math.log(1 + chunks.length / hits.length);
      for (const i of hits)
        if (chunks[i].id !== focus.id) scores.set(i, (scores.get(i) || 0) + weight);
    }
    const refs = [
      ...focus.content.matchAll(/\[\[([^\]|#]+)[^\]]*\]\]|\]\(([^)#]+)(?:#[^)]*)?\)/g),
    ].map((match) => (match[1] || match[2]).trim());
    for (const reference of refs) {
      let target;
      try {
        target = decodeURIComponent(reference);
      } catch {
        continue;
      }
      for (const candidate of [
        target,
        target + ".md",
        path.posix.normalize(path.posix.join(path.posix.dirname(focus.path), target)),
        path.posix.normalize(path.posix.join(path.posix.dirname(focus.path), target + ".md")),
      ]) {
        for (const i of byPath.get(candidate) || []) {
          if (chunks[i].id !== focus.id) {
            linked.add(i);
            scores.set(i, (scores.get(i) || 0) + 1000);
          }
        }
      }
    }
    const ranked = [...scores].sort(
      (a, b) => b[1] - a[1] || chunks[a[0]].id.localeCompare(chunks[b[0]].id),
    );
    const selected = ["note", "code"].flatMap((kind) =>
      ranked.filter(([i]) => chunks[i].kind === kind).slice(0, 3),
    );
    return {
      candidates: selected.map(([i, score]) => ({
        ...chunks[i],
        existingLink: linked.has(i),
        retrievalScore: score,
      })),
      candidateCoverage: {
        method: "lexical-and-explicit-file-links",
        candidateCount: scores.size,
        selectedCount: selected.length,
        explicitLinkChunks: linked.size,
        omittedExplicitLinkChunks: [...linked].filter((i) => !selected.some(([j]) => i === j))
          .length,
        limitation:
          "Shortlist only. Heading anchors, coderefs, glob bindings and semantic matches are not resolved by this draft.",
      },
    };
  };
}
