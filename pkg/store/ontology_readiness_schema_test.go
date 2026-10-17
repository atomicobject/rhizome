package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOntologyReadinessRejectsIneffectiveTrigger(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "readiness.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = store.db.Exec(`DROP TRIGGER ontology_readiness_ontology_nodes_DELETE;
		CREATE TRIGGER ontology_readiness_ontology_nodes_DELETE AFTER DELETE ON ontology_nodes BEGIN SELECT 1; END;`)
	require.NoError(t, err)
	tx, err := store.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.ErrorContains(t, store.validateIntelSchemaTx(t.Context(), tx), "invalid ontology readiness trigger")
}
