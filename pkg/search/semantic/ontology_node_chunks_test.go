package semantic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func TestBuildOntologyNodeChunks_IncludesEmbeddedNodeAndParentContext(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["notes/specs/*.md"]) {
  status: String @field
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "specs", "checkout.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	content := `---
status: draft
---
# Checkout Refresh

Intro belongs to the root spec.

` + strings.Repeat("Root context must remain searchable. ", 500) + `

## User Stories

### Faster checkout
storyId:: US-1
Users need quicker checkout.
`
	require.NoError(t, os.WriteFile(notePath, []byte(content), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/checkout.md")
	require.NoError(t, err)

	set, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	require.Contains(t, set.NotePaths, "notes/specs/checkout.md")

	var storyNodeID string
	var rootNodeID string
	var containerNodeID string
	var rootBody string
	var containerBody string
	var storyBody string
	for _, node := range set.Nodes {
		if node.TypeName == "Spec" {
			rootNodeID = node.NodeID
		}
		if node.TypeName == "UserStoriesSection" {
			containerNodeID = node.NodeID
		}
		if node.TypeName == "UserStory" {
			storyNodeID = node.NodeID
			require.Equal(t, "EMBEDDED", node.NodeKind)
			require.NotEmpty(t, node.ParentNodeID)
			require.Contains(t, node.NodeRefJSON, `"typeName":"UserStory"`)
		}
	}
	require.NotEmpty(t, rootNodeID)
	require.NotEmpty(t, containerNodeID)
	require.NotEmpty(t, storyNodeID)
	require.Equal(t, "draft", ontologyNodeFieldValue(t, set.FieldValues, rootNodeID, "status").ValueNorm)
	for _, chunk := range set.Chunks {
		if chunk.OwnerID == rootNodeID && chunk.Granularity == GranularityOntologyNodeBody && rootBody == "" {
			rootBody = set.Texts[chunk.ChunkID]
		}
		if chunk.OwnerID == containerNodeID && chunk.Granularity == GranularityOntologyNodeBody {
			containerBody = set.Texts[chunk.ChunkID]
		}
		if chunk.OwnerID == storyNodeID && chunk.Granularity == GranularityOntologyNodeBody {
			storyBody = set.Texts[chunk.ChunkID]
		}
	}
	require.Contains(t, rootBody, "- status: draft")
	require.Contains(t, storyBody, "Type: UserStory")
	require.Contains(t, storyBody, "Ancestors: Spec > checkout > userStories / UserStoriesSection > User Stories > stories")
	require.Contains(t, storyBody, "Breadcrumb: notes/specs/checkout.md > UserStory > Faster checkout")
	require.Contains(t, storyBody, "storyId:: US-1")
	require.Contains(t, rootBody, "Intro belongs to the root spec.")
	require.NotContains(t, rootBody, "storyId:: US-1")
	require.NotContains(t, rootBody, "Users need quicker checkout.")
	require.NotEmpty(t, containerBody, "bodyless organizing sections retain an identity-only primary chunk")
	for _, chunk := range set.Chunks {
		require.Equal(t, GranularityOntologyNodeBody, chunk.Granularity)
		if chunk.OwnerID == storyNodeID {
			require.Equal(t, 0, chunk.Ord)
		}
	}

	var storyState codeState
	for _, state := range set.States {
		if state.NodeID == storyNodeID && state.ChunkGranularity == GranularityOntologyNodeBody {
			storyState = codeState{
				schemaSignature: state.EmbeddingSchemaSignature,
				textHash:        state.ChunkTextHash,
			}
		}
	}
	require.NotEmpty(t, storyState.schemaSignature)
	require.NotEmpty(t, storyState.textHash)
	require.False(t, strings.Contains(storyState.schemaSignature, "Spec|"))
	// Field ranking is observed through the projected chunk, including its
	// continuation, rather than by calling the renderer in isolation.
	fields := []*ontology.Field{
		{Name: "optionalZ", Kind: ontology.FieldKindScalar},
		{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "Status"},
		{Name: "required", Kind: ontology.FieldKindScalar, Required: true},
		{Name: "context", Kind: ontology.FieldKindScalar, ContextInclude: true},
		{Name: "identifier", Kind: ontology.FieldKindScalar, IsIdentifier: true},
		{Name: "id", Kind: ontology.FieldKindScalar, IsIdentifier: true, IsPreferredIdentifier: true},
		{Name: "optionalA", Kind: ontology.FieldKindScalar},
		{Name: "identity", Kind: ontology.FieldKindScalar},
		{Name: "relation", Kind: ontology.FieldKindLink},
	}
	byName := make(map[string]*ontology.Field, len(fields))
	bindings := make(map[string]ontology.FieldBinding, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
		bindings[field.Name] = ontology.FieldBinding{Present: true, Values: []string{field.Name + "-3", field.Name + "-1", field.Name + "-2"}}
	}
	bindings["identity"] = ontology.FieldBinding{Present: true, Values: []string{"checkout"}}
	projection.Type.Fields = append(projection.Type.Fields, fields...)
	for name, field := range byName {
		projection.Type.ByName[name] = field
	}
	for name, binding := range bindings {
		projection.Fields[name] = binding
	}
	if schema.Enums == nil {
		schema.Enums = map[string]map[string]struct{}{}
	}
	schema.Enums["Status"] = map[string]struct{}{"ACTIVE": {}, "DRAFT": {}}
	enriched, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	var rootParts []string
	for _, chunk := range enriched.Chunks {
		if chunk.OwnerID == rootNodeID && chunk.Granularity == GranularityOntologyNodeBody {
			rootParts = append(rootParts, enriched.Texts[chunk.ChunkID])
		}
	}
	require.Greater(t, len(rootParts), 1, "long root must produce continuation chunks")
	first := rootParts[0]
	for _, label := range []string{"id:", "identifier:", "context:", "status [ACTIVE|DRAFT]:", "required:", "optionalA:"} {
		require.Contains(t, first, label)
	}
	require.NotContains(t, first, "optionalZ:")
	require.NotContains(t, first, "identity:")
	require.NotContains(t, first, "relation:")
	require.NotContains(t, first, "-3")
	ordered := []string{"id:", "identifier:", "context:", "status [ACTIVE|DRAFT]:", "required:", "optionalA:"}
	for i := 1; i < len(ordered); i++ {
		require.Less(t, strings.Index(first, ordered[i-1]), strings.Index(first, ordered[i]), "field priority %s before %s", ordered[i-1], ordered[i])
	}
	for _, part := range rootParts[1:] {
		require.NotContains(t, part, "Fields:")
		require.Contains(t, part, "Ref: notes/specs/checkout.md")
	}

}

func ontologyNodeFieldValue(t *testing.T, rows []codeanchor.IntelOntologyNodeFieldValue, nodeID, fieldName string) codeanchor.IntelOntologyNodeFieldValue {
	t.Helper()
	for _, row := range rows {
		if row.NodeID == nodeID && row.FieldName == fieldName {
			return row
		}
	}
	t.Fatalf("expected field value %s.%s", nodeID, fieldName)
	return codeanchor.IntelOntologyNodeFieldValue{}
}

func TestBuildOntologyNodeChunks_FallbackUntypedNoteIncludesSectionProse(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["docs/specs/*.md"]) {
  summary: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "untyped.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`# Untyped Research

Root prose should be searchable.

## Findings

The frobnicator requires a pressure valve.

### Detail

Nested section text must survive fallback chunking.
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/untyped.md")
	require.NoError(t, err)
	require.Empty(t, projection.ResolvedType)
	require.Nil(t, projection.Assessment)

	set, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	require.Len(t, set.Nodes, 3)
	var fallbackNodeID string
	var sectionNodeIDs []string
	for _, node := range set.Nodes {
		switch node.TypeName {
		case FallbackNoteTypeName:
			require.Equal(t, "NOTE", node.NodeKind)
			fallbackNodeID = node.NodeID
		case ontology.FallbackSectionTypeName:
			require.Equal(t, "SECTION", node.NodeKind)
			require.Contains(t, node.SourceLocator, "notes/untyped.md#")
			if strings.Contains(node.Title, "Detail") {
				require.NotEmpty(t, node.ParentNodeID)
			}
			sectionNodeIDs = append(sectionNodeIDs, node.NodeID)
		}
	}
	require.NotEmpty(t, fallbackNodeID)
	require.Len(t, sectionNodeIDs, 2)

	var combined string
	owners := map[string]struct{}{}
	for _, chunk := range set.Chunks {
		require.Equal(t, "ontology_node", chunk.OwnerType)
		owners[chunk.OwnerID] = struct{}{}
		if chunk.Granularity == GranularityOntologyNodeBody {
			combined += "\n" + set.Texts[chunk.ChunkID]
		}
	}
	require.Contains(t, owners, fallbackNodeID)
	for _, nodeID := range sectionNodeIDs {
		require.Contains(t, owners, nodeID)
	}
	require.Contains(t, combined, "Root prose should be searchable.")
	require.Contains(t, combined, "Section: Findings")
	require.Contains(t, combined, "The frobnicator requires a pressure valve.")
	require.Contains(t, combined, "Section: Detail")
	require.Contains(t, combined, "Nested section text must survive fallback chunking.")
}

