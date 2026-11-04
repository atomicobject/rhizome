/** Longest snippet the server returns; it cuts there without an ellipsis. */
const SERVER_SNIPPET_LIMIT = 240;

const LIST_MARKER = /^\s*(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?/;

/** One run of a note excerpt: plain prose or an inline code span. */
export type ExcerptPart = { code: boolean; text: string };

/**
 * Turns a Markdown note snippet into one line of readable prose: source line
 * breaks become spaces, list items join with " · ", heading and emphasis
 * marks drop, and inline code stays marked as code. A snippet the server cut
 * mid-word ends at the last whole word with "…".
 */
export function noteExcerptParts(snippet: string): ExcerptPart[] {
  let text = snippet.trim();

  if (text.length >= SERVER_SNIPPET_LIMIT && !/[\s.!?:;,)\]"'`…]$/.test(text)) {
    const cut = text.search(/\s+\S*$/);

    if (cut > 0) text = text.slice(0, cut);
    text += "…";
  }

  let prose = "";

  for (const raw of text.split("\n")) {
    const item = LIST_MARKER.test(raw);

    const line = raw
      .replace(LIST_MARKER, "")
      .replace(/^\s*#{1,6}\s+/, "")
      .trim();

    if (!line) continue;

    if (prose) prose += item && !prose.endsWith(":") ? " · " : " ";
    prose += line;
  }

  prose = prose.replace(/(\*\*|__)(\S(?:.*?\S)?)\1/g, "$2");

  // Backticks pair into code spans; an unpaired one, cut off by the server, is dropped.
  const pieces = prose.split("`");

  if (pieces.length % 2 === 0) pieces[pieces.length - 2] += pieces.pop() ?? "";

  return pieces.flatMap((piece, index) => (piece ? [{ code: index % 2 === 1, text: piece }] : []));
}
