package noderead

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestScopeResolveExplicitTargetsDoesNotListVault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/product.md"]) { title: String @field }`, "# Product\n")
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ScopeOptions{})
	result, err := scope.Resolve(ctx, ResolveRequest{IndexOnly: true,
		Targets: []NodeTarget{
			{Input: `{"notePath":"specs/product.md","kind":"NOTE"}`},
			{Input: "specs/product.md#struct:abc123"},
			{Input: "http://localhost:5173/notes?note=specs/product.md"},
		},
		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 2)
	for _, node := range result.Resolved {
		require.Equal(t, "specs/product.md", node.Ref.NotePath)
		require.Equal(t, ontology.NodeKindNote, node.Ref.Kind)
	}
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, "specs/product.md#struct:abc123", result.Diagnostics[0].Input)
	require.Zero(t, reader.lists)
	require.Zero(t, reader.contents)
}

func TestScopeResolveResolvesEmbeddedBlockLinks(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ProductSpec @node(paths: ["specs/product.md"]) {
  owner: Person @link
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  owner: Person @link
  summary: String @field
}
`, `# Product

## Stories

### Story A

^story-a
summary:: Ship precise context
`)
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ScopeOptions{})

	result, err := scope.Resolve(context.Background(), ResolveRequest{
		IncludeLocators: true,
		Targets:         []NodeTarget{{Input: "[[product#^story-a]]"}},
		Hydrate:         HydrateOptions{Profile: HydrateContent},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	resolved := result.Resolved[0]
	require.Equal(t, ontology.NodeKindEmbedded, resolved.Ref.Kind)
	require.Equal(t, "UserStory", resolved.Ref.TypeName)
	require.Equal(t, "specs/product.md#^story-a", resolved.Locator.SourceLocator)
	require.Equal(t, "[[product#^story-a]]", resolved.Locator.LinkTarget.Wikilink)
	require.Contains(t, resolved.Record.Content, "Ship precise context")
	reader.lists = 0
	identityScope := NewService(vaultDef, reader, store, schema).NewScope(ScopeOptions{})
	identity, err := identityScope.Resolve(context.Background(), ResolveRequest{Targets: []NodeTarget{{Input: "[[product#^story-a]]"}}, Hydrate: HydrateOptions{Profile: HydrateSummary}})
	require.NoError(t, err)
	require.Len(t, identity.Resolved, 1)
	require.Equal(t, "^story-a", identity.Resolved[0].Ref.Fragment)
	_, err = identityScope.Resolve(context.Background(), ResolveRequest{Targets: []NodeTarget{{Input: "[[product#^story-a]]"}}, Hydrate: HydrateOptions{Profile: HydrateSummary}})
	require.NoError(t, err)
	require.Equal(t, 1, reader.lists)
}

func TestScopeResolveReportsAmbiguousAliasCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true)
}
`, `---
id: SPEC-A
aliases:
  - DUPE
---
# Product A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
id: SPEC-B
aliases:
  - DUPE
---
# Product B
`)
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/product.md", "specs/product-b.md"}, nil))
	_, err := ontologytest.Sync(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, []string{"specs/product.md", "specs/product-b.md"}, nil)
	require.NoError(t, err)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	result, err := scope.Resolve(ctx, ResolveRequest{
		Targets: []NodeTarget{{Input: "[[DUPE]]"}},
		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Empty(t, result.Resolved)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, "target_ambiguous", result.Diagnostics[0].Code)
	require.Equal(t, []ontology.NodeRef{
		{NotePath: "specs/product-b.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
		{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
	}, result.Diagnostics[0].Candidates)
}

func TestScopeResolveReportsAmbiguousPreferredIdentifierCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true)
}
`, `---
id: DUPE
---
# Product A
`)
	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
id: dupe
---
# Product B
`)
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/product.md", "specs/product-b.md"}, nil))
	_, err := ontologytest.Sync(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, []string{"specs/product.md", "specs/product-b.md"}, nil)
	require.NoError(t, err)

	for _, input := range []string{"DUPE", "[[DUPE]]"} {
		t.Run(input, func(t *testing.T) {
			scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
			result, err := scope.Resolve(ctx, ResolveRequest{
				Targets: []NodeTarget{{Input: input}},
				Hydrate: HydrateOptions{Profile: HydrateSummary},
			})
			require.NoError(t, err)
			require.Empty(t, result.Resolved)
			require.Len(t, result.Diagnostics, 1)
			require.Equal(t, "target_ambiguous", result.Diagnostics[0].Code)
			require.Equal(t, []ontology.NodeRef{
				{NotePath: "specs/product-b.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
				{NotePath: "specs/product.md", Kind: ontology.NodeKindNote, TypeName: "ProductSpec"},
			}, result.Diagnostics[0].Candidates)
		})
	}

	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	initial, err := scope.Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: "DUPE"}}})
	require.NoError(t, err)
	require.Empty(t, initial.Resolved)
	require.Len(t, initial.Diagnostics, 1)
	require.Equal(t, "target_ambiguous", initial.Diagnostics[0].Code)

	writeFixtureNote(t, vaultDef.Path, "specs/product-b.md", `---
id: UNIQUE
---
# Product B
`)
	require.NoError(t, testkit.NoteMetadataIndexer(t).SyncPaths(ctx, vaultDef, &obsidian.Note{}, store, []string{"specs/product-b.md"}, nil))
	_, err = ontologytest.Sync(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store, []string{"specs/product-b.md"}, nil)
	require.NoError(t, err)
	refreshed, err := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{}).Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: "DUPE"}}})
	require.NoError(t, err)
	require.Empty(t, refreshed.Diagnostics)
	require.Len(t, refreshed.Resolved, 1)
	require.Equal(t, "specs/product.md", refreshed.Resolved[0].Ref.NotePath)
}

