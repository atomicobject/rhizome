package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

// Node chunk ordinals restart at zero per node and do not index the note's
// heading sections, so each embedded node hit must survive aggregation and
// show its own body.
func TestFindConnections_EmbeddedNodeHitsKeepTheirOwnText(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write(".rhizome/config.yml", "notes:\n  includes: [\"**/*.md\"]\n")
	write(".rhizome/ontology/schema.graphql", `
type Story implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type Stories implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`)
	write("specs/one.md", `---
type: Spec
summary: One
---

# One

Overview text that is not any story.

## Stories

### Alpha
Alpha story body.

### Beta
Beta story body.
`)
	write("Notes/Q.md", "# Q\n\nalpha\n")

	ctx := context.Background()
	cfg := Config{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: configuredMCPNoteMetadata(t),
	}
	store := newIntelStore(t)
	_, err := ontologytest.Publish(ctx, cfg.NoteMetadata, cfg.VaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	nodes, err := store.OntologyNodesByPaths(ctx, []string{"specs/one.md"})
	require.NoError(t, err)
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vecs, err := prov.EmbedTexts(ctx, []string{"alpha"})
	require.NoError(t, err)
	var ownerIDs []string
	var chunks []codeanchor.IntelChunk
	embeds := map[string]embeddings.Embedding{}
	for _, node := range nodes {
		if node.TypeName != "Story" {
			continue
		}
		chunkID := codeanchor.IntelChunkID(node.NodeID, 0, embeddings.GranularityOntologyNodeBody)
		ownerIDs = append(ownerIDs, node.NodeID)
		chunks = append(chunks, codeanchor.IntelChunk{
			ChunkID: chunkID, OwnerID: node.NodeID, OwnerType: embeddings.OntologyNodeOwnerType,
			Ord: 0, Granularity: embeddings.GranularityOntologyNodeBody,
			Breadcrumb: "One > " + node.Title, Heading: node.Title, ContentHash: "h-" + chunkID,
		})
		embeds[chunkID] = vecs[0]
	}
	require.Len(t, chunks, 2)
	require.NoError(t, store.ReplaceIntelChunks(ctx, ownerIDs, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, embeds))
	addDocSection(t, store, prov, "Notes/Q.md", "Q", "alpha", "section-q", "chunk-q")

	cfg.IntelStore = store
	cfg.EmbedProvider = prov
	cfg.EmbeddingsOn = true
	res, err := FindConnectionsTool(cfg)(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "find_connections",
		Arguments: map[string]any{"note": "Notes/Q.md", "limit": 5},
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	var payload struct {
		Matches []struct {
			Path   string `json:"path"`
			Chunks []struct {
				Heading string `json:"heading"`
				Text    string `json:"text"`
			} `json:"chunks"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &payload))
	require.Len(t, payload.Matches, 1)
	require.Equal(t, "specs/one.md", payload.Matches[0].Path)
	got := map[string]string{}
	for _, chunk := range payload.Matches[0].Chunks {
		got[chunk.Heading] = chunk.Text
	}
	require.Equal(t, map[string]string{"Alpha": "Alpha story body.", "Beta": "Beta story body."}, got)
}

// RECORD nodes in YAML sources project through their format provider, so a
// connection hit on one must still show that record's own text.
func TestFindConnections_RecordNodeHitsShowTheirText(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write(".rhizome/config.yml", "notes:\n  includes: [\"**/*.md\", \"model/*.yaml\"]\n")
	write(".rhizome/ontology/schema.graphql", `
type Service @node(locator: EMBEDDED) @source(shape: RECORD, paths: ["model/services.yaml"], select: "services", key: {field: "id"}, title: ["name", "$key"], space: "selected", logicalKind: "service") {
  id: ID @field(path: "id")
  name: String @field(path: "name")
}
`)
	write("model/services.yaml", "services:\n  - id: A\n    name: Alpha\n    purpose: Books rooms for guests.\n")
	write("Notes/Q.md", "# Q\n\nalpha\n")

	ctx := context.Background()
	cfg := Config{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Root: root, Links: obsidian.LinkTypeBoth, Includes: []string{"**/*.md", "model/*.yaml"}},
		NoteMetadata: configuredMCPNoteMetadata(t),
	}
	store := newIntelStore(t)
	_, err := ontologytest.Publish(ctx, cfg.NoteMetadata, cfg.VaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"model/services.yaml"})
	require.NoError(t, err)
	prov := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	vecs, err := prov.EmbedTexts(ctx, []string{"alpha"})
	require.NoError(t, err)
	var service string
	for _, node := range nodes {
		if node.TypeName == "Service" {
			service = node.NodeID
		}
	}
	require.NotEmpty(t, service)
	chunkID := codeanchor.IntelChunkID(service, 0, embeddings.GranularityOntologyNodeBody)
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{service}, []codeanchor.IntelChunk{{
		ChunkID: chunkID, OwnerID: service, OwnerType: embeddings.OntologyNodeOwnerType,
		Ord: 0, Granularity: embeddings.GranularityOntologyNodeBody, Heading: "Alpha", ContentHash: "h-" + chunkID,
	}}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{chunkID: vecs[0]}))
	addDocSection(t, store, prov, "Notes/Q.md", "Q", "alpha", "section-q", "chunk-q")

	cfg.IntelStore, cfg.EmbedProvider, cfg.EmbeddingsOn = store, prov, true
	res, err := FindConnectionsTool(cfg)(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name:      "find_connections",
		Arguments: map[string]any{"note": "Notes/Q.md", "limit": 5},
	}})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)
	var payload struct {
		Matches []struct {
			Path   string `json:"path"`
			Chunks []struct {
				Text string `json:"text"`
			} `json:"chunks"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].(mcp.TextContent).Text), &payload))
	require.Len(t, payload.Matches, 1)
	require.Len(t, payload.Matches[0].Chunks, 1)
	require.Contains(t, payload.Matches[0].Chunks[0].Text, "Books rooms for guests.")
}
