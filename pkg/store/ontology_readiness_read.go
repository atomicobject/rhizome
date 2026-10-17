package store

import (
	"context"
	"database/sql"
)

// OntologyReadinessSnapshot loads the complete publication for repair planning.
// Serving reads use GetOntologyReadiness instead of hydrating these rows.
func (s *Store) OntologyReadinessSnapshot(ctx context.Context) (OntologyReadinessSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	defer tx.Rollback()
	state, err := ontologyReadinessState(ctx, tx, "")
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	snapshot, err := ontologyReadinessSnapshotTx(ctx, tx, state)
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	return snapshot, nil
}
