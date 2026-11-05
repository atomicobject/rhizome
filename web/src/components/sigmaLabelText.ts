/** Wrap note titles before shortening, keeping the distinguishing ending. */
export function noteLabelLines(
  text: string,
  measure: (text: string) => number,
  maxWidth = 220,
): string[] {
  text = text.replaceAll("...", "…");

  if (measure(text) <= maxWidth) return [text];
  const lineWidth = Math.min(maxWidth, measure(text) / 2);
  let split = 1;

  while (split < text.length && measure(text.slice(0, split + 1)) <= lineWidth) split++;
  const space = Math.max(text.lastIndexOf(" ", split), text.lastIndexOf("-", split) + 1);

  if (space > split / 2) split = space;
  const first = text.slice(0, split).trim();
  const rest = text.slice(split).trim();

  if (measure(rest) <= maxWidth) return [first, rest];

  for (let keep = rest.length - 1; keep > 0; keep--) {
    const head = Math.ceil(keep / 2),
      tail = Math.floor(keep / 2);

    const last = `${rest.slice(0, head)}…${tail ? rest.slice(-tail) : ""}`;

    if (measure(last) <= maxWidth) return [first, last];
  }

  return [first, "…"];
}

/** The selected note stays below the focus, inside the gap between its lanes. */
export function selectedNoteSpot(x: number, y: number, r: number, width: number, size: number) {
  const left = x - width / 2;
  const baseline = y + r + 16;

  return {
    x: left,
    y: baseline,
    box: [left - 4, baseline - size, left + width + 4, baseline + 4] satisfies [
      number,
      number,
      number,
      number,
    ],
  };
}
