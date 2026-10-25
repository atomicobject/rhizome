/** Internal fallback identities support navigation but are not public types. */
export function publicTypeName(name: string | null | undefined): string {
  const value = name?.trim() || "";

  return value === "_FallbackNote" || value === "_FallbackSection" ? "" : value;
}

/**
 * Format an ontology type name as a URL slug (`UserStory` → `user-story`).
 * Display text uses the schema label from `useTypeLabel` instead.
 *
 * Rules:
 * - Insert a hyphen between a lowercase/digit and an uppercase letter.
 * - Insert a hyphen between a run of uppercase letters and the next
 *   uppercase-followed-by-lowercase sequence (so "HTTPServer" →
 *   "http-server", not "h-t-t-p-server").
 * - Leave an existing hyphen, underscore, or space as a word boundary
 *   (normalized to hyphen).
 * - Lowercase the final result so slugs match regardless of source casing.
 */
export function formatTypeName(name: string): string {
  if (!name) return "";

  return name
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1-$2")
    .replace(/[\s_]+/g, "-")
    .toLowerCase();
}
