package ontology

import (
	"errors"
	"fmt"
)

// OntologyMaterializationVersion identifies the assessment and read-model
// algorithm that produced the persisted ontology projection. Increment this
// whenever unchanged source and schema inputs can produce different ontology
// evidence or rows.
//
// v4: materialized has_issues/type_ambiguous flags on assessment rows.
// v5: unanchored list-item structural fingerprints derive from owned content.
// v6: assessments record canonical catalog count and identity digest, including zero.
// v7: assessments bind catalog witnesses to the projected source content hash.
// v8: explicit non-Markdown file paths resolve in authored ontology links.
// v9: broken-link findings moved to the broken-links check; assessments no
// longer carry broken_note_link.
// v10: note titles derived from an H1 are plain text, so title-pattern
// findings and catalog labels no longer see inline Markdown.
// v11: uppercase AND/OR/NOT split selector expressions with fewer than two
// colons, so "Log AND tag:x" matches instead of reading as one property.
// v12: findings carry issue variants, and type_ambiguous records its
// candidate types.
// v13: dependency-affected sources reproject even when metadata includes them
// among direct candidates, repairing older stale link targets and findings.
// Version 14 materializes section-backed display summaries for compact reads.
// v15: full and incremental paths share embedded validation and provider-current
// type resolution; scalar and metadata-link normalization use one contract.
// Alias retirement invalidates unresolved dependencies before old aliases vanish.
// v16: note titles derived from an H1 drop a trailing Obsidian block ID.
// v17: embedded display labels retain the full headline without changing indexed titles or fields.
// v18: provider-neutral owners, record fields/presence, and relation occurrence evidence.
// v19: persisted RECORD binding context keeps scoped positional reads in the indexed logical namespace.
// v20: list-item neighbor edges include links on the item headline, and a declared type that
// differs only in case from its selector-matched type resolves to it.
// v21: structured providers, empty reference presence, cross-format references, and complete native chunk coverage.
// v22: scalar path fields reject structured leaves instead of formatting Go collections, and typed body-link
// candidates exclude incompatible basename claimants before uniqueness.
// v23: descendants of an invalid RECORD container are invalid too, so no catalog row or chunk
// names a parent without identity, and RECORD chunks take positional logical IDs from the composed
// record context.
// v24: RECORD references resolve projected file keys; property presence and wildcard membership
// and @admit preserve selected-product admission without authored identifier aliases.
// v25: quoted property matcher values compare frontmatter strings exactly, so notes admitted
// under the looser inline or case-folded comparison are retyped.
// v26: embedded records publish lexical search rows with their catalog nodes, so existing
// indexes republish records for search without embeddings.
// v27: relation occurrences name their authoring node and field, and a relation declared from
// both ends merges into one edge row instead of keeping whichever host wrote last.
// v28: an ATX heading needs a space, tab, or line end after its hashes, so a line-start
// tag such as #todo no longer becomes a heading node.
const OntologyMaterializationVersion = 28

// ErrFutureOntologyMaterialization means the persisted ontology projection was
// produced by a newer materializer than this binary understands.
var ErrFutureOntologyMaterialization = errors.New("future ontology materialization version")

func validateOntologyMaterializationVersion(version int) error {
	if version <= OntologyMaterializationVersion {
		return nil
	}
	return fmt.Errorf(
		"%w: stored version %d exceeds supported version %d; upgrade Rhizome before using this index",
		ErrFutureOntologyMaterialization,
		version,
		OntologyMaterializationVersion,
	)
}
