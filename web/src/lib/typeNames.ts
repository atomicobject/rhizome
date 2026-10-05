/** Internal fallback identities support navigation but are not public types. */
export function publicTypeName(name: string | null | undefined): string {
  const value = name?.trim() || "";

  return value === "_FallbackNote" || value === "_FallbackSection" ? "" : value;
}

/**
 * Format an ontology type name for display in a type badge / pill.
 *
 * The ontology stores type names in PascalCase (e.g. `UserStory`,
 * `UserStoriesSection`, `ProductSpec`) so they can be referenced in the
 * GraphQL schema. For display purposes we prefer kebab-case so the badge
 * reads as separate words at a glance — "USER-STORY" rather than the
 * word-mashed "USERSTORY". The CSS layer still handles the uppercase
 * transform; this helper only inserts the word boundaries.
 *
 * Rules:
 * - Insert a hyphen between a lowercase/digit and an uppercase letter.
 * - Insert a hyphen between a run of uppercase letters and the next
 *   uppercase-followed-by-lowercase sequence (so "HTTPServer" →
 *   "http-server", not "h-t-t-p-server").
 * - Leave an existing hyphen, underscore, or space as a word boundary
 *   (normalized to hyphen).
 * - Lowercase the final result so CSS `text-transform: uppercase` makes
 *   the display consistent regardless of source casing.
 */
export function formatTypeName(name: string): string {
  if (!name) return "";

  return name
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1-$2")
    .replace(/[\s_]+/g, "-")
    .toLowerCase();
}
