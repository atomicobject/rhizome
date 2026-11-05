import type { ChangeLine } from "./briefing-activity.tsx";

export function commonMotionMetadata(lines: readonly Pick<ChangeLine, "type" | "folder">[]) {
  const first = lines[0];

  return first && lines.every((line) => line.type === first.type && line.folder === first.folder)
    ? first
    : undefined;
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
