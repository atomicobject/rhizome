import { readPassage, readVaultText } from "./passage.ts";
import type { ReadText } from "./passage.ts";
import { citations, sourceText } from "./knowledge.ts";
import type { Knowledge, Source } from "./knowledge.ts";
import { safeLocalPath } from "./locators.ts";

export async function loadSourceDocument(
  source: Source,
  data: Knowledge,
  readText: ReadText = readVaultText,
) {
  const text = sourceText(source);
  const slots = await Promise.all(
    text.paths.map(async (path, index) => {
      if (!safeLocalPath(path) || !/\.(md|txt)$/i.test(path))
        return { slot: index + 1, path, status: "Not an eligible local text file" };
      try {
        const content = await readText(path);
        return {
          slot: index + 1,
          path,
          status: content === undefined ? "Unavailable" : "Available",
          content,
        };
      } catch {
        return { slot: index + 1, path, status: "Read refused or failed" };
      }
    }),
  );
  const files = slots
    .filter((s) => s.content !== undefined)
    .map((s) => ({ path: s.path, content: s.content ?? "", fallback: false }));
  // A read refusal must never enable fallback to a different source.
  if (
    !files.length &&
    !slots.some((s) => s.status === "Read refused or failed") &&
    text.body.trim()
  )
    files.push({ path: source.path, content: text.body, fallback: true });
  const citers = await Promise.all(
    data.assertion.flatMap((a) =>
      citations(a, data.source)
        .filter((c) => c.source?.path === source.path)
        .map(async (c) => ({
          a,
          citation: c,
          passage: await readPassage(text, c.locator, c.raw, async (path) => {
            const slot = slots.find((s) => s.path === path);
            if (slot?.status === "Read refused or failed")
              throw new Error("Source read refused or failed");
            return slot?.content;
          }),
        })),
    ),
  );
  return { slots, files, citers };
}
