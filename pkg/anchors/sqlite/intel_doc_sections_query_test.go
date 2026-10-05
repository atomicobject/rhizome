package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestIntelDocSectionIDsByPathPreservesOrderedContentReader(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "section-ids.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	path := "notes/ordered.md"
	sections := []codeanchor.IntelDocSection{
		{SectionID: "z", Path: path, Title: "Z", Level: 2, StartByte: 10, EndByte: 20, Content: "z content", Fingerprint: "z-fingerprint", UpdatedAt: 101},
		{SectionID: "first", Path: path, Title: "First", Level: 1, StartByte: 0, EndByte: 10, Content: "first content", Fingerprint: "first-fingerprint", UpdatedAt: 102},
		{SectionID: "a", Path: path, Title: "A", Level: 3, StartByte: 10, EndByte: 20, Content: "a content", Fingerprint: "a-fingerprint", UpdatedAt: 103},
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, path, sections, nil, nil))
	require.NoError(t, store.ReplaceIntelDocSections(ctx, "notes/other.md", []codeanchor.IntelDocSection{
		{SectionID: "unrelated", Path: "notes/other.md", Title: "Other", Level: 1, Content: "other body", Fingerprint: "other"},
	}, nil, nil))
	ids, err := store.IntelDocSectionIDsByPath(ctx, path)
	require.NoError(t, err)
	require.Equal(t, []string{"first", "a", "z"}, ids)
	full, err := store.IntelDocSectionsByPath(ctx, path)
	require.NoError(t, err)
	require.Equal(t, []codeanchor.IntelDocSection{sections[1], sections[2], sections[0]}, full,
		"the content-bearing reader keeps all existing fields and the same order")
	ids, err = store.IntelDocSectionIDsByPath(ctx, "notes/missing.md")
	require.NoError(t, err)
	require.Empty(t, ids)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	ids, err = store.IntelDocSectionIDsByPath(canceled, path)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, ids)
}
