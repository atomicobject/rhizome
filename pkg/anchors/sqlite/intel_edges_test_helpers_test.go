package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustIntelNodeRowID(t *testing.T, ctx context.Context, store *Store, nodeType, externalID string) int64 {
	t.Helper()

	var (
		query string
		rowID int64
	)
	switch nodeType {
	case "anchor":
		query = `SELECT id FROM intel_code_anchors WHERE anchor_id = ?`
	case "doc_section":
		query = `SELECT id FROM intel_doc_sections WHERE section_id = ?`
	default:
		t.Fatalf("unknown intel node type %q", nodeType)
	}
	require.NoError(t, store.db.QueryRowContext(ctx, query, externalID).Scan(&rowID))
	return rowID
}

func intelNodeRowID(ctx context.Context, store *Store, nodeType, externalID string) (int64, bool, error) {
	var (
		query string
		rowID int64
	)
	switch nodeType {
	case "anchor":
		query = `SELECT id FROM intel_code_anchors WHERE anchor_id = ?`
	case "doc_section":
		query = `SELECT id FROM intel_doc_sections WHERE section_id = ?`
	default:
		return 0, false, nil
	}
	err := store.db.QueryRowContext(ctx, query, externalID).Scan(&rowID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}
	return rowID, true, nil
}

func mustInsertIntelEdge(t *testing.T, ctx context.Context, store *Store, srcType, srcID, dstType, dstID, kind string) {
	t.Helper()
	srcRowID := mustIntelNodeRowID(t, ctx, store, srcType, srcID)
	dstRowID := mustIntelNodeRowID(t, ctx, store, dstType, dstID)
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO intel_edges(src_type, src_row_id, dst_type, dst_row_id, kind, meta_json)
		VALUES (?, ?, ?, ?, ?, NULL)
	`, srcType, srcRowID, dstType, dstRowID, kind)
	require.NoError(t, err)
}

func countIntelEdgesByNodes(t *testing.T, ctx context.Context, store *Store, srcType, srcID, dstType, dstID, kind string) int {
	t.Helper()
	srcRowID, ok, err := intelNodeRowID(ctx, store, srcType, srcID)
	require.NoError(t, err)
	if !ok {
		return 0
	}
	dstRowID, ok, err := intelNodeRowID(ctx, store, dstType, dstID)
	require.NoError(t, err)
	if !ok {
		return 0
	}
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_edges
		WHERE src_type = ? AND src_row_id = ?
		  AND dst_type = ? AND dst_row_id = ?
		  AND kind = ?
	`, srcType, srcRowID, dstType, dstRowID, kind).Scan(&count))
	return count
}

func countIntelEdgesFromNode(t *testing.T, ctx context.Context, store *Store, srcType, srcID string) int {
	t.Helper()
	srcRowID, ok, err := intelNodeRowID(ctx, store, srcType, srcID)
	require.NoError(t, err)
	if !ok {
		return 0
	}
	var count int
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_edges
		WHERE src_type = ? AND src_row_id = ?
	`, srcType, srcRowID).Scan(&count))
	return count
}
