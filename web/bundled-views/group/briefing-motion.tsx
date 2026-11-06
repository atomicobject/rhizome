import type { ChangeLine } from "./briefing-activity.tsx";

export function commonMotionMetadata(lines: readonly Pick<ChangeLine, "type" | "folder">[]) {
  const counts = new Map<string, { line: (typeof lines)[number]; count: number }>();

  for (const line of lines) {
    const key = `${line.type.toLowerCase()}:${line.folder}`;
    const entry = counts.get(key);
    counts.set(key, { line, count: (entry?.count ?? 0) + 1 });
  }

  return [...counts.values()].find((entry) => entry.count > lines.length / 2)?.line;
}

export function MotionCount({
  count,
  metadata,
}: {
  count: number;
  metadata: ReturnType<typeof commonMotionMetadata>;
}) {
  return (
    <>
      {count}
      {metadata && (
        <span className="gv-motion-metadata">
          {" "}
          · {metadata.type}
          {metadata.folder && (
            <>
              {" "}
              · <span className="gv-path">{metadata.folder}/</span>
            </>
          )}
        </span>
      )}
    </>
  );
}