func TestScopeResolveUsesFallbackSectionSourceLocator(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type Spec @node(paths: ["typed/*.md"]) {
  summary: String @field
}
`, `# Product

Root fallback prose.

## Findings

Fallback section prose.
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	result, err := scope.Resolve(context.Background(), ResolveRequest{
		IncludeLocators: true,
		Targets:         []NodeTarget{{Input: "[[product#Findings]]"}},
		Hydrate:         HydrateOptions{Profile: HydrateContent},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	resolved := result.Resolved[0]
	require.Equal(t, ontology.NodeKindSection, resolved.Ref.Kind)
	require.Equal(t, ontoschema.FallbackSectionTypeName, resolved.Ref.TypeName)
	require.Equal(t, "specs/product.md#findings-33", resolved.Locator.SourceLocator)
	require.Contains(t, resolved.Record.Content, "Fallback section prose.")

	rows, err := store.OntologyNodesBySourceLocators(context.Background(), []string{resolved.Locator.SourceLocator})
	require.NoError(t, err)
	require.Contains(t, rows, resolved.Locator.SourceLocator)
	require.Equal(t, ontoschema.FallbackSectionTypeName, rows[resolved.Locator.SourceLocator].TypeName)
}

func TestScopeResolveIndexOnlyCanonicalSelectorsUseCatalog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`, `# Product

## Stories

### Story A
summary:: Ship precise context
`)
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"specs/product.md"})
	require.NoError(t, err)
	var canonical ontology.NodeRef
	for _, node := range nodes {
		if node.TypeName == "UserStory" {
			require.NoError(t, json.Unmarshal([]byte(node.NodeRefJSON), &canonical))
			break
		}
	}
	require.Equal(t, ontology.NodeKindEmbedded, canonical.Kind)
	require.NotEmpty(t, canonical.Structural)

	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ScopeOptions{})
	inputs := []string{
		canonical.NotePath + "#struct:" + canonical.Structural,
		canonical.NotePath + "#node:" + canonical.NodeID,
		mustNodeRefJSON(t, ontology.NodeRef{NotePath: canonical.NotePath, Kind: ontology.NodeKindSection, Structural: canonical.Structural}),
	}
	for _, input := range inputs {
		result, err := scope.Resolve(ctx, ResolveRequest{
			Targets:   []NodeTarget{{Input: input}},
			IndexOnly: true,

			Hydrate: HydrateOptions{Profile: HydrateSummary},
		})
		require.NoError(t, err)
		require.Empty(t, result.Diagnostics)
		require.Len(t, result.Resolved, 1)
		require.Equal(t, canonical, result.Resolved[0].Ref)
		require.Equal(t, "Story A", result.Resolved[0].Record.Title)
	}

	missing, err := scope.Resolve(ctx, ResolveRequest{
		Targets:   []NodeTarget{{Input: canonical.NotePath + "#struct:missing"}},
		IndexOnly: true,

		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Empty(t, missing.Resolved)
	require.Len(t, missing.Diagnostics, 1)
	require.Equal(t, "node_unresolved", missing.Diagnostics[0].Code)
	require.Zero(t, reader.contents)
	require.Zero(t, reader.lists)
}

func TestScopeResolveStructuralSelectorReturnsOverlayCanonicalRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vaultDef, store, schema := buildFixture(t, sourceSpanIdentitySchema, "# Product\n\n## Acceptance Criteria\n\n- Target item\n")
	initial := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})
	listed, err := initial.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	original := listed.Items[0].Ref

	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, schema).NewScope(ScopeOptions{ReadOverlay: &ReadOverlay{
		SourceFormat: "markdown",
		UpdatedContentByPath: map[string]string{
			original.NotePath: "# Product\n\n## Acceptance Criteria\n\n- New preceding item\n- Target item\n",
		},
		TouchedPaths: []string{original.NotePath},
	}})
	for _, includeLocators := range []bool{false, true} {
		result, err := scope.Resolve(ctx, ResolveRequest{
			IncludeLocators: includeLocators, Targets: []NodeTarget{{Input: original.NotePath + "#struct:" + original.Structural}},

			Hydrate: HydrateOptions{Profile: HydrateSummary},
		})
		require.NoError(t, err)
		require.Len(t, result.Resolved, 1)
		require.Equal(t, "Target item", result.Resolved[0].Record.Title)
		require.Equal(t, result.Resolved[0].Record.Ref, result.Resolved[0].Ref)
		require.NotEqual(t, original.NodeID, result.Resolved[0].Ref.NodeID)
		require.Equal(t, original.Structural, result.Resolved[0].Ref.Structural)
		require.Zero(t, reader.contents)
		if includeLocators {
			require.Equal(t, result.Resolved[0].Ref, result.Resolved[0].Locator.Ref)
			require.NotNil(t, result.Resolved[0].Locator.LinkTarget)
			require.Equal(t, "Target item", result.Resolved[0].Locator.LinkTarget.DisplayLabel)
		}

	}

}

func TestScopeResolveIndexOnlyRootNodeSelector(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	schemaSDL := strings.ReplaceAll(sourceSpanIdentitySchema, "specs/product.md", "product.md")
	vaultDef, store, _ := buildFixture(t, schemaSDL, "# Ignored\n")
	writeFixtureNote(t, vaultDef.Path, "product.md", "# Product\n\n## Acceptance Criteria\n\n- Target item\n")
	runtime, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	scope := NewService(vaultDef, reader, store, runtime.Schema).NewScope(ScopeOptions{})
	listed, err := scope.TypeInstances(ctx, TypeInstancesRequest{TypeName: "AcceptanceCriterion"})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	ref := listed.Items[0].Ref
	result, err := scope.Resolve(ctx, ResolveRequest{
		Targets:   []NodeTarget{{Input: ref.NotePath + "#node:" + ref.NodeID}},
		IndexOnly: true,

		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, ref, result.Resolved[0].Ref)
	require.Equal(t, "Target item", result.Resolved[0].Record.Title)
	require.Zero(t, reader.contents)
	require.Zero(t, reader.lists)
}

func TestScopeResolveUnwrapsCopiedNoteURLs(t *testing.T) {
	t.Parallel()

	vaultDef, store, schema := buildFixture(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ProductSpec @node(paths: ["specs/product.md"]) {
  owner: Person @link
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`, `# Product

## Stories

### Story A

^story-a
summary:: Ship precise context
`)
	scope := NewService(vaultDef, &obsidian.Note{}, store, schema).NewScope(ScopeOptions{})

	result, err := scope.Resolve(context.Background(), ResolveRequest{
		Targets: []NodeTarget{{Input: "http://localhost:5173/notes?note=specs/product.md#%5Estory-a"}},
		Hydrate: HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, ontology.NodeKindEmbedded, result.Resolved[0].Ref.Kind)
	require.Equal(t, "^story-a", result.Resolved[0].Ref.Fragment)
	require.Equal(t, "UserStory", result.Resolved[0].Ref.TypeName)
}
