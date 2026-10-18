package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestResolveMarkdownPreservesAdmissionAliasesAndAmbiguity(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { summary: String }`, "---\naliases: [ALIAS]\n---\n# Product\n")
	service := NewService(vault, &obsidian.Note{}, store, schema)
	for _, tc := range []struct {
		name, source, target string
		resolves             bool
	}{
		{"root invalid drive", "Source.md", "C%3A/specs/product.md", false},
		{"nested invalid drive", "Notes/Source.md", "C%3A/specs/product.md", false},
		{"nested invalid colon", "Notes/Source.md", "a%3Ab/product.md", false},
		{"confined parent", "Notes/Source.md", "../specs/product.md", true},
		{"root alias", "Source.md", "ALIAS", true},
		{"nested alias", "Notes/Source.md", "ALIAS", true},
		{"confined parent alias", "Notes/Source.md", "../ALIAS", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.NewScope(ScopeOptions{}).Resolve(ctx, ResolveRequest{FromPath: tc.source, Targets: []NodeTarget{{Input: "[target](" + tc.target + ")"}}})
			require.NoError(t, err)
			if tc.resolves {
				require.Len(t, result.Resolved, 1)
				require.Equal(t, "specs/product.md", result.Resolved[0].Ref.NotePath)
				return
			}
			require.Empty(t, result.Resolved)
			require.Len(t, result.Diagnostics, 1)
			require.Equal(t, "target_unresolved", result.Diagnostics[0].Code)
			require.Empty(t, result.Diagnostics[0].Candidates)
		})
	}
	writeFixtureNote(t, vault.Path, "archive/product.md", "# Archived\n")
	_, err := ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	result, err := service.NewScope(ScopeOptions{}).Resolve(ctx, ResolveRequest{Targets: []NodeTarget{{Input: "[choice](product)"}}})
	require.NoError(t, err)
	require.Empty(t, result.Resolved)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, "target_ambiguous", result.Diagnostics[0].Code)
	require.Len(t, result.Diagnostics[0].Candidates, 2)
	require.ElementsMatch(t, []string{"specs/product.md", "archive/product.md"}, []string{result.Diagnostics[0].Candidates[0].NotePath, result.Diagnostics[0].Candidates[1].NotePath})
	writeFixtureNote(t, vault.Path, "Notes/ALIAS.md", "# Local\n")
	_, err = ontologytest.Publish(ctx, testkit.NoteMetadataIndexer(t), vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	local, err := service.NewScope(ScopeOptions{}).Resolve(ctx, ResolveRequest{
		FromPath: "Notes/Source.md", Targets: []NodeTarget{{Input: "[local](ALIAS)"}},
	})
	require.NoError(t, err)
	require.Len(t, local.Resolved, 1)
	require.Equal(t, "Notes/ALIAS.md", local.Resolved[0].Ref.NotePath)
}

func TestResolveMarkdownRejectsAuthoredURIAndDriveNormalization(t *testing.T) {
	ctx := context.Background()
	vault, store, schema := buildFixture(t, `type ProductSpec @node(paths: ["specs/*.md"]) { summary: String }`, "---\naliases: [ALIAS, 'mailto:ALIAS', 'custom+v1:ALIAS', 'C:ALIAS']\n---\n# Product\n")
	service := NewService(vault, &obsidian.Note{}, store, schema)
	for _, source := range []string{"Source.md", "Notes/Source.md"} {
		for _, target := range []string{
			"https://example.test/specs/product.md", "https%3A//example.test/specs/product.md",
			"https://example.test/../../specs/product.md", "https%3A//example.test/../../specs/product.md",
			"//example.test/../../specs/product.md", "%2F%2Fexample.test/../../specs/product.md",
			"C:/../specs/product.md", "C%3A/../specs/product.md", "C%3A%2F%2E%2E%2Fspecs/product.md",
			"mailto:product", "mailto%3Aproduct", "custom+v1:product", "custom+v1%3Aproduct",
			"mailto:ALIAS", "custom+v1:ALIAS", "C:ALIAS", "a:b/../specs/product.md",
		} {
			t.Run(source+"/"+target, func(t *testing.T) {
				result, err := service.NewScope(ScopeOptions{}).Resolve(ctx, ResolveRequest{FromPath: source, Targets: []NodeTarget{{Input: "[target](" + target + ")"}}})
				require.NoError(t, err)
				require.Empty(t, result.Resolved)
				require.Len(t, result.Diagnostics, 1)
				require.Equal(t, "target_unresolved", result.Diagnostics[0].Code)
				require.Empty(t, result.Diagnostics[0].Candidates)
			})
		}
	}
}
