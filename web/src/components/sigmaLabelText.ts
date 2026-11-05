/** Wrap note titles before shortening, keeping the distinguishing ending. */
export function noteLabelLines(text: string, measure: (text: string) => number): string[] {
  text = text.replaceAll("...", "…");

  if (measure(text) <= 220) return [text];
  const lineWidth = Math.min(220, measure(text) / 2);
  let split = 1;

  while (split < text.length && measure(text.slice(0, split + 1)) <= lineWidth) split++;
  const space = Math.max(text.lastIndexOf(" ", split), text.lastIndexOf("-", split) + 1);

  if (space > split / 2) split = space;
  const first = text.slice(0, split).trim();
  const rest = text.slice(split).trim();

  if (measure(rest) <= 220) return [first, rest];

  for (let keep = rest.length - 1; keep > 0; keep--) {
    const head = Math.ceil(keep / 2),
      tail = Math.floor(keep / 2);

    const last = `${rest.slice(0, head)}…${tail ? rest.slice(-tail) : ""}`;

    if (measure(last) <= 220) return [first, last];
  }

  return [first, "…"];
}
