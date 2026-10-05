package mcp

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

func BenchmarkSemanticQueryToolDeterministic(b *testing.B) {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vaultPath := b.TempDir()
	store := newIntelStore(b)
	addDocSection(b, store, provider, "Alpha.md", "Alpha", "kickoff project transaction invariants", "section1", "chunk1")
	tool := SemanticQueryTool(Config{
		VaultPath: vaultPath, VaultDef: obsidian.VaultDefinition{Path: vaultPath},
		EmbedProvider: provider, EmbeddingsOn: true, IntelStore: store, SessionStore: store,
	})
	request := mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "semantic_query", Arguments: map[string]any{
			"queries": []string{"kickoff project transaction invariants"}, "limit": 10,
		},
	}}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := tool(context.Background(), request)
		if err != nil {
			b.Fatal(err)
		}
		if result == nil || result.IsError {
			b.Fatalf("semantic query failed: %#v", result)
		}
	}
}
