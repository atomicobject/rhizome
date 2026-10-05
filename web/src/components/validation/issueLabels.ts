import type { ValidationDiagnostic } from "../../api/types";
import { isStringArray } from "../../api/parse";
import { humanizeName } from "../../lib/labels";

const CHECK_LABELS = new Map([
  ["broken_links", "Broken links"],
  ["ontology", "Ontology"],
  ["code_frontmatter", "Code frontmatter"],
  ["code_anchors", "Code anchors"],
  ["identifiers", "Identifiers"],
]);

const ISSUE_LABELS = new Map([
  ["broken_link", "Broken link"],
  ["inverse_mismatch", "Missing inverse link"],
  ["declared_type_mismatch", "Declared type mismatch"],
  ["unknown_declared_type", "Unknown declared type"],
  ["type_ambiguous", "Ambiguous type"],
  ["missing_required_field", "Missing required field"],
  ["required_field_missing", "Missing required field"],
  ["field_shape_mismatch", "Field shape mismatch"],
  ["field_type_mismatch", "Field type mismatch"],
  ["broken_note_link", "Broken note link"],
  ["link_target_missing", "Link target missing"],
  ["wrong_target_type", "Wrong target type"],
  ["empty_required_section", "Empty required section"],
  ["duplicate_section", "Duplicate section"],
  ["schema_invalid", "Invalid schema"],
  ["schema_state_decode_error", "Schema decode error"],
  ["missing_required_section", "Missing required section"],
  ["wrong_section_level", "Wrong section level"],
  ["identifier_not_in_aliases", "Identifier not in aliases"],
  ["duplicate_preferred_identifier", "Duplicate preferred identifier"],
]);

export type IssueHelp = { meaning: string; fix: string };

const REQUIRED_FIELD_HELP: IssueHelp = {
  meaning: "A field the type marks required has no value.",
  fix: "Fill in the field, or make it optional in the schema.",
};

// Backticks mark inline code; render with IssueHelpText.
const ISSUE_HELP = new Map<string, IssueHelp>([
  [
    "type_ambiguous",
    {
      meaning:
        "The note matches the selectors of more than one type and none is more specific, so it resolves to no type.",
      fix: "Add `type: <Type>` to the note's frontmatter to choose one, or narrow a type's `@node` selectors so only one matches.",
    },
  ],
  [
    "broken_note_link",
    {
      meaning: "A wikilink points to a note that does not exist in the vault.",
      fix: "Create the target note or retarget the link. When a likely target exists, a grouped retarget repair is offered.",
    },
  ],
  [
    "unknown_declared_type",
    {
      meaning: "The note's `type:` frontmatter names a type the ontology does not define.",
      fix: "Correct the `type:` value, or add the type to the schema.",
    },
  ],
  [
    "declared_type_mismatch",
    {
      meaning: "The note declares a `type:` that its path and content selectors do not match.",
      fix: "Change `type:` to a type whose selectors match, or widen that type's `@node` selectors.",
    },
  ],
  [
    "link_target_missing",
    {
      meaning: "A relation field value does not resolve to any note.",
      fix: "Point the value at an existing note, or create the target note.",
    },
  ],
  [
    "wrong_target_type",
    {
      meaning: "A relation field links to a note whose type the field does not accept.",
      fix: "Link to a note of the expected type, or fix the target note's type.",
    },
  ],
  ["missing_required_field", REQUIRED_FIELD_HELP],
  ["required_field_missing", REQUIRED_FIELD_HELP],
  [
    "inverse_mismatch",
    {
      meaning: "A relation is set on one note but the declared inverse is missing on the target.",
      fix: "Add the inverse link on the target note.",
    },
  ],
]);

export function checkLabel(check: string) {
  return CHECK_LABELS.get(check) ?? check.replaceAll("_", " ");
}

export function issueLabel(code?: string) {
  return ISSUE_LABELS.get(code ?? "") ?? (code ? code.replaceAll("_", " ") : "Validation issue");
}

export function issueHelp(code?: string): IssueHelp | null {
  return ISSUE_HELP.get(code ?? "") ?? null;
}

/** The short fact that tells one issue apart from others of the same kind. */
export function issueSummary(diagnostic: ValidationDiagnostic): string {
  const types = candidateTypes(diagnostic);

  if (types.length) return types.map(humanizeName).join(" · ");

  if (diagnostic.target) {
    return diagnostic.field
      ? `${diagnostic.field} → ${diagnostic.target}`
      : `[[${diagnostic.target}]]`;
  }

  return diagnostic.field ?? "";
}

/** The types an ambiguous-type finding could resolve to, from its evidence. */
export function candidateTypes(diagnostic: ValidationDiagnostic): string[] {
  const types = diagnostic.evidence?.candidateTypes;

  return diagnostic.code === "type_ambiguous" && isStringArray(types) ? types : [];
}

const SAFETY_LABELS = new Map([
  ["safe", "Safe"],
  ["needs_confirmation", "Needs confirmation"],
  ["agent_required", "Agent task"],
]);

export function safetyLabel(safety: string) {
  return SAFETY_LABELS.get(safety) ?? safety.replaceAll("_", " ");
}
