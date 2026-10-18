package mcp

import (
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestSemanticQueryFallbackDoesNotMigrateOrWriteIndex(t *testing.T) {
	for _, older := range []bool{true, false} {
		name := "current index"
		if older {
			name = "older index"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".rhizome", "db.sqlite")
			saveSemanticFallbackConfig(t, root, "")
			store, err := semdb.Open(path)
			require.NoError(t, err)
			defer store.Close()
			require.NoError(t, store.ReplaceIntelCodeFile(t.Context(), "pkg/kept.go", []codeanchor.IntelAnchor{{
				AnchorID: "kept-anchor", Path: "pkg/kept.go", Symbol: "KeepIndexedSymbol", FQN: "synthetic.KeepIndexedSymbol",
				Kind: "function", Lang: codeanchor.LangGo, Fingerprint: "kept-source",
			}}, nil, []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: "kept-anchor", Path: "pkg/kept.go", Title: "KeepIndexedSymbol", Body: "func KeepIndexedSymbol()"}}))
			if older {
				// Model an index published by an earlier binary without letting
				// the public query repair its migration state or validation proof.
				updated, err := store.DB().Exec("UPDATE rzm_migration_state SET version = version - 1 WHERE domain = 'intel'")
				require.NoError(t, err)
				count, err := updated.RowsAffected()
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
				_, err = store.DB().Exec("DELETE FROM rzm_migration_validation WHERE domain = 'intel'")
				require.NoError(t, err)
			}
			observer, err := store.DB().Conn(t.Context())
			require.NoError(t, err)
			defer observer.Close()
			before := observeSemanticFallbackIndex(t, observer, path)

			response, err := SemanticQueryUnifiedWithOptions(t.Context(), Config{
				NoteMetadata: configuredMCPNoteMetadata(t),

				VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStorePolicy: IntelStoreFallbackAllowed,
			}, SemanticQueryOptions{Query: "KeepIndexedSymbol", Scope: "code", Mode: "search", Limit: 5})

			if older {
				require.ErrorContains(t, err, "rzm index")
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, response.Matches, "the uninjected public fallback must execute the query")
				require.Equal(t, "pkg/kept.go", response.Matches[0].Path)
			}
			after := observeSemanticFallbackIndex(t, observer, path)
			require.Equal(t, before, after, "fallback reads must preserve database/WAL bytes, schema, migration state, and indexed data")
		})
	}
}

func TestSemanticQueryFallbackDoesNotCreateMissingIndex(t *testing.T) {
	root := t.TempDir()
	saveSemanticFallbackConfig(t, root, "missing/index.sqlite")

	_, err := SemanticQueryUnifiedWithOptions(t.Context(), Config{
		NoteMetadata: configuredMCPNoteMetadata(t),

		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStorePolicy: IntelStoreFallbackAllowed,
	}, SemanticQueryOptions{Query: "KeepIndexedSymbol", Scope: "code"})

	require.ErrorContains(t, err, "rzm index")
	_, err = os.Stat(filepath.Join(root, "missing"))
	require.True(t, os.IsNotExist(err), "public search must not create an index directory")
}

func saveSemanticFallbackConfig(t *testing.T, root, indexPath string) {
	t.Helper()
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{
		IndexPath: indexPath, Code: repoconfig.LocalCodeConfig{Enabled: true},
		NoteEmbeddings: &embeddings.Config{Enabled: false}, CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
}

type semanticFallbackIndexState struct {
	DataVersion, SchemaVersion, DomainVersion int
	Symbol                                    string
	MainHash, WALHash, SchemaHash             [32]byte
	WALExists                                 bool
}

func observeSemanticFallbackIndex(t *testing.T, conn *sql.Conn, path string) semanticFallbackIndexState {
	t.Helper()
	var state semanticFallbackIndexState
	require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA data_version").Scan(&state.DataVersion))
	require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA schema_version").Scan(&state.SchemaVersion))
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT version FROM rzm_migration_state WHERE domain = 'intel'").Scan(&state.DomainVersion))
	require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT symbol FROM intel_code_anchors WHERE anchor_id = 'kept-anchor'").Scan(&state.Symbol))
	require.Equal(t, "KeepIndexedSymbol", state.Symbol)
	rows, err := conn.QueryContext(t.Context(), "SELECT type, name, COALESCE(sql, '') FROM sqlite_schema ORDER BY type, name")
	require.NoError(t, err)
	var schema []string
	for rows.Next() {
		var kind, name, ddl string
		require.NoError(t, rows.Scan(&kind, &name, &ddl))
		schema = append(schema, strings.Join([]string{kind, name, ddl}, "\x00"))
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	state.SchemaHash = sha256.Sum256([]byte(strings.Join(schema, "\n")))
	main, err := os.ReadFile(path)
	require.NoError(t, err)
	state.MainHash = sha256.Sum256(main)
	wal, err := os.ReadFile(path + "-wal")
	if !os.IsNotExist(err) {
		require.NoError(t, err)
		state.WALExists = true
		state.WALHash = sha256.Sum256(wal)
	}
	return state
}
