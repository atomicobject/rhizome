/**
 * Display labels for schema identifiers and note titles.
 *
 * The API hands back identifiers (`nextStep`, `IPOpportunity`, `in_progress`)
 * where a schema supplies no label, and section headings that keep their
 * authored Markdown. These helpers turn both into reader-facing text. They are for
 * display only: never feed their output back into an edit.
 */

/**
 * Sentence-case an identifier: `nextStep` → "Next step", `IPOpportunity` →
 * "IP opportunity", `in_progress` → "In progress". Text that already contains
 * a space is an authored label and passes through unchanged.
 */
export function humanizeName(name: string | null | undefined): string {
  const value = (name ?? "").trim().replace(/^(frontmatter|inline)\./, "");

  if (!value || /\s/.test(value)) return value;

  const words = value
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .split(/[\s_\-.]+/)
    .filter(Boolean)
    // Acronyms keep their capitals; ordinary words read in sentence case.
    .map((word) => (/^[A-Z0-9]{2,}$/.test(word) ? word : word.toLowerCase()));

  if (!words.length) return value;
  words[0] = words[0].charAt(0).toUpperCase() + words[0].slice(1);

  return words.join(" ");
}

/**
 * Strip inline Markdown from a heading. Note titles already arrive as plain
 * text from the server; this keeps headings and older payloads readable:
 * `[[Tech Lead Sync]] 2022-05-12` → "Tech Lead Sync 2022-05-12",
 * `**AI in Healthcare**` → "AI in Healthcare".
 */
export function displayTitle(title: string | null | undefined): string {
  return (title ?? "")
    .replace(/!?\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g, (_match, target: string, label?: string) =>
      (label ?? target.split("#")[0]).trim(),
    )
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/(\*\*|__|~~|==)(.+?)\1/g, "$2")
    .replace(/(^|[\s(])[*_]([^*_\s][^*_]*?)[*_](?=$|[\s).,:;!?])/g, "$1$2")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/^#{1,6}\s+/, "")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * Label for an enum value. The API echoes the raw value as the label when the
 * schema declares none, so an echoed label is treated as missing.
 */
export function enumLabel(value: string, label?: string | null): string {
  return label && label !== value ? label : humanizeName(value);
}
