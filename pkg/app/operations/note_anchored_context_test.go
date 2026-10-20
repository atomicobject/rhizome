package operations

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const specGuideGlob = "docs/specs/**/*.md"

// setupGlobAnchoredNotes writes a spec note and a guide note whose glob anchor
// covers every spec, including the guide itself, and ingests both anchors.
// specBody follows the spec's heading.
func setupGlobAnchoredNotes(t *testing.T, specBody string) (root, specPath, guidePath string, svc *codeanchor.Service) {
	t.Helper()
	root = t.TempDir()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))

	specPath = filepath.Join(root, "docs", "specs", "feature.md")
	guidePath = filepath.Join(root, "docs", "specs", "GUIDE.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(specPath), 0o755))
	specContent := "---\nsummary: \"feature spec\"\n---\n# Feature\n" + specBody
	guideContent := "---\nsummary: \"how to write specs\"\ncode-anchors:\n  any:\n    - glob: \"" + specGuideGlob + "\"\n---\n# Spec guide\n\nState the behavior first.\n"
	require.NoError(t, os.WriteFile(specPath, []byte(specContent), 0o644))
	require.NoError(t, os.WriteFile(guidePath, []byte(guideContent), 0o644))

	store, err := sqlitefixture.Open(filepath.Join(rhizomeDir, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	svc = codeanchor.NewServiceWithOptions(store, nil, codeanchor.WithBasePath(root), codeanchor.WithoutWarmCache())
	ctx := context.Background()
	for path, content := range map[string]string{specPath: specContent, guidePath: guideContent} {
		_, err = svc.IngestNoteSource(ctx, fileContextTestNoteSource(t, root, path, content))
		require.NoError(t, err)
	}
	require.NoError(t, svc.RecomputeAnchorScopes(ctx))
	return root, specPath, guidePath, svc
}

func noteFileContextText(t *testing.T, root, target string, svc *codeanchor.Service, exclude []string) string {
	t.Helper()
	vault := stubContextVault{def: obsidian.VaultDefinition{Name: "test", Root: root, Includes: []string{"**/*.md"}}}
	noteMgr := obsidian.Note{}
	text, err := BuildFileContextText(&vault, &noteMgr, FileContextTextParams{
		NoteMetadata:     testkit.NoteMetadataIndexer(t),
		Files:            []string{target},
		BudgetChars:      8000,
		Profile:          ContextProfileVault,
		CodeAnchor:       svc,
		ExcludeNotePaths: exclude,
	})
	require.NoError(t, err)
	return text
}

func TestBuildFileContextText_NoteIncludesGlobAnchoredNotesWithAttribution(t *testing.T) {
	root, specPath, _, svc := setupGlobAnchoredNotes(t, "")

	text := noteFileContextText(t, root, specPath, svc, nil)

	require.Contains(t, text, "#### Anchored notes")
	require.Contains(t, text, `path="docs/specs/GUIDE.md"`)
	require.Contains(t, text, `source="anchor:`+specGuideGlob+`"`)
	require.Contains(t, text, "State the behavior first.")
}

func TestBuildFileContextText_NoteIsNotAnchoredToItself(t *testing.T) {
	root, _, guidePath, svc := setupGlobAnchoredNotes(t, "")

	text := noteFileContextText(t, root, guidePath, svc, nil)

	require.Contains(t, text, "- path: docs/specs/GUIDE.md")
	require.NotContains(t, text, "#### Anchored notes")
	require.NotContains(t, text, `source="anchor:`)
}

func TestBuildFileContextText_NoteAnchoredNotesHonorExclusions(t *testing.T) {
	root, specPath, _, svc := setupGlobAnchoredNotes(t, "")

	text := noteFileContextText(t, root, specPath, svc, []string{"docs/specs/GUIDE.md"})

	require.Contains(t, text, "- path: docs/specs/feature.md")
	require.NotContains(t, text, "#### Anchored notes")
	require.NotContains(t, text, "docs/specs/GUIDE.md")
}

func TestBuildFileContextText_LinkedNoteKeepsItsAnchoredBody(t *testing.T) {
	root, specPath, _, svc := setupGlobAnchoredNotes(t, "\nFollow [[GUIDE]].\n")

	text := noteFileContextText(t, root, specPath, svc, nil)

	require.Contains(t, text, "### Top outbound\n\n- docs/specs/GUIDE.md")
	require.Contains(t, text, `source="anchor:`+specGuideGlob+`"`)
	require.Contains(t, text, "State the behavior first.")
}