func TestBuildOntologyNodeChunks_DoesNotFallbackInvalidDeclaredType(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["docs/specs/*.md"]) {
  summary: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "broken.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
type: MissingType
---
# Broken

This should not become a fallback note.
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/broken.md")
	require.NoError(t, err)
	require.Empty(t, projection.ResolvedType)
	require.NotNil(t, projection.Assessment)

	set, err := BuildOntologyNodeChunks(schema, projection, embeddings.ProviderConfig{Provider: "test", Model: "deterministic"}, 123)
	require.NoError(t, err)
	require.Empty(t, set.Nodes)
	require.Empty(t, set.Chunks)
}

func TestOntologyNodeSyncer_SkipsEmbeddingWhenSidecarStateMatches(t *testing.T) {
	root, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	countingStore := &countingOntologyNodeStore{Store: store}
	provider := &countingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}),
	}
	syncer := OntologyNodeSyncer{
		Store:        countingStore,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
		BatchSize:    100,
	}

	require.NoError(t, syncer.SyncProjections(context.Background(), projection))
	require.Greater(t, provider.calls.Load(), int64(0))
	require.Zero(t, countingStore.replaceNodes.Load())
	require.Greater(t, countingStore.replaceChunks.Load(), int64(0))

	provider.Reset()
	countingStore.Reset()
	require.NoError(t, syncer.SyncNotePaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, []string{"notes/specs/checkout.md"}, nil))
	require.Zero(t, provider.calls.Load())
	require.Zero(t, countingStore.replaceNodes.Load())
	require.Zero(t, countingStore.replaceChunks.Load())
}

