package indexing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func touchParityVault(t *testing.T) (string, obsidian.VaultDefinition) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Decision @node(paths: ["notes/*.md"]) {
  title: String
}
`), 0o600))
	writeTouchParityNote(t, root, "notes/Source.md", "---\ntitle: Source\n---\nSee [[Target]].\n")
	writeTouchParityNote(t, root, "notes/Target.md", "---\ntitle: Target\naliases: [Target]\n---\n# Target\n")
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          obsidian.LocalVaultConfig{Includes: []string{"notes/**/*.md"}},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	return root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}, Links: obsidian.LinkTypeBoth}
}

type touchParityFixtureFile struct {
	path string
	mode os.FileMode
	data []byte
}

var indexedTouchParitySnapshot struct {
	once  sync.Once
	files []touchParityFixtureFile
	err   error
}

func indexedTouchParityVault(t *testing.T) (string, obsidian.VaultDefinition) {
	t.Helper()
	indexedTouchParitySnapshot.once.Do(func() {
		root, definition := touchParityVault(t)
		indexedTouchParitySnapshot.err = RunUnifiedCore(context.Background(), UnifiedOptions{
			VaultPath:    root,
			VaultDef:     definition,
			NoteMetadata: testNoteMetadataIndexer(t),
		})
		if indexedTouchParitySnapshot.err != nil {
			return
		}
		indexedTouchParitySnapshot.err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() || strings.HasSuffix(path, ".init.lock") || strings.HasSuffix(path, "-wal") || strings.HasSuffix(path, "-shm") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			indexedTouchParitySnapshot.files = append(indexedTouchParitySnapshot.files, touchParityFixtureFile{
				path: rel,
				mode: info.Mode(),
				data: data,
			})
			return nil
		})
	})
	require.NoError(t, indexedTouchParitySnapshot.err)

	root := t.TempDir()
	for _, file := range indexedTouchParitySnapshot.files {
		path := filepath.Join(root, file.path)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, file.data, file.mode))
	}
	return root, obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md"}, Links: obsidian.LinkTypeBoth}
}

func writeTouchParityNote(t *testing.T, root, notePath, content string) {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(notePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
	require.NoError(t, os.WriteFile(absPath, []byte(content), 0o600))
}

func touchEveryParityNote(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		next := info.ModTime().Add(2 * time.Second)
		return os.Chtimes(path, next, next)
	}))
}

func openTouchParityStore(t *testing.T, root string) *semdb.Store {
	t.Helper()
	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	return store
}

// TestRunUnifiedCore_TouchOnlyReindexKeepsGenerationAndGraphRevision pins the
// core contract: an mtime-only change refreshes note freshness evidence and
// nothing else, so no cached graph response and no published-generation reader
// is invalidated.
func TestRunUnifiedCore_TouchOnlyReindexKeepsGenerationAndGraphRevision(t *testing.T) {
	ctx := context.Background()
	root, definition := touchParityVault(t)
	indexer := testNoteMetadataIndexer(t)
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))

	store := openTouchParityStore(t, root)
	noteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	ontologyState, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	fingerprint, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, edges)

	touchEveryParityNote(t, root)
	sourceInfo, err := os.Stat(filepath.Join(root, "notes", "Source.md"))
	require.NoError(t, err)
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))

	updatedFingerprint, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, fingerprint, updatedFingerprint, "a touch-only re-index must not bump the graph revision")
	updatedNoteState, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, noteState, updatedNoteState)
	updatedOntologyState, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, ontologyState, updatedOntologyState)
	updatedEdges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, edges, updatedEdges)

	runtime, err := ontology.PublishedRuntimeWithStore(ctx, indexer, definition, store)
	require.NoError(t, err)
	require.True(t, runtime.Ready)

	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/Source.md"})
	require.NoError(t, err)
	require.Equal(t, sourceInfo.ModTime().Unix(), rows["notes/Source.md"].Mtime,
		"note metadata and code-intel mtimes must share one unit")
}

// Every note-shaped change must still converge on exactly what a fresh index
// of the same tree produces.
func TestRunUnifiedCore_IncrementalScenariosMatchFreshRebuild(t *testing.T) {
	scenarios := []struct {
		name  string
		apply func(t *testing.T, root string)
	}{
		{"touch only", func(t *testing.T, root string) { touchEveryParityNote(t, root) }},
		{"real edit", func(t *testing.T, root string) {
			writeTouchParityNote(t, root, "notes/Source.md", "---\ntitle: Source\n---\nSee [[Target]] once more.\n")
		}},
		{"add note", func(t *testing.T, root string) {
			writeTouchParityNote(t, root, "notes/Added.md", "---\ntitle: Added\n---\nSee [[Target]].\n")
		}},
		{"delete linked target", func(t *testing.T, root string) {
			require.NoError(t, os.Remove(filepath.Join(root, "notes", "Target.md")))
		}},
		{"alias rename on linked target", func(t *testing.T, root string) {
			writeTouchParityNote(t, root, "notes/Source.md", "---\ntitle: Source\n---\nSee [[Renamed]].\n")
			writeTouchParityNote(t, root, "notes/Target.md", "---\ntitle: Target\naliases: [Renamed]\n---\n# Target\n")
		}},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			root, definition := indexedTouchParityVault(t)
			indexer := testNoteMetadataIndexer(t)
			scenario.apply(t, root)
			require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
			incremental := openTouchParityStore(t, root)

			freshRoot, freshDefinition := touchParityVault(t)
			require.NoError(t, os.RemoveAll(filepath.Join(freshRoot, "notes")))
			require.NoError(t, copyTouchParityNotes(filepath.Join(root, "notes"), filepath.Join(freshRoot, "notes")))
			require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: freshRoot, VaultDef: freshDefinition, NoteMetadata: testNoteMetadataIndexer(t)}))
			fresh := openTouchParityStore(t, freshRoot)

			require.Equal(t, noteParitySnapshot(t, ctx, fresh), noteParitySnapshot(t, ctx, incremental))
		})
	}
}

func copyTouchParityNotes(from, to string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	names, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(from, name.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, name.Name()), content, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// noteParityRows is normalised: row identity (note ids) and write timestamps
// are dropped, mtime is deliberately kept out because a fresh index observes
// its own copied files.
type noteParityRows struct {
	Paths         []string
	ContentHashes map[string]string
	Edges         []semdb.GraphDocEdge
	Aliases       map[string][]string
	Properties    []semdb.NotePropertyValueRow
	Targets       []semdb.NoteFragmentTargetRow
	OntologyTypes map[string]string
	OntologyEdges []semdb.OntologyEdgeRow
	OntologyNodes int64
}

func noteParitySnapshot(t *testing.T, ctx context.Context, store *semdb.Store) noteParityRows {
	t.Helper()
	notePaths, err := store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, notePaths)
	require.NoError(t, err)
	hashes := make(map[string]string, len(rows))
	for path, row := range rows {
		hashes[path] = row.ContentHash
	}
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	aliases, err := store.CurrentNoteAliases(ctx)
	require.NoError(t, err)
	properties, err := store.CurrentNotePropertyValues(ctx, notePaths, nil, 0)
	require.NoError(t, err)
	for index := range properties {
		properties[index].NoteID = 0
	}
	targets, err := store.CurrentNoteFragmentTargets(ctx, notePaths, "", "")
	require.NoError(t, err)
	for index := range targets {
		targets[index].NoteID = 0
	}
	types, err := store.OntologyTypesByPaths(ctx, notePaths)
	require.NoError(t, err)
	typeNames := make(map[string]string, len(types))
	for path, row := range types {
		typeNames[path] = row.TypeName
	}
	ontologyEdges, err := store.AllOntologyEdges(ctx, true, 0)
	require.NoError(t, err)
	for index := range ontologyEdges {
		ontologyEdges[index].UpdatedAt = 0
	}
	nodes, err := store.OntologyNodeCount(ctx)
	require.NoError(t, err)
	return noteParityRows{
		Paths: notePaths, ContentHashes: hashes, Edges: edges, Aliases: aliases,
		Properties: properties, Targets: targets,
		OntologyTypes: typeNames, OntologyEdges: ontologyEdges, OntologyNodes: nodes,
	}
}
