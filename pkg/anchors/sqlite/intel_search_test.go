package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchIntelFTS(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Anchor
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES ('a1', 'go', 'function', 'pkg/foo.go', 'Foo', 'example.com/mod/pkg.Foo', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_fts(item_type, item_id, path, title, body)
		VALUES ('anchor', 'a1', 'pkg/foo.go', 'Foo', 'Bar does things')
	`)
	require.NoError(t, err)

	// Doc section
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_doc_sections(section_id, path, title, level, start_byte, end_byte, content, fingerprint, updated_at)
		VALUES ('s1', 'notes/foo.md', 'Foo Note', 1, 0, 10, 'Foo note content', 'fp', 1)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_fts(item_type, item_id, path, title, body)
		VALUES ('doc_section', 's1', 'notes/foo.md', 'Bar Note', 'Foo note content mentions Foo')
	`)
	require.NoError(t, err)

	results, err := store.SearchIntelFTS(ctx, "Foo", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	types := []string{results[0].Type, results[1].Type}
	require.Contains(t, types, "anchor")
	require.Contains(t, types, "doc_section")
	for _, result := range results {
		if result.ID == "a1" {
			require.True(t, result.TitleHit)
			require.False(t, result.BodyHit)
		} else {
			require.False(t, result.TitleHit)
			require.True(t, result.BodyHit)
		}
	}
	punctuation, err := store.SearchIntelFTS(ctx, "Foo?", 10)
	require.NoError(t, err)
	require.Len(t, punctuation, 2)
	_, err = store.SearchIntelFTS(ctx, "how do modules have their embedding code prepared?", 10)
	require.NoError(t, err)
}

func TestSearchIntelFTS_DownweightsPathMatches(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Path-only match (should rank lower than body match).
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES ('p1', 'go', 'module', 'pkg/chunker.go', 'chunker.go', '', '', '', 0, 0, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_fts(item_type, item_id, path, title, body)
		VALUES ('anchor', 'p1', 'pkg/chunker.go', 'chunker.go', 'pkg/chunker.go')
	`)
	require.NoError(t, err)

	// Body match (should rank higher).
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_code_anchors(anchor_id, lang, kind, path, symbol, fqn, signature, doc_comment, start_byte, end_byte, start_line, end_line, fingerprint, updated_at)
		VALUES ('b1', 'go', 'function', 'pkg/foo.go', 'Foo', 'example.com/mod/pkg.Foo', '', '', 0, 1, 1, 1, 'fp', 1)
	`)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO intel_fts(item_type, item_id, path, title, body)
		VALUES ('anchor', 'b1', 'pkg/foo.go', 'Foo', 'chunker chunker chunker')
	`)
	require.NoError(t, err)

	results, err := store.SearchIntelFTS(ctx, "chunker", 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 2)
	require.Equal(t, "b1", results[0].ID)
}