func TestOntologyNodeSyncer_PreservesCatalogOwnedRows(t *testing.T) {
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	catalogModel, err := ontology.BuildIntelOntologyNodeReadModel(schema, projection, 1)
	require.NoError(t, err)
	require.NotEmpty(t, catalogModel.Nodes)
	require.NotEmpty(t, catalogModel.FieldValues)
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, catalogModel))

	countingStore := &countingOntologyNodeStore{Store: store}
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8})
	syncer := OntologyNodeSyncer{
		Store:        countingStore,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
	}
	require.NoError(t, syncer.SyncProjections(ctx, projection))
	require.Zero(t, countingStore.replaceNodes.Load(), "semantic sync must not write ontology catalog rows")

	nodes, err := store.OntologyNodesByPaths(ctx, catalogModel.NotePaths)
	require.NoError(t, err)
	require.Len(t, nodes, len(catalogModel.Nodes))
	nodeIDs := make([]string, 0, len(catalogModel.Nodes))
	for _, node := range catalogModel.Nodes {
		nodeIDs = append(nodeIDs, node.NodeID)
	}
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	require.Len(t, fields, len(catalogModel.FieldValues))
}

func TestOntologyNodeSyncer_EmbedsFallbackUntypedNoteChunks(t *testing.T) {
	// The semantic syncer is responsible for chunks + embeddings of fallback
	// projections. The ontology_nodes NOTE row itself is owned by the catalog
	// sync (see TestBuildIntelOntologyNodeReadModel_UntypedGlobalSourceEmits...
	// in pkg/ontology); writing it here would re-issue per-path destructive
	// Replace and wipe sibling embedded children.
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["docs/specs/*.md"]) {
  summary: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "untyped.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`# Untyped

Search-only fallback prose.
`), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8})

	syncer := OntologyNodeSyncer{
		Store:        store,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
	}
	require.NoError(t, syncer.SyncNotePaths(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, []string{"notes/untyped.md"}, nil))

	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/untyped.md")
	require.NoError(t, err)
	fallback, ok := ontology.AsFallbackNoteProjection(projection)
	require.True(t, ok)
	fallbackNodeID := ontology.OntologyNodeID(fallback.Ref)
	chunks, err := store.IntelChunksByOwners(context.Background(), []string{fallbackNodeID})
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	var bodyChunk string
	for _, chunk := range chunks {
		if chunk.Granularity == GranularityOntologyNodeBody {
			bodyChunk = chunk.ChunkID
		}
	}
	require.NotEmpty(t, bodyChunk)
	vecs, err := store.EmbeddingsByChunkIDs(context.Background(), []string{bodyChunk})
	require.NoError(t, err)
	require.NotEmpty(t, vecs[bodyChunk])
}

func TestOntologyNodeSyncer_EmbedsChangedChunksConcurrentlyWithGate(t *testing.T) {
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := &concurrentRecordingProvider{
		inner: embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Model: "deterministic", Dimensions: 8}),
		delay: 20 * time.Millisecond,
	}
	syncer := OntologyNodeSyncer{
		Store:         store,
		Provider:      provider,
		ProviderInfo:  embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:        schema,
		BatchSize:     1,
		MaxConcurrent: 4,
		EmbedGate:     make(chan struct{}, 2),
	}

	require.NoError(t, syncer.SyncProjections(context.Background(), projection))
	require.Greater(t, provider.calls.Load(), int64(1))
	require.Greater(t, provider.maxInflight.Load(), int64(1))
	require.LessOrEqual(t, provider.maxInflight.Load(), int64(2))
}

func TestOntologyNodeSyncer_UsesProviderDefaultBatchSize(t *testing.T) {
	_, schema, projection := setupOntologyNodeChunkFixture(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := &defaultingProvider{batchSize: 1, maxConcurrency: 1}
	syncer := OntologyNodeSyncer{
		Store:        store,
		Provider:     provider,
		ProviderInfo: embeddings.ProviderConfig{Provider: "test", Model: "deterministic"},
		Schema:       schema,
	}

	require.NoError(t, syncer.SyncProjections(context.Background(), projection))
	sizes := provider.BatchSizes()
	require.Greater(t, len(sizes), 1)
	for _, size := range sizes {
		require.Equal(t, 1, size)
	}
	set, err := BuildOntologyNodeChunks(schema, projection, syncer.ProviderInfo, 0)
	require.NoError(t, err)
	ids := make([]string, 0, len(set.Chunks))
	for _, chunk := range set.Chunks {
		ids = append(ids, chunk.ChunkID)
	}
	vectors, err := store.EmbeddingsByChunkIDs(context.Background(), ids)
	require.NoError(t, err)
	require.Len(t, vectors, len(ids))
}

func TestSplitOntologyBody_PrefersMarkdownBoundaries(t *testing.T) {
	paragraphs := []string{
		"ALPHA-START " + strings.Repeat("alpha ", 8) + "ALPHA-END",
		"BETA-START " + strings.Repeat("beta ", 8) + "BETA-END",
		"```go\nfmt.Println(\"hello\")\n```",
		"GAMMA-START " + strings.Repeat("gamma ", 8) + "GAMMA-TAIL",
	}
	body := strings.Join(paragraphs, "\n\n")

	parts := splitOntologyBody(body, 80)
	require.GreaterOrEqual(t, len(parts), 2)
	for _, part := range parts {
		require.NotEmpty(t, part)
	}
	joined := strings.Join(parts, "\n")
	for _, paragraph := range paragraphs {
		require.Contains(t, joined, paragraph)
	}
	require.Equal(t, 1, strings.Count(joined, paragraphs[2]), "bounded code fence must stay whole")
}

