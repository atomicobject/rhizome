package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// ReplaceOntologyNodes atomically syncs ontology-node owners for the given note
// paths. Existing rows for unchanged node IDs are updated in place so their
// chunk rows and embeddings can survive hash-stable incremental indexing.
func (s *Store) ReplaceOntologyNodes(ctx context.Context, notePaths []string, nodes []readmodel.NodeRow) error {
	return s.ReplaceOntologyNodeReadModel(ctx, readmodel.NodeReadModel{
		NotePaths: notePaths,
		Nodes:     nodes,
	})
}

// ReplaceOntologyNodeReadModel atomically syncs ontology-node identity rows and
// their queryable field-value sidecar rows for the given note paths.
func (s *Store) ReplaceOntologyNodeReadModel(ctx context.Context, model readmodel.NodeReadModel) error {
	return s.replaceOntologyNodeReadModel(ctx, model, false)
}

// ReplaceOntologyNodeReadModelPreservingSemantic refreshes ontology catalog
// rows without advancing or pruning ontology-owned chunks, embeddings, or their
// invalidation sidecar. Validation projection uses this mode because semantic
// freshness is an independent prerequisite that only full indexing may advance.
func (s *Store) ReplaceOntologyNodeReadModelPreservingSemantic(ctx context.Context, model readmodel.NodeReadModel) error {
	return s.replaceOntologyNodeReadModel(ctx, model, true)
}

func (s *Store) replaceOntologyNodeReadModel(ctx context.Context, model readmodel.NodeReadModel, preserveSemantic bool) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_ontology_node_read_model")
	var fieldRowsWritten int
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		var err error
		fieldRowsWritten, err = replaceOntologyNodeReadModelTx(ctx, tx, model, preserveSemantic)
		return err
	})
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.OntologyFieldRowsWritten, int64(fieldRowsWritten))
	}
	return err
}

func replaceOntologyNodeReadModelTx(ctx context.Context, tx *sql.Tx, model readmodel.NodeReadModel, preserveSemantic bool) (int, error) {
	notePaths := normalizeNonEmptyStrings(model.NotePaths)
	nodes, fieldValues, linkDependencies := model.Nodes, model.FieldValues, model.LinkDependencies
	if len(notePaths) == 0 && !model.FullReplace {
		return 0, nil
	}
	var fieldRowsWritten int
	if model.FullReplace {
		var statements []string
		if !preserveSemantic {
			statements = append(statements, `DELETE FROM ontology_node_embedding_state`)
		}
		statements = append(statements,
			`DELETE FROM ontology_node_field_values`,
			`DELETE FROM ontology_node_field_value_dependencies`,
		)
		if !preserveSemantic {
			statements = append(statements,
				`DELETE FROM intel_chunks WHERE owner_type = 'ontology_node'`,
			)
		}
		statements = append(statements, `DELETE FROM ontology_nodes`)
		for _, stmt := range statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return 0, err
			}
		}
	} else {
		if err := deleteOntologyNodeFieldValuesForPathsTx(ctx, tx, notePaths); err != nil {
			return 0, err
		}
		if err := deleteOntologyNodeLinkDependenciesForPathsTx(ctx, tx, notePaths); err != nil {
			return 0, err
		}
		if err := deleteStaleOntologyNodesForPathsTx(ctx, tx, notePaths, nodes, preserveSemantic); err != nil {
			return 0, err
		}
	}
	if len(nodes) > 0 {
		if err := insertOntologyNodesTx(ctx, tx, nodes); err != nil {
			return 0, err
		}
	}
	if len(fieldValues) > 0 {
		written, err := insertOntologyNodeFieldValuesTx(ctx, tx, fieldValues)
		if err != nil {
			return 0, err
		}
		fieldRowsWritten = written
	}
	if len(linkDependencies) > 0 {
		return fieldRowsWritten, insertOntologyNodeLinkDependenciesTx(ctx, tx, linkDependencies)
	}
	return fieldRowsWritten, nil
}
