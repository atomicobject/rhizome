import type { CSSProperties } from "react";

type DiffLine = {
  kind: "" | " is-hunk" | " is-add" | " is-remove";
  oldLine?: number;
  newLine?: number;
  text: string;
};

/** A unified diff with old/new line numbers and tinted added and removed lines. */
export function UnifiedDiff({ diff, className = "" }: { diff: string; className?: string }) {
  const lines = diffLines(diff);

  const widest = Math.max(
    0,
    ...lines.map((line) => Math.max(line.oldLine ?? 0, line.newLine ?? 0)),
  );

  const style: CSSProperties & { "--unified-diff-gutter": string } = {
    "--unified-diff-gutter": `${String(widest).length}ch`,
  };

  return (
    <pre className={`unified-diff ${className}`.trim()} style={style} tabIndex={0}>
      {lines.map((line, index) => (
        // Lines have no identity beyond their position in this static text.
        <span key={index} className={`unified-diff__line${line.kind}`}>
          <span className="unified-diff__num" aria-hidden="true">
            {line.oldLine ?? ""}
          </span>
          <span className="unified-diff__num" aria-hidden="true">
            {line.newLine ?? ""}
          </span>
          {line.text || " "}
        </span>
      ))}
    </pre>
  );
}

const HUNK_HEADER = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/** Parse a line-based unified diff into rows numbered from each hunk header. */
function diffLines(diff: string): DiffLine[] {
  let oldLine = 0;
  let newLine = 0;
  let inHunk = false;

  return diff.split("\n").flatMap((line): DiffLine[] => {
    const header = HUNK_HEADER.exec(line);

    if (header) {
      inHunk = true;
      // An empty range names the line before it, so numbering starts after it.
      oldLine = Math.max(Number(header[1]), 1);
      newLine = Math.max(Number(header[2]), 1);

      return [{ kind: " is-hunk", text: "⋯" }];
    }

    // File headers precede the first hunk; the container already names the file.
    if (!inHunk) return [];

    if (line.startsWith("\\")) return [{ kind: " is-hunk", text: line }];

    switch (line[0]) {
      case "+":
        return [{ kind: " is-add", newLine: newLine++, text: line }];
      case "-":
        return [{ kind: " is-remove", oldLine: oldLine++, text: line }];
      case " ":
        return [{ kind: "", oldLine: oldLine++, newLine: newLine++, text: line }];
      default:
        return [];
    }
  });
}