func TestSplitOntologyBody_PreservesTailChunks(t *testing.T) {
	body := strings.Join([]string{
		"part-one " + strings.Repeat("alpha ", 20),
		"part-two " + strings.Repeat("beta ", 20),
		"part-three " + strings.Repeat("gamma ", 20),
		"TAIL-SENTINEL " + strings.Repeat("omega ", 20),
	}, "\n\n")

	parts := splitOntologyBody(body, 90)
	require.Greater(t, len(parts), 3)
	require.Contains(t, strings.Join(parts, "\n"), "TAIL-SENTINEL")
}

func setupOntologyNodeChunkFixture(t *testing.T) (string, *ontology.Schema, *ontology.NodeProjection) {
	t.Helper()
	root := t.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`
type Spec @node(paths: ["notes/specs/*.md"]) {
  status: String @field
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  storyId: String @field
}
`), 0o644))
	notePath := filepath.Join(root, "notes", "specs", "checkout.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(notePath), 0o755))
	require.NoError(t, os.WriteFile(notePath, []byte(`---
status: draft
---
# Checkout Refresh

## User Stories

### Faster checkout
storyId:: US-1
Users need quicker checkout.
`), 0o644))

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/checkout.md")
	require.NoError(t, err)
	return root, schema, projection
}

type codeState struct {
	schemaSignature string
	textHash        string
}

