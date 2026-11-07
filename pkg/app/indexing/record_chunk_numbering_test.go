package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

const positionalEntrySchema = `type Host @node(default:true){status:String}
type Entry @node(locator:EMBEDDED) @source(shape:RECORD,paths:["entries/*.yaml"],select:"entries",key:null,title:["$key"],space:"test",logicalKind:"entry"){ name:String @field(path:"name") }
`

type recordChunkVault struct {
	root       string
	definition obsidian.VaultDefinition
	provider   *recordingOntologyEmbeddingProvider
}

func newRecordChunkVault(t *testing.T, files map[string]string) *recordChunkVault {
	t.Helper()
	root := t.TempDir()
	files[".rhizome/ontology/schema.graphql"] = positionalEntrySchema
	for path, body := range files {
		writeRecordChunkFile(t, root, path, body)
	}
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"entries/*.yaml"}}
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{
		Notes:          repoconfig.LocalVaultConfig{Includes: definition.Includes},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	return &recordChunkVault{root: root, definition: definition, provider: &recordingOntologyEmbeddingProvider{}}
}

func writeRecordChunkFile(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
}

// index publishes the catalog, then runs the ontology body pass the unified
// follow-up runs for the given changed and deleted paths.
func (v *recordChunkVault) index(t *testing.T, changed, deleted []string, rebuilt bool) {
	t.Helper()
	ctx := context.Background()
	indexer := testkit.NoteMetadataIndexer(t)
	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{VaultPath: v.root, VaultDef: v.definition, NoteMetadata: indexer}))
	store, cleanup, err := indexstore.Open(v.root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	defer cleanup()
	writer := indexwriter.New(ctx, indexwriter.Handlers{ApplyIntelChunks: store.ReplaceIntelChunks, ApplyIntelEmbeddings: store.UpsertEmbeddings, ApplyOntologyNodeReadModel: store.ReplaceOntologyNodeReadModel, ApplyOntologyNodeStates: store.UpsertOntologyNodeEmbeddingStates, ApplyEmbeddingCache: store.UpsertEmbeddingCache})
	schema, err := ontoschema.LoadSchema(v.root)
	require.NoError(t, err)
	providerConfig := embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: v.provider.Dimensions()}
	require.NoError(t, syncOntologyNodeEmbeddings(ctx, v.definition, indexer, store, v.provider, providerConfig, schema, 8, 1, nil, nil, writer, changed, deleted, rebuilt))
	require.NoError(t, writer.Close())
}

type recordChunkState struct {
	catalogTitles map[string]string
	headings      map[string]string
	textHashes    map[string]string
}

func (v *recordChunkVault) state(t *testing.T) recordChunkState {
	t.Helper()
	ctx := context.Background()
	store, cleanup, err := indexstore.Open(v.root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	defer cleanup()
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"entries/a.yaml", "entries/b.yaml"})
	require.NoError(t, err)
	out := recordChunkState{catalogTitles: map[string]string{}, headings: map[string]string{}, textHashes: map[string]string{}}
	var owners []string
	for _, node := range nodes {
		if node.TypeName == "Entry" {
			out.catalogTitles[node.NodeID] = node.Title
			owners = append(owners, node.NodeID)
		}
	}
	chunks, err := store.IntelChunksByOwners(ctx, owners)
	require.NoError(t, err)
	for _, chunk := range chunks {
		out.headings[chunk.OwnerID] = chunk.Heading
		out.textHashes[chunk.ChunkID] = chunk.ContentHash
	}
	return out
}

func TestRecordChunksKeepComposedPositionalNumbering(t *testing.T) {
	vault := newRecordChunkVault(t, map[string]string{
		"entries/a.yaml": "entries: [{name: alpha}]\n",
		"entries/b.yaml": "entries: [{name: beta}]\n",
	})
	vault.index(t, nil, nil, true)
	full := vault.state(t)
	require.ElementsMatch(t, []string{"1", "2"}, mapValues(full.catalogTitles))
	require.Equal(t, full.catalogTitles, full.headings, "chunk headings use the catalog's composed numbering")

	// Adding a row to a.yaml renumbers b.yaml's unchanged row in the catalog;
	// its chunk must follow even though only a.yaml changed.
	writeRecordChunkFile(t, vault.root, "entries/a.yaml", "entries: [{name: alpha}, {name: gamma}]\n")
	before := len(vault.provider.Texts())
	vault.index(t, []string{"entries/a.yaml"}, nil, false)
	incremental := vault.state(t)
	require.ElementsMatch(t, []string{"1", "2", "3"}, mapValues(incremental.catalogTitles))
	require.Equal(t, incremental.catalogTitles, incremental.headings)

	fresh := newRecordChunkVault(t, map[string]string{
		"entries/a.yaml": "entries: [{name: alpha}, {name: gamma}]\n",
		"entries/b.yaml": "entries: [{name: beta}]\n",
	})
	fresh.index(t, nil, nil, true)
	require.Equal(t, fresh.state(t), incremental, "incremental chunks equal a fresh full index")

	changedTexts := 0
	for id, hash := range incremental.textHashes {
		if full.textHashes[id] != hash {
			changedTexts++
		}
	}
	require.Len(t, vault.provider.Texts()[before:], changedTexts, "only changed chunk text reaches the provider")

	// Deleting a.yaml renumbers b.yaml back to 1.
	require.NoError(t, os.Remove(filepath.Join(vault.root, "entries", "a.yaml")))
	vault.index(t, nil, []string{"entries/a.yaml"}, false)
	deleted := vault.state(t)
	require.Equal(t, []string{"1"}, mapValues(deleted.catalogTitles))
	require.Equal(t, deleted.catalogTitles, deleted.headings)

	before = len(vault.provider.Texts())
	vault.index(t, nil, nil, true)
	require.Len(t, vault.provider.Texts(), before, "unchanged chunk text is never re-embedded")
}

func mapValues(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
