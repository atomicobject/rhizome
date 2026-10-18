package store

import (
	"context"
	"database/sql"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"slices"
	"strings"
)

// UpsertOntologyNodeEmbeddingStates records ontology-only invalidation metadata
// beside vector rows.
func (s *Store) UpsertOntologyNodeEmbeddingStates(ctx context.Context, states []codeanchor.IntelOntologyNodeEmbeddingState) error {
	if len(states) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_ontology_node_embedding_state")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO ontology_node_embedding_state (
				chunk_id, node_id, note_path, type_name, node_kind,
				embedding_schema_signature, node_structure_fingerprint,
				source_content_hash, chunk_text_hash, chunk_granularity,
				provider, model, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(chunk_id) DO UPDATE SET
				node_id = excluded.node_id,
				note_path = excluded.note_path,
				type_name = excluded.type_name,
				node_kind = excluded.node_kind,
				embedding_schema_signature = excluded.embedding_schema_signature,
				node_structure_fingerprint = excluded.node_structure_fingerprint,
				source_content_hash = excluded.source_content_hash,
				chunk_text_hash = excluded.chunk_text_hash,
				chunk_granularity = excluded.chunk_granularity,
				provider = excluded.provider,
				model = excluded.model,
				updated_at = excluded.updated_at
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, state := range states {
			if strings.TrimSpace(state.ChunkID) == "" || strings.TrimSpace(state.NodeID) == "" || strings.TrimSpace(state.NotePath) == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx,
				state.ChunkID,
				state.NodeID,
				state.NotePath,
				state.TypeName,
				state.NodeKind,
				state.EmbeddingSchemaSignature,
				state.NodeStructureFingerprint,
				state.SourceContentHash,
				state.ChunkTextHash,
				state.ChunkGranularity,
				state.Provider,
				state.Model,
				state.UpdatedAt,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

// OntologyNodeEmbeddingStatesByChunkIDs returns sidecar invalidation metadata keyed by chunk_id.
func (s *Store) OntologyNodeEmbeddingStatesByChunkIDs(ctx context.Context, chunkIDs []string) (map[string]codeanchor.IntelOntologyNodeEmbeddingState, error) {
	chunkIDs = normalizeNonEmptyStrings(chunkIDs)
	out := make(map[string]codeanchor.IntelOntologyNodeEmbeddingState, len(chunkIDs))
	if len(chunkIDs) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(chunkIDs, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT chunk_id, node_id, note_path, COALESCE(type_name, ''), node_kind,
				embedding_schema_signature, node_structure_fingerprint, source_content_hash,
				chunk_text_hash, chunk_granularity, COALESCE(provider, ''), COALESCE(model, ''), updated_at
			FROM ontology_node_embedding_state
			WHERE chunk_id IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var state codeanchor.IntelOntologyNodeEmbeddingState
			if err := rows.Scan(
				&state.ChunkID,
				&state.NodeID,
				&state.NotePath,
				&state.TypeName,
				&state.NodeKind,
				&state.EmbeddingSchemaSignature,
				&state.NodeStructureFingerprint,
				&state.SourceContentHash,
				&state.ChunkTextHash,
				&state.ChunkGranularity,
				&state.Provider,
				&state.Model,
				&state.UpdatedAt,
			); err != nil {
				rows.Close()
				return nil, err
			}
			out[state.ChunkID] = state
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