type countingOntologyNodeStore struct {
	*semdb.Store
	replaceNodes  atomic.Int64
	replaceChunks atomic.Int64
}

func (s *countingOntologyNodeStore) ReplaceOntologyNodeReadModel(ctx context.Context, model codeanchor.IntelOntologyNodeReadModel) error {
	s.replaceNodes.Add(1)
	return s.Store.ReplaceOntologyNodeReadModel(ctx, model)
}

func (s *countingOntologyNodeStore) ReplaceIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error {
	s.replaceChunks.Add(1)
	return s.Store.ReplaceIntelChunks(ctx, ownerIDs, chunks)
}

func (s *countingOntologyNodeStore) Reset() {
	s.replaceNodes.Store(0)
	s.replaceChunks.Store(0)
}

type concurrentRecordingProvider struct {
	inner       embeddings.Provider
	delay       time.Duration
	calls       atomic.Int64
	inflight    atomic.Int64
	maxInflight atomic.Int64
}

func (p *concurrentRecordingProvider) Dimensions() int { return p.inner.Dimensions() }

func (p *concurrentRecordingProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	p.calls.Add(1)
	current := p.inflight.Add(1)
	for {
		max := p.maxInflight.Load()
		if current <= max || p.maxInflight.CompareAndSwap(max, current) {
			break
		}
	}
	defer p.inflight.Add(-1)
	if p.delay > 0 {
		timer := time.NewTimer(p.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return p.inner.EmbedTexts(ctx, texts)
}
