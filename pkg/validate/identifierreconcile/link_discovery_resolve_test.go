package identifierreconcile

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestIdentifierLinkResolutionPreservesFragmentKindAndBaseAmbiguity(t *testing.T) {
	root := ontology.NodeRef{NotePath: "notes/a.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	heading := ontology.NodeRef{NotePath: root.NotePath, Fragment: "foo", TypeName: "Section", Kind: ontology.NodeKindSection}
	block := ontology.NodeRef{NotePath: root.NotePath, Fragment: "^foo", NodeID: "foo", TypeName: "Story", Kind: ontology.NodeKindEmbedded}
	inventory := &identifierLinkResolutionInventory{
		paths: map[string][]ontology.NodeRef{"notes/a.md": {root}}, fragments: make(map[string][]ontology.NodeRef),
		rewriteRefs: map[string]struct{}{repairRefKey(block): {}},
	}
	inventory.indexFragment(heading, heading)
	inventory.indexFragment(block, block)
	inventory.normalize()

	require.Equal(t, []ontology.NodeRef{heading}, inventory.withFragment([]ontology.NodeRef{root}, "foo"))
	require.Equal(t, []ontology.NodeRef{block}, inventory.withFragment([]ontology.NodeRef{root}, "^foo"))

	other := ontology.NodeRef{NotePath: "notes/b.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	ambiguous := []ontology.NodeRef{root, other}
	require.Equal(t, ambiguous, inventory.withFragment(ambiguous, "^foo"), "a fragment cannot silently disambiguate a duplicate base identity")
}

func TestIdentifierFieldLocatorFragmentsAreScopedToOwningNote(t *testing.T) {
	first := ontology.NodeRef{NotePath: "notes/a.md", Fragment: "^SAME", NodeID: "SAME", TypeName: "Story", Kind: ontology.NodeKindEmbedded}
	second := ontology.NodeRef{NotePath: "notes/b.md", Fragment: "^SAME", NodeID: "SAME", TypeName: "Story", Kind: ontology.NodeKindEmbedded}
	index := make(map[string][]ontology.NodeRef)
	indexProjectionLocators(index, first)
	indexProjectionLocators(index, second)
	normalizeFieldCandidateIndex(index)

	require.Equal(t, []ontology.NodeRef{first}, index[scopedFieldLocatorKey("notes/a.md", "#^SAME")])
	require.Equal(t, []ontology.NodeRef{second}, index[scopedFieldLocatorKey("notes/b.md", "^SAME")])
	require.Empty(t, index[normalizeFieldLocator("#^SAME")], "fragment-only locators must never use a vault-global key")
}

func TestIdentifierLinkResolutionPreservesPhysicalDuplicateTargets(t *testing.T) {
	field := &ontology.Field{Name: "id", IsIdentifier: true, IsPreferredIdentifier: true}
	noteType := &ontology.NoteType{Name: "Story", Fields: []*ontology.Field{field}}
	semantic := ontology.NodeRef{NotePath: "specs/spec.md", Fragment: "^DUP", TypeName: "Story", Kind: ontology.NodeKindEmbedded}
	first := semantic
	first.NodeID = "first"
	first.Structural = "first-structure"
	second := semantic
	second.NodeID = "second"
	second.Structural = "second-structure"
	projections := []*ontology.NodeProjection{
		{Ref: first, ResolvedType: "Story", Type: noteType, Fields: map[string]ontology.FieldBinding{"id": {Present: true, Values: []string{"DUP"}}}},
		{Ref: second, ResolvedType: "Story", Type: noteType, Fields: map[string]ontology.FieldBinding{"id": {Present: true, Values: []string{"DUP"}}}},
	}
	rewrite := reference.IdentifierRewrite{
		Mode: reference.IdentifierRewritePreferredRekey, OldRef: semantic, NewRef: semantic,
		OldIdentifier: "DUP", NewIdentifier: "SPEC-0002", PreferredField: "id", AliasesField: "aliases",
	}
	inventory, err := newIdentifierLinkResolutionInventory(nil, projections, nil, []reference.IdentifierRewrite{rewrite})
	require.NoError(t, err)
	content := "[[DUP]]"
	scan := obsidian.ScanStructuredLinkSnapshot(content)
	resolutions := inventory.resolutionsFor("notes/inbound.md", scan.Links)
	require.Len(t, resolutions, 1)
	require.Len(t, resolutions[0].Candidates, 2, "physical nodes sharing a semantic locator remain ambiguous")

	plan := reference.PlanStructuredLinkRewrites(reference.StructuredLinkRewriteInput{
		NotePath: "notes/inbound.md", Content: content, LinkSnapshot: &scan,
		Rewrites: []reference.IdentifierRewrite{rewrite}, Resolutions: resolutions,
	})
	require.Empty(t, plan.Edits)
	require.Len(t, plan.Diagnostics, 1)
	require.Equal(t, reference.LinkRewriteDiagnosticAmbiguousTarget, plan.Diagnostics[0].Kind)
}

func TestIdentifierLinkResolutionResolvesSourceRelativeWikiPaths(t *testing.T) {
	target := ontology.NodeRef{NotePath: "specs/SPEC-0001.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	inventory := &identifierLinkResolutionInventory{
		paths: make(map[string][]ontology.NodeRef), identities: make(map[string][]ontology.NodeRef),
		fragments: make(map[string][]ontology.NodeRef), rewriteRefs: make(map[string]struct{}),
	}
	inventory.indexPath(target.NotePath, target)
	inventory.normalize()

	for _, content := range []string{"[[../specs/SPEC-0001]]", `[[..\specs\SPEC-0001]]`} {
		scan := obsidian.ScanStructuredLinkSnapshot(content)
		require.Len(t, scan.Links, 1)
		resolutions := inventory.resolutionsFor("notes/inbound.md", scan.Links)
		require.Equal(t, []ontology.NodeRef{target}, resolutions[0].Candidates, content)
	}
}

func TestIdentifierLinkResolutionRejectsPathContainmentEscapesBeforeBasenameFallback(t *testing.T) {
	target := ontology.NodeRef{NotePath: "specs/SPEC-0001.md", TypeName: "Spec", Kind: ontology.NodeKindNote}
	inventory := &identifierLinkResolutionInventory{
		paths: make(map[string][]ontology.NodeRef), identities: make(map[string][]ontology.NodeRef),
		fragments: make(map[string][]ontology.NodeRef), rewriteRefs: make(map[string]struct{}),
	}
	inventory.indexPath(target.NotePath, target)
	inventory.indexIdentity("SPEC-0001", target)
	inventory.normalize()

	tests := []struct {
		name    string
		content string
	}{
		{name: "wiki forward slash escape", content: "[[../../specs/SPEC-0001]]"},
		{name: "wiki backslash escape", content: `[[..\..\specs\SPEC-0001]]`},
		{name: "markdown escape", content: "[spec](../../specs/SPEC-0001)"},
		{name: "markdown absolute", content: "[spec](/specs/SPEC-0001)"},
		{name: "markdown backslash absolute", content: `[spec](\specs\SPEC-0001)`},
		{name: "markdown UNC", content: `[spec](\\host\specs\SPEC-0001)`},
		{name: "wiki absolute", content: "[[/specs/SPEC-0001]]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scan := obsidian.ScanStructuredLinkSnapshot(tt.content)
			require.Len(t, scan.Links, 1)
			resolutions := inventory.resolutionsFor("notes/inbound.md", scan.Links)
			require.Len(t, resolutions, 1)
			require.Empty(t, resolutions[0].Candidates, "containment rejection must not fall back to basename or identifier resolution")
		})
	}
}
