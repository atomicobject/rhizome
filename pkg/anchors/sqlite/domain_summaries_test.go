//go:build fts5

package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersistedDomainSummariesReportAvailabilityAndGeneration(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "db.sqlite"))
	require.NoError(t, err)
	for _, statement := range []string{
		`INSERT INTO files(path, lang, hash, indexer_version, parse_status, call_edges_stale, mtime) VALUES ('code/a.go', 'go', 'hash', 'v1', 'ok', 0, 5)`,
		`INSERT INTO intel_code_anchors(id, anchor_id, lang, kind, path, symbol, fqn, start_byte, end_byte, start_line, end_line, fingerprint, updated_at) VALUES (7, 'anchor-7', 'go', 'function', 'code/a.go', 'A', 'a.A', 0, 1, 1, 1, 'fp', 11)`,
		`INSERT INTO graph_doc_scores(doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at) VALUES ('code/a.go', 'code', 1, 1, 'c', 1, 1, 29)`,
		`INSERT INTO graph_anchor_scores(anchor_id, pagerank, updated_at) VALUES ('anchor-7', 1, 31)`,
	} {
		_, err = store.DB().ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	defer func() { _ = store.Close() }()

	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO intel_chunks(id, chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity, content_hash, start_byte, end_byte, updated_at)
		VALUES (42, 'chunk-42', 'owner-42', 42, 'anchor', 'default', 0, 'symbol', 'hash', 0, 1, 17)
	`)
	require.NoError(t, err)
	_, err = store.DB().ExecContext(ctx, `
		INSERT INTO intel_embeddings(chunk_row_id, chunk_id, norm, dimensions, created_at)
		VALUES (42, 'chunk-42', 1, 3, 23)
	`)
	require.NoError(t, err)

	chunks, err := store.IntelChunksSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, PersistedDomainSummary{Count: 1, Generation: 17}, chunks)
	embeddings, err := store.EmbeddingsSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, PersistedDomainSummary{Count: 1, Generation: 23}, embeddings)
	untouched, err := store.UntouchedIndexDomainsSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, UntouchedIndexSummary{
		Code:              PersistedDomainSummary{Count: 1, Generation: 5},
		CodeAnchors:       PersistedDomainSummary{Count: 1, Generation: 11},
		GraphDocScores:    PersistedDomainSummary{Count: 1, Generation: 29},
		GraphAnchorScores: PersistedDomainSummary{Count: 1, Generation: 31},
	}, untouched)
}

func TestCodeAnchorSelectorSummaryIsRebuildOrderIndependentAndContentSensitive(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	insert := func(reverse bool, basePkg string) {
		t.Helper()
		statements := []string{
			`INSERT INTO notes(id, path, title) VALUES (1, 'notes/a.md', 'A'), (2, 'notes/z.md', 'Z')`,
			`INSERT INTO anchors(id, label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix)
			 VALUES (10, 'alpha', 'function', 'go', 'go', '` + basePkg + `', 'Alpha', 0, '', '', '', '', 'pkg'),
			        (20, 'zeta', 'glob', 'go', '', '', '', 0, '', '', '', '', '')`,
			`INSERT INTO note_anchors(note_id, anchor_id) VALUES (1, 10), (2, 20)`,
			`INSERT INTO anchor_globs(anchor_id, pattern) VALUES (20, 'z/**/*.go'), (20, 'a/**/*.go')`,
		}
		if reverse {
			statements = []string{
				`INSERT INTO notes(id, path, title) VALUES (22, 'notes/z.md', 'Z'), (11, 'notes/a.md', 'A')`,
				`INSERT INTO anchors(id, label, kind, lang, base_lang, base_pkg, base_name, base_member, ann_lang, ann_pkg, ann_name, ann_args_json, path_prefix)
				 VALUES (220, 'zeta', 'glob', 'go', '', '', '', 0, '', '', '', '', ''),
				        (110, 'alpha', 'function', 'go', 'go', '` + basePkg + `', 'Alpha', 0, '', '', '', '', 'pkg')`,
				`INSERT INTO note_anchors(note_id, anchor_id) VALUES (22, 220), (11, 110)`,
				`INSERT INTO anchor_globs(anchor_id, pattern) VALUES (220, 'a/**/*.go'), (220, 'z/**/*.go')`,
			}
		}
		for _, statement := range statements {
			_, err := store.DB().ExecContext(ctx, statement)
			require.NoError(t, err)
		}
	}
	clear := func() {
		t.Helper()
		for _, table := range []string{"note_anchors", "anchor_globs", "anchors", "notes"} {
			_, err := store.DB().ExecContext(ctx, "DELETE FROM "+table)
			require.NoError(t, err)
		}
	}

	insert(false, "example")
	first, err := store.CodeAnchorSelectorSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, first.Count)

	clear()
	insert(true, "example")
	rebuilt, err := store.CodeAnchorSelectorSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, first, rebuilt, "logical selector evidence must not depend on row IDs or insertion order")

	_, err = store.DB().ExecContext(ctx, `UPDATE anchors SET base_pkg = 'full.example' WHERE label = 'alpha'`)
	require.NoError(t, err)
	changed, err := store.CodeAnchorSelectorSummary(ctx)
	require.NoError(t, err)
	require.Equal(t, rebuilt.Count, changed.Count)
	require.NotEqual(t, rebuilt.Hash, changed.Hash, "same-count selector rewrites must invalidate freshness")
}
