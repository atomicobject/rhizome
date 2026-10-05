package sqlite

import (
	"context"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestCodeIndexPrerequisiteSnapshot_EmptyStoreReportsExplicitAbsence(t *testing.T) {
	t.Parallel()

	store, err := Open(currentSchemaTestDBPath(t, "empty.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	snapshot, err := store.CodeIndexPrerequisiteSnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, CodeIndexPrerequisiteSnapshot{}, snapshot)
}

func TestCodeIndexPrerequisiteSnapshot_ReturnsPersistedCodeFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "populated.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime)
		VALUES ('src/a.go', 'go', 'hash-a', 'v1.8.0', 'ok', 100),
		       ('src/b.go', 'go', 'hash-b', 'v1.8.0', 'ok', 250)
	`)
	require.NoError(t, err)
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "src/a.go", []codeanchor.IntelAnchor{{
		AnchorID:    "go:src/a.go:Run",
		Lang:        codeanchor.LangGo,
		Kind:        "function",
		Path:        "src/a.go",
		Symbol:      "Run",
		FQN:         "example.Run",
		Fingerprint: "fingerprint-run",
	}}, nil, nil))
	require.NoError(t, store.SetIndexerVersion(ctx, "v1.8.0"))
	require.NoError(t, store.SetScopeConfigHash(ctx, "scope-v2"))

	snapshot, err := store.CodeIndexPrerequisiteSnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, CodeIndexPrerequisiteSnapshot{
		CodeRowsPresent:        true,
		IndexedFileCount:       2,
		IndexerVersion:         "v1.8.0",
		IndexerVersionPresent:  true,
		ScopeConfigHash:        "scope-v2",
		ScopeConfigHashPresent: true,
		IndexedAt:              time.Unix(250, 0).UTC(),
		IndexedAtPresent:       true,
	}, snapshot)
}

func TestCodeIndexPrerequisiteSnapshot_SymbolRefsCountAsCodeRowsWithoutInventingFileFacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "refs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.ReplaceIntelSymbolRefsForPathsBatch(ctx, map[string][]codeanchor.SymbolRefRow{
		"src/caller.go": {{
			SrcPath:  "src/caller.go",
			OwnerFQN: "example.Caller",
			RefKind:  codeanchor.RefKindCalls,
			DstLang:  codeanchor.LangGo,
			DstPkg:   "example",
			DstName:  "Callee",
			DstFQN:   "example.Callee",
		}},
	}))

	snapshot, err := store.CodeIndexPrerequisiteSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, snapshot.CodeRowsPresent)
	require.Zero(t, snapshot.IndexedFileCount)
	require.False(t, snapshot.IndexerVersionPresent)
	require.False(t, snapshot.ScopeConfigHashPresent)
	require.False(t, snapshot.IndexedAtPresent)
	require.True(t, snapshot.IndexedAt.IsZero())
}

func TestCodeIndexPrerequisiteSnapshot_FileMetadataAloneIsNotCodeRowPresence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "files-only.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO files(path, lang, hash, indexer_version, parse_status, mtime)
		VALUES ('src/empty.go', 'go', 'hash-empty', 'v1.8.0', 'ok', 125)
	`)
	require.NoError(t, err)

	snapshot, err := store.CodeIndexPrerequisiteSnapshot(ctx)
	require.NoError(t, err)
	require.False(t, snapshot.CodeRowsPresent)
	require.Equal(t, 1, snapshot.IndexedFileCount)
	require.Equal(t, time.Unix(125, 0).UTC(), snapshot.IndexedAt)
	require.True(t, snapshot.IndexedAtPresent)
}
