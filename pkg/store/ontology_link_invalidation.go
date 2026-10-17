package store

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// Resolve dependencies before replacing their old target aliases. An unresolved
// or ambiguous reference has no resolved target path to invalidate afterward.
func ontologyLinkDependentsBeforeTargetsChangeTx(ctx context.Context, tx *sql.Tx, targets []string) ([]string, error) {
	targets = normalizeNonEmptyStrings(targets)
	if len(targets) == 0 {
		return nil, nil
	}
	aliases, err := notePropertyValues(ctx, tx, targets, []string{"aliases"}, NotePropertySourceFrontmatter)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, 3*len(targets)+len(aliases))
	for _, target := range targets {
		keys = append(keys, readmodel.LinkDependencyKey(target), readmodel.LinkDependencyKey(strings.TrimSuffix(target, path.Ext(target))), readmodel.LinkDependencyKey(strings.TrimSuffix(path.Base(target), path.Ext(target))))
	}
	for _, alias := range aliases {
		keys = append(keys, readmodel.LinkDependencyKey(alias.ValueText))
	}
	return ontologyNodeLinkDependencySources(ctx, tx, targets, keys)
}

// Retire only the completeness witness. The catalog and dependencies remain
// available for planning, while published reads cannot claim current authority.
func invalidateOntologyNoteStatesTx(ctx context.Context, tx *sql.Tx, sources []string) error {
	for batch := range slices.Chunk(normalizeNonEmptyStrings(sources), 400) {
		holders, args := placeholders(batch)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM ontology_note_state WHERE note_path IN (%s)`, holders), args...); err != nil {
			return err
		}
	}
	return nil
}
