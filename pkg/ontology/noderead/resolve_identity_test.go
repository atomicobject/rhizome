package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveAuthoredWikilinksBeforeCanonicalPathReads(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { summary: String }`, "# Product\n\n## Detail\n\nAcceptance body.\n\n^acceptance\n")
	service := NewService(vault, &obsidian.Note{}, store, schema)
	for _, target := range []string{"[[specs/product]]", "[[../specs/product]]", "[[../specs/product#^acceptance|Acceptance]]"} {
		t.Run(target, func(t *testing.T) {
			result, err := service.NewScope(ctx, ScopeOptions{}).Resolve(ctx, ResolveRequest{
				Targets:  []NodeTarget{{Input: "specs/product.md"}, {Input: target}},
				FromPath: "efforts/plan.md",
				Hydrate:  HydrateOptions{Profile: HydrateSummary},
			})
			require.NoError(t, err)
			require.Len(t, result.Resolved, 2)
			require.Equal(t, "specs/product.md", result.Resolved[1].Ref.NotePath)
		})
	}
}

func TestResolveBareNotePathHydratesSummaryRecordInOneCall(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
type ReferenceDoc @node(paths: ["specs/*.md"]) {
  summary: String
}`, `---
summary: Indexed summary
---
# Product
`)

	result, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{}).Resolve(ctx, ResolveRequest{
		OmitLocators: true,
		Targets:      []NodeTarget{{Input: "specs/product.md"}},
		Hydrate:      HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, "specs/product.md", result.Resolved[0].Record.Path)
	require.Equal(t, "ReferenceDoc", result.Resolved[0].Record.TypeName)
	require.Equal(t, "Indexed summary", result.Resolved[0].Record.Frontmatter["summary"])
	require.Empty(t, result.Resolved[0].Record.Content)
}

func TestSummaryRecordCarriesIndexedIssueState(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
type ReferenceDoc @node(paths: ["specs/*.md"]) {
  owner: String!
}`, "# Product\n")

	result, err := NewService(vault, &obsidian.Note{}, store, schema).NewScope(ctx, ScopeOptions{}).Resolve(ctx, ResolveRequest{
		OmitLocators: true,
		Targets:      []NodeTarget{{Input: "specs/product.md"}},
		Hydrate:      HydrateOptions{Profile: HydrateSummary},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.True(t, result.Resolved[0].Record.HasIssues, "a note missing a required field has issues on the summary path")
}

func TestRelativeWikilinksDoNotFallBackToAnotherDirectory(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { summary: String }`, "# Product\n")
	writeFixtureNote(t, vault.Path, "archive/product.md", "# Archived\n")
	_, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	service := NewService(vault, &obsidian.Note{}, store, schema)
	result, err := service.NewScope(ctx, ScopeOptions{}).Resolve(ctx, ResolveRequest{
		FromPath: "efforts/plan.md", Targets: []NodeTarget{{Input: "[[../specs/product]]"}},
	})
	require.NoError(t, err)
	require.Len(t, result.Resolved, 1)
	require.Equal(t, "specs/product.md", result.Resolved[0].Ref.NotePath)
	missing, err := service.NewScope(ctx, ScopeOptions{}).Resolve(ctx, ResolveRequest{
		FromPath: "efforts/plan.md", Targets: []NodeTarget{{Input: "[[../missing/product]]"}},
	})
	require.NoError(t, err)
	require.Empty(t, missing.Resolved)
	require.NotEmpty(t, missing.Diagnostics)
}

func TestResolveOmitLocatorsPreservesCatalogIdentityAndExplicitFixes(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `
type ProductSpec @node(paths: ["specs/product.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
type StoriesSection implements Section { stories: [UserStory!] @contains(level: H3) }
type UserStory implements Section @node(locator: EMBEDDED) { summary: String @field }
`, "# Product\n\n## Stories\n\n### Story A\nsummary:: Needs a block ID\n")
	reader := &countingNoteReader{Note: &obsidian.Note{}}
	service := enableTestLinkApply(t, NewService(vault, reader, store, schema))
	request := ResolveRequest{Targets: []NodeTarget{{Input: "specs/product.md"}}, Hydrate: HydrateOptions{Profile: HydrateSummary}}
	full, err := service.NewScope(ctx, ScopeOptions{}).Resolve(ctx, request)
	require.NoError(t, err)
	require.Len(t, full.Resolved, 1)
	reader.contents, reader.lists = 0, 0
	request.OmitLocators = true
	identity, err := service.NewScope(ctx, ScopeOptions{}).Resolve(ctx, request)
	require.NoError(t, err)
	require.Len(t, identity.Resolved, 1)
	require.Equal(t, full.Resolved[0].Ref, identity.Resolved[0].Ref)
	require.Equal(t, full.Resolved[0].Record, identity.Resolved[0].Record)
	require.Equal(t, "ProductSpec", identity.Resolved[0].Ref.TypeName)
	require.Empty(t, identity.Resolved[0].Locator)
	require.Zero(t, reader.contents)
	require.Zero(t, reader.lists)

	request.Targets = []NodeTarget{{Input: "specs/product.md#Story A"}}
	request.EnsureLinkTarget = ontology.EnsureLinkTargetPlan
	fixScope := service.NewScope(ctx, ScopeOptions{})
	planned, err := fixScope.Resolve(ctx, request)
	require.NoError(t, err)
	require.Len(t, planned.Resolved, 1)
	require.NotNil(t, planned.FixPlan)
	require.NotEmpty(t, planned.FixPlan.Actions)
	require.Equal(t, ontology.NodeLocatorRequiresFix, planned.Resolved[0].Locator.Status)
	request.EnsureLinkTarget = ontology.EnsureLinkTargetApply
	fixed, err := fixScope.Resolve(ctx, request)
	require.NoError(t, err)
	require.Len(t, fixed.Resolved, 1)
	require.Equal(t, ontology.NodeLocatorLinkable, fixed.Resolved[0].Locator.Status)
	require.Contains(t, fixed.Resolved[0].Locator.LinkTarget.Wikilink, "#^")
}

func TestResolveOmitLocatorsRetainsRootBasenameAmbiguity(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ name, target, sibling, body string }{
		{"root basename", "README.md", "nested/README.md", "# Root\n"},
		{"product basename", "product", "other/product.md", "# Product\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { summary: String }`, tc.body)
			if tc.name == "root basename" {
				writeFixtureNote(t, vault.Path, "README.md", "# Root\n")
			}
			writeFixtureNote(t, vault.Path, tc.sibling, "# Sibling\n")
			service := NewService(vault, &obsidian.Note{}, store, schema)
			request := ResolveRequest{Targets: []NodeTarget{{Input: tc.target}}, Hydrate: HydrateOptions{Profile: HydrateSummary}}
			full, err := service.NewScope(ctx, ScopeOptions{}).Resolve(ctx, request)
			require.NoError(t, err)
			require.Empty(t, full.Resolved)
			require.NotEmpty(t, full.Diagnostics)
			require.Equal(t, "target_ambiguous", full.Diagnostics[0].Code)
			require.Len(t, full.Diagnostics[0].Candidates, 2)
			request.OmitLocators = true
			identity, err := service.NewScope(ctx, ScopeOptions{}).Resolve(ctx, request)
			require.NoError(t, err)
			require.Equal(t, full, identity)
		})
	}
}
