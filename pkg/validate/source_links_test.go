package validate

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBrokenLinksAgreeWithProviderResolvedEdges(t *testing.T) {
	for _, tc := range []struct {
		name        string
		files       map[string]string
		wantBroken  []string
		wantIndexed bool
	}{
		{name: "relative Markdown basename", files: map[string]string{"sub/source.md": "[open](Foo.md)\n", "Foo.md": "# Foo\n"}, wantIndexed: true},
		{name: "alias", files: map[string]string{"source.md": "[[Old name]]\n", "Target.md": "---\naliases: [Old name]\n---\nBody\n"}, wantIndexed: true},
		{name: "case-only note spelling", files: map[string]string{"source.md": "[[TaRgEt]]\n", "Target.md": "# Target\n"}, wantIndexed: true},
		{name: "dotted basename", files: map[string]string{"source.md": "[[note.with.dots]]\n", "note.with.dots.md": "# Note\n"}, wantIndexed: true},
		{name: "attachment intentionally has no indexed edge", files: map[string]string{"source.md": "[[figure.png]]\n", "figure.png": "synthetic image placeholder"}},
		{name: "ignored note intentionally has no indexed edge", files: map[string]string{"source.md": "[[ignored/hidden#Inside]]\n", "ignored/hidden.md": "## Inside\n", ".rhizome/ignore": "ignored/**\n"}},
		{name: "HTML broken href", files: map[string]string{"source.html": "<html><body><a href=\"missing.html\">missing</a></body></html>"}, wantBroken: []string{"missing.html"}},
		{name: "Markdown fragment into HTML", files: map[string]string{"source.md": "[[effort.html#Deviations]]\n", "effort.html": "<html><body><h2 id=\"Deviations\">Deviations</h2></body></html>"}, wantIndexed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.files[".rhizome/config.yml"] = "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n"
			require.NoError(t, writeFixtureFiles(root, tc.files))
			run := RunContext{VaultPath: root, VaultDef: obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", "**/*.html"}, Links: obsidian.LinkTypeBoth}, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t)}
			snapshot, err := captureValidationRunSnapshot(context.Background(), run)
			require.NoError(t, err)
			run.sourceSnapshot = snapshot
			broken, err := sourceBrokenLinks(context.Background(), run, ScanOptions{IncludeImages: true})
			require.NoError(t, err)
			var actual []string
			for _, link := range broken {
				actual = append(actual, link.Target)
			}
			require.Equal(t, tc.wantBroken, actual)
			indexed := false
			for _, source := range snapshot.sources {
				if len(source.Links) > 0 {
					indexed = true
				}
				for _, link := range source.Links {
					for _, finding := range broken {
						require.False(t, finding.Source == link.SourcePath.String() && finding.Target == link.TargetInput, "indexed provider edge cannot be a missing-note finding")
					}
				}
			}
			require.Equal(t, tc.wantIndexed, indexed)
		})
	}
}

func TestSourceFragmentFactsCoverMarkdownSlugsAndDuplicateHeadings(t *testing.T) {
	indexer := testNoteMetadata(t)
	source, err := indexer.ProjectNoteSource("note.md", "# Root\n\n## Setup details!\n\n## Setup details!\n\nParagraph\n^block\n", 0)
	require.NoError(t, err)
	for _, fragment := range []string{"Root", "Setup details!", "setup-details", "setup-details-1", "^block"} {
		require.True(t, sourceHasFragment(source.FragmentTargets, fragment, true), fragment)
	}
	require.False(t, sourceHasFragment(source.FragmentTargets, "setup-details-2", true))
}

func TestSourceLinkResolutionRequiresExplicitProviderRuntime(t *testing.T) {
	_, err := validationLinksFor(context.Background(), RunContext{NoteMetadata: notemeta.Indexer{}})
	require.Error(t, err)
}

func TestOrphanAnchorsCountHTMLReferencesAndIgnoreSourceExamples(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n",
		"target.md":           "---\nexample: '^frontmatter-example'\n---\n# Target\n^kept\n\n```md\n^code-example\n```\n",
		"source.html":         "<html><body><a href=\"target.md#^kept\">open</a></body></html>",
	}))
	result := RunOrphanBlockIDs(context.Background(), sourceLinkRunContext(t, root))
	require.Empty(t, result.Error)
	require.Empty(t, result.Issues)
	require.True(t, result.OK)
}

func TestLinkHygieneUsesMarkdownFactsAndPreservesDestinationSyntax(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n",
		"target.md":           "# Target\n",
		"nested/source.md":    "[label](</target.md> \"keep title\")\n",
		"examples.html":       "<html><body><pre>[[target.md]] [label](/target.md)</pre></body></html>",
	}))
	run := sourceLinkRunContext(t, root)
	result := RunLinkHygiene(run)
	require.Empty(t, result.Error)
	require.Len(t, result.Issues, 1)
	require.Equal(t, "nested/source.md", result.Issues[0].Path)
	require.Len(t, result.Fixes, 1)
	require.Len(t, result.Fixes[0].Edits, 1)
	edit := result.Fixes[0].Edits[0]
	updated, err := ApplyFixEditInMemory("[label](</target.md> \"keep title\")\n", edit)
	require.NoError(t, err)
	require.Equal(t, "[label](<../target.md> \"keep title\")\n", updated)
}

func TestBlockIDRewritePatchesHTMLFragmentWithoutChangingVisibleText(t *testing.T) {
	root := t.TempDir()
	const original = "<html><body><a href=\"target.md#^old\">^old example</a></body></html>"
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n",
		"target.md":           "# Target\n^old\n", "source.html": original,
	}))
	run := sourceLinkRunContext(t, root)
	updated, count, handled, err := applyBlockFragmentRewrite(run, "source.html", original, "target.md#^old", "target.md#^new")
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, 1, count)
	require.Equal(t, "<html><body><a href=\"target.md#^new\">^old example</a></body></html>", updated)
}

func sourceLinkRunContext(t *testing.T, root string) RunContext {
	t.Helper()
	run := linkHygieneRunContext(t, root)
	run.VaultDef = obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", "**/*.html"}, Links: obsidian.LinkTypeBoth}
	return run
}

func TestOrphanAnchorsIgnoreCodeAndFrontmatterReferences(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md":   "# Target\n^orphan\n",
		"examples.md": "---\nexample: '[[target#^orphan]]'\n---\n\n```md\n[[target#^orphan]]\n```\n\n`[[target#^orphan]]`\n\n    [[target#^orphan]]\n",
	}))
	result := RunOrphanBlockIDs(context.Background(), sourceLinkRunContext(t, root))
	require.Empty(t, result.Error)
	require.Len(t, result.Issues, 1)
	require.Equal(t, "orphan_block_id", result.Issues[0].Code)
	require.Equal(t, "target.md", result.Issues[0].Path)
	require.Len(t, result.Fixes, 1)
	require.Equal(t, FixSafetySafe, result.Fixes[0].Safety)
}
