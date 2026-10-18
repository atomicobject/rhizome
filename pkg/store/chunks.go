package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"slices"
	"strings"
)

// ReplaceIntelChunks atomically replaces all chunks for the given owner IDs.
// This is typically called after generating chunks for a set of anchors/sections.
func (s *Store) ReplaceIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_chunks")
	return s.replaceIntelChunks(ctx, ownerIDs, "", chunks)
}

// ReplaceIntelChunksByFamily atomically replaces one chunk family without
// deleting other producers that share the same owner IDs.
func (s *Store) ReplaceIntelChunksByFamily(ctx context.Context, ownerIDs []string, family string, chunks []codeanchor.IntelChunk) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_chunks_by_family")
	family = normalizeIntelChunkFamily(family)
	for i := range chunks {
		if strings.TrimSpace(chunks[i].ChunkFamily) == "" {
			chunks[i].ChunkFamily = family
		}
	}
	return s.replaceIntelChunks(ctx, ownerIDs, family, chunks)
}

func (s *Store) replaceIntelChunks(ctx context.Context, ownerIDs []string, family string, chunks []codeanchor.IntelChunk) error {
	if len(ownerIDs) == 0 {
		return nil
	}
	ownerIDs = dedupeStringsStable(ownerIDs)
	chunks = dedupeIntelChunksStable(chunks)
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		ownerRefs := make(map[string]intelOwnerRef, len(ownerIDs))
		for _, ownerID := range ownerIDs {
			ref, err := lookupIntelOwnerRefTx(ctx, tx, ownerID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			ownerRefs[ownerID] = ref
		}

		keepChunkIDs := make(map[string]struct{}, len(chunks))
		for _, c := range chunks {
			if strings.TrimSpace(c.ChunkID) != "" {
				keepChunkIDs[c.ChunkID] = struct{}{}
			}
		}
		useKeepTable := len(keepChunkIDs) > 200
		if useKeepTable {
			if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_intel_keep_chunks`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_intel_keep_chunks (chunk_id TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_intel_keep_chunks(chunk_id) VALUES (?)`)
			if err != nil {
				return err
			}
			for id := range keepChunkIDs {
				if _, err := stmt.ExecContext(ctx, id); err != nil {
					_ = stmt.Close()
					return err
				}
			}
			if err := stmt.Close(); err != nil {
				return err
			}
			defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_intel_keep_chunks`) //nolint:errcheck
		}

		// Delete stale chunks for the owner IDs while preserving unchanged chunk
		// rows so their embeddings survive hash-stable incremental indexing.
		// Family-scoped replacement lets primary chunks and authored
		// section chunks share owners without erasing each other.
		for batch := range slices.Chunk(ownerIDs, 100) {
			holders, args := placeholders(batch)
			deleteSQL := fmt.Sprintf(`DELETE FROM intel_chunks WHERE owner_id IN (%s)`, holders)
			if useKeepTable {
				deleteSQL += ` AND chunk_id NOT IN (SELECT chunk_id FROM temp_intel_keep_chunks)`
			} else if len(keepChunkIDs) > 0 {
				chunkHolders := make([]string, 0, len(keepChunkIDs))
				for id := range keepChunkIDs {
					chunkHolders = append(chunkHolders, "?")
					args = append(args, id)
				}
				deleteSQL += fmt.Sprintf(` AND chunk_id NOT IN (%s)`, strings.Join(chunkHolders, ","))
			}
			if family != "" {
				deleteSQL += ` AND chunk_family = ?`
				args = append(args, family)
			}
			if _, err := tx.ExecContext(ctx, deleteSQL, args...); err != nil {
				return err
			}
		}

		// Insert or update current chunks.
		if len(chunks) == 0 {
			return nil
		}
		if err := retireChangedChunkEmbeddingEvidenceTx(ctx, tx, chunks); err != nil {
			return err
		}
		const insertSQL = `
			INSERT INTO intel_chunks (
				chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity,
				breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(chunk_id) DO UPDATE SET
				owner_id = excluded.owner_id,
				owner_row_id = excluded.owner_row_id,
				owner_type = excluded.owner_type,
				chunk_family = excluded.chunk_family,
				ord = excluded.ord,
				granularity = excluded.granularity,
				breadcrumb = excluded.breadcrumb,
				heading = excluded.heading,
				content_hash = excluded.content_hash,
				start_byte = excluded.start_byte,
				end_byte = excluded.end_byte,
				updated_at = excluded.updated_at`
		stmt, err := tx.PrepareContext(ctx, insertSQL)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, c := range chunks {
			ref, ok := ownerRefs[c.OwnerID]
			if !ok {
				var err error
				ref, err = lookupIntelOwnerRefTx(ctx, tx, c.OwnerID)
				if err != nil {
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
					ref = intelOwnerRef{ownerType: string(c.OwnerType)}
				}
				ownerRefs[c.OwnerID] = ref
			}
			var ownerRowID any
			if ref.rowID > 0 {
				ownerRowID = ref.rowID
			}
			if _, err := stmt.ExecContext(ctx, c.ChunkID, c.OwnerID, ownerRowID, ref.ownerType, normalizeIntelChunkFamily(c.ChunkFamily), c.Ord, c.Granularity, c.Breadcrumb, c.Heading, c.ContentHash, c.StartByte, c.EndByte, c.UpdatedAt); err != nil {
				return err
			}
		}
		return repartitionIntelEmbeddingOwnersTx(ctx, tx, keepChunkIDs, useKeepTable)
	})
}
