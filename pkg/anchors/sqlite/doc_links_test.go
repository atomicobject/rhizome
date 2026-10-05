package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

func TestReplaceDocLinksForPath_HandlesDuplicates(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "test.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Now().Unix()
	links := []codeanchor.DocLink{
		{SrcType: "code", SrcPath: "src/main.go", DstKind: "note", DstPath: "docs/README.md", DstID: "", UpdatedAt: now},
		{SrcType: "code", SrcPath: "src/main.go", DstKind: "note", DstPath: "docs/README.md", DstID: "", UpdatedAt: now}, // duplicate
		{SrcType: "code", SrcPath: "src/main.go", DstKind: "note", DstPath: "docs/GUIDE.md", DstID: "", UpdatedAt: now},
	}

	// Should not fail with duplicate links
	err = store.ReplaceDocLinksForPath(ctx, "src/main.go", links)
	require.NoError(t, err)

	// Verify only 2 unique links were stored
	result, err := store.DocLinksFromCodePath(ctx, "src/main.go", 10)
	require.NoError(t, err)
	require.Len(t, result, 2)
}

func TestReplaceDocLinksForPathsBatch_HandlesDuplicates(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "test.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Now().Unix()
	batches := []codeanchor.DocLinksBatch{
		{
			SrcPath: "src/a.go",
			Links: []codeanchor.DocLink{
				{SrcType: "code", SrcPath: "src/a.go", DstKind: "note", DstPath: "docs/A.md", DstID: "", UpdatedAt: now},
				{SrcType: "code", SrcPath: "src/a.go", DstKind: "note", DstPath: "docs/A.md", DstID: "", UpdatedAt: now}, // duplicate
			},
		},
		{
			SrcPath: "src/b.go",
			Links: []codeanchor.DocLink{
				{SrcType: "code", SrcPath: "src/b.go", DstKind: "note", DstPath: "docs/B.md", DstID: "", UpdatedAt: now},
			},
		},
	}

	// Should not fail with duplicate links
	err = store.ReplaceDocLinksForPathsBatch(ctx, batches)
	require.NoError(t, err)

	// Verify correct counts
	resultA, err := store.DocLinksFromCodePath(ctx, "src/a.go", 10)
	require.NoError(t, err)
	require.Len(t, resultA, 1) // deduplicated

	resultB, err := store.DocLinksFromCodePath(ctx, "src/b.go", 10)
	require.NoError(t, err)
	require.Len(t, resultB, 1)
}

func TestReplaceDocLinksForPathsBatch_ReplacesExisting(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "test.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Now().Unix()

	// Insert initial links
	err = store.ReplaceDocLinksForPath(ctx, "src/main.go", []codeanchor.DocLink{
		{SrcType: "code", SrcPath: "src/main.go", DstKind: "note", DstPath: "docs/OLD.md", DstID: "", UpdatedAt: now},
	})
	require.NoError(t, err)

	// Replace with batch
	batches := []codeanchor.DocLinksBatch{
		{
			SrcPath: "src/main.go",
			Links: []codeanchor.DocLink{
				{SrcType: "code", SrcPath: "src/main.go", DstKind: "note", DstPath: "docs/NEW.md", DstID: "", UpdatedAt: now},
			},
		},
	}
	err = store.ReplaceDocLinksForPathsBatch(ctx, batches)
	require.NoError(t, err)

	// Verify old link is gone and new link exists
	result, err := store.DocLinksFromCodePath(ctx, "src/main.go", 10)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "docs/NEW.md", result[0].DstPath)
}

func TestDocLinksForNotePreservesMixedCaseAuthoredPath(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "test.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceDocLinksForPath(ctx, "src/main.go", []codeanchor.DocLink{{
		SrcType:   "code",
		SrcPath:   "src/main.go",
		DstKind:   "note",
		DstPath:   "Decision.MD",
		UpdatedAt: time.Now().Unix(),
	}}))

	links, err := store.DocLinksForNote(ctx, "Decision.MD", 10)
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, "Decision.MD", links[0].DstPath)
}
