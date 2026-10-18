package indexing

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSyncOntologyNodeEmbeddings_ExcludesDescriptorOnlyHTML(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Decision @node(paths: ["notes/*.md"]) {
  status: String
}
`), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Decision.md"), []byte("---\nstatus: accepted\n---\n# Decision\nMarkdown body\n"), 0o600))
	const htmlBody = "HTML_BODY_MUST_NOT_REACH_ONTOLOGY_EMBEDDINGS"
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "Reference.html"), []byte("<article>"+htmlBody+"</article>\n"), 0o600))

	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md", "docs/*.html"}}
	indexer := testkit.DescriptorOnlyHTMLNoteMetadataIndexer(t)
	// Establish the published source and ontology catalog without requiring an
	// external embedding service. The subsequent direct semantic sync uses the
	// enabled configuration and a deterministic provider.
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{
		Notes:          repoconfig.LocalVaultConfig{Includes: definition.Includes},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{
		VaultPath: root, VaultDef: definition, NoteMetadata: indexer,
	}))
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{
		Notes:          repoconfig.LocalVaultConfig{Includes: definition.Includes},
		NoteEmbeddings: &embeddings.Config{Enabled: true},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))

	store, cleanup, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	writer := indexwriter.New(ctx, indexwriter.Handlers{ApplyIntelChunks: store.ReplaceIntelChunks, ApplyIntelEmbeddings: store.UpsertEmbeddings, ApplyOntologyNodeReadModel: store.ReplaceOntologyNodeReadModel, ApplyOntologyNodeStates: store.UpsertOntologyNodeEmbeddingStates})
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	provider := &recordingOntologyEmbeddingProvider{}
	providerConfig := embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: provider.Dimensions()}

	require.NoError(t, syncOntologyNodeEmbeddings(
		ctx, definition, indexer, store, provider, providerConfig, schema,
		1, 1, nil, nil, writer, []string{"docs/Reference.html"}, nil, true,
	))
	require.NotEmpty(t, provider.Texts(), "Markdown ontology body should be embedded")
	for _, text := range provider.Texts() {
		require.NotContains(t, text, htmlBody)
		require.NotContains(t, text, "docs/Reference.html")
	}

	beforeFollowup := len(provider.Texts())
	require.NoError(t, syncOntologyNodeEmbeddings(
		ctx, definition, indexer, store, provider, providerConfig, schema,
		1, 1, nil, nil, writer, []string{"docs/Reference.html"}, nil, false,
	))
	require.Len(t, provider.Texts(), beforeFollowup, "descriptor-only HTML must not enter follow-up embedding")
}

type recordingOntologyEmbeddingProvider struct {
	mu    sync.Mutex
	texts []string
}

func (p *recordingOntologyEmbeddingProvider) EmbedTexts(_ context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.mu.Lock()
	p.texts = append(p.texts, texts...)
	p.mu.Unlock()
	result := make([]embeddings.Embedding, len(texts))
	for i := range texts {
		result[i] = embeddings.Embedding{1, 0, 0}
	}
	return result, nil
}

func (p *recordingOntologyEmbeddingProvider) Dimensions() int { return 3 }

func (p *recordingOntologyEmbeddingProvider) Texts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.texts...)
}

var _ embeddings.Provider = (*recordingOntologyEmbeddingProvider)(nil)

func TestSyncOntologyNodeEmbeddingsUsesHTMLProviderVisibleSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte("type Record @node(paths: [\"docs/*.html\"]) { title: String }\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "record.html"), []byte("<article><h1>Provider title</h1><p>Visible HTML evidence</p></article>"), 0600))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"docs/*.html"}}
	indexer := testkit.NoteMetadataIndexer(t)
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{Notes: repoconfig.LocalVaultConfig{Includes: definition.Includes}, NoteEmbeddings: &embeddings.Config{Enabled: false}, CodeEmbeddings: &embeddings.Config{Enabled: false}}))
	require.NoError(t, runUnifiedCoreForTest(t, ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	store, cleanup, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadWrite, RequireEnabled: false})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	writer := indexwriter.New(ctx, indexwriter.Handlers{ApplyIntelChunks: store.ReplaceIntelChunks, ApplyIntelEmbeddings: store.UpsertEmbeddings, ApplyOntologyNodeReadModel: store.ReplaceOntologyNodeReadModel, ApplyOntologyNodeStates: store.UpsertOntologyNodeEmbeddingStates})
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	provider := &recordingOntologyEmbeddingProvider{}
	providerConfig := embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: provider.Dimensions()}
	require.NoError(t, syncOntologyNodeEmbeddings(ctx, definition, indexer, store, provider, providerConfig, schema, 1, 1, nil, nil, writer, []string{"docs/record.html"}, nil, false))
	require.NotEmpty(t, provider.Texts())
	for _, text := range provider.Texts() {
		require.Contains(t, text, "Provider title")
		require.Contains(t, text, "Visible HTML evidence")
		require.NotContains(t, text, "<article>")
	}
}
