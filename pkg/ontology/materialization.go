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
const OntologyMaterializationVersion = 14

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
