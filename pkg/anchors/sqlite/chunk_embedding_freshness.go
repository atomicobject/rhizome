package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// Retire both current-vector proofs before overwriting changed content hashes.
// Vector bytes and hash-addressed caches remain available for later reuse.
func retireChangedChunkEmbeddingEvidenceTx(ctx context.Context, tx *sql.Tx, chunks []codeanchor.IntelChunk) error {
	// Natural-key deduplication can retain the same chunk ID at different
	// ordinals. Match the final sequential upsert before retiring any batch.
	finalHashes := make(map[string]string, len(chunks))
	for _, chunk := range chunks {
		finalHashes[chunk.ChunkID] = chunk.ContentHash
	}
	// Two parameters per row keep each statement below SQLite's 999-variable
	// compatibility limit. Repeated IDs compare the same final hash, including
	// when their updates span batches.
	for batch := range slices.Chunk(chunks, 400) {
		args := make([]any, 0, 2*len(batch))
		for _, chunk := range batch {
			args = append(args, chunk.ChunkID, finalHashes[chunk.ChunkID])
		}
		inputSQL := `
			WITH input(chunk_id, content_hash) AS (VALUES ` + strings.TrimSuffix(strings.Repeat("(?,?),", len(batch)), ",") + `)
		`
		for _, table := range []string{"ontology_node_embedding_state", "intel_embeddings"} {
			statement := inputSQL + `
				DELETE FROM ` + table + `
				WHERE chunk_id IN (
					SELECT c.chunk_id
					FROM intel_chunks c
					JOIN input i ON i.chunk_id = c.chunk_id
					WHERE c.content_hash != i.content_hash
				)
			`
			if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
				return fmt.Errorf("retire changed chunk embedding evidence in %s: %w", table, err)
			}
		}
	}
	return nil
}
