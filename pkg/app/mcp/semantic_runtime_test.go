package mcp

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type semanticMetadataRuntime struct {
	*mutableRuntimeView
	metadata notemeta.Indexer
}

func (r semanticMetadataRuntime) NoteMetadataIndexer() notemeta.Indexer { return r.metadata }

func TestSemanticQueryUsesLiveMetadataRuntime(t *testing.T) {
	root := t.TempDir()
	store := newIntelStore(t)
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	addDocSection(t, store, provider, "Alpha.md", "Alpha", "kickoff project", "section1", "chunk1")
	config := Config{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStore: store,
		EmbedProvider: provider, EmbeddingsOn: true,
		Runtime: semanticMetadataRuntime{
			mutableRuntimeView: &mutableRuntimeView{snapshot: runtimeview.Snapshot{IntelStore: store, NoteProvider: provider}},
			metadata:           configuredMCPNoteMetadata(t),
		},
	}
	result, err := SemanticQueryUnifiedWithOptions(context.Background(), config, SemanticQueryOptions{Query: "kickoff project", Limit: 10})
	require.NoError(t, err)
	paths := make([]string, 0, len(result.Matches))
	for _, match := range result.Matches {
		paths = append(paths, match.Path)
	}
	require.Contains(t, paths, "Alpha.md")
	config.Runtime = nil
	_, err = SemanticQueryUnifiedWithOptions(context.Background(), config, SemanticQueryOptions{Query: "kickoff project", Limit: 10})
	require.ErrorContains(t, err, "semantic query requires note metadata runtime")
}
