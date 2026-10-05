//go:build integration
// +build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestWebCoderefs_FileContext(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	htmlPath := ws.CodePath("web/index.html")
	cssPath := ws.CodePath("web/styles/site.css")
	scssPath := ws.CodePath("web/styles/theme.scss")
	astroPath := ws.CodePath("web/pages/dashboard.astro")

	codeRefsByFile := ws.BuildCodeRefs(t, []string{
		htmlPath,
		cssPath,
		scssPath,
		astroPath,
	})

	t.Run("coderefs_are_scanned_for_web_comment_families", func(t *testing.T) {
		htmlRel := fixture.RelPath(t, ws.CodeRoot, htmlPath)
		cssRel := fixture.RelPath(t, ws.CodeRoot, cssPath)
		scssRel := fixture.RelPath(t, ws.CodeRoot, scssPath)
		astroRel := fixture.RelPath(t, ws.CodeRoot, astroPath)

		require.True(t, hasRef(codeRefsByFile[htmlRel], "notes/task-flow.md"))
		require.True(t, hasRef(codeRefsByFile[htmlRel], "notes/release-plan.md"))
		require.True(t, hasRef(codeRefsByFile[cssRel], "notes/product-brief.md"))
		require.True(t, hasRef(codeRefsByFile[scssRel], "notes/task-flow.md"))
		require.True(t, hasRef(codeRefsByFile[scssRel], "notes/sync-strategy.md"))
		require.True(t, hasRef(codeRefsByFile[astroRel], "notes/release-plan.md"))
		require.False(t, hasRef(codeRefsByFile[astroRel], "notes/sync-strategy.md"), "astro v1 should ignore script comments")
	})

	t.Run("file_context_surfaces_coderef_notes_without_anchor_matches", func(t *testing.T) {
		fc, err := actions.BuildFileContext(astroPath, actions.FileContextParams{
			VaultDef:       ws.VaultDef,
			ProjectRoot:    ws.ProjectRoot,
			CodeRefRoot:    ws.CodeRoot,
			DocPatterns:    []string{"CONTEXT.md"},
			MaxEmptyLevels: 4,
			ContextBudget:  4000,
			CodeRefsByFile: codeRefsByFile,
			NoteReader:     ws.NoteMgr,
			CodeAnchor:     ws.CodeAnchor,
		})
		require.NoError(t, err)
		require.True(t, linkedNotePathExists(fc.LinkedNotes, "notes/release-plan.md"))
		require.Empty(t, fc.AnchorMatches)
	})
}

func hasRef(refs []coderefs.CodeRef, target string) bool {
	for _, r := range refs {
		if strings.Contains(r.Target, target) {
			return true
		}
	}
	return false
}

func linkedNotePathExists(notes []actions.LinkedNoteContext, target string) bool {
	for _, note := range notes {
		if strings.Contains(note.Path, target) {
			return true
		}
	}
	return false
}
