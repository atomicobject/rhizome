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

func TestGo_FileContext(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	goCoderefPath := ws.CodePath("go/app/main.go")
	workerPath := ws.CodePath("go/worker/worker.go")
	todoPath := ws.CodePath("go/todo/todo.go")

	codeRefsByFile := ws.BuildCodeRefs(t, []string{
		goCoderefPath,
		workerPath,
		todoPath,
	})

	goRel := fixture.RelPath(t, ws.CodeRoot, goCoderefPath)
	require.NotEmpty(t, codeRefsByFile[goRel], "expected coderefs for go/app/main.go to be indexed")

	t.Run("coderefs_are_scanned_from_c_style_comments", func(t *testing.T) {
		refs := codeRefsByFile[goRel]
		require.True(t, anyRef(refs, "notes/task-flow.md"), "expected task-flow referenced from Go comments")
		require.True(t, anyRef(refs, "notes/sync-strategy.md"), "expected sync-strategy referenced from Go comments")
		require.True(t, anyRef(refs, "notes/product-brief.md"), "expected product-brief referenced from Go block comment")
		require.True(t, anyRef(refs, "notes/release-plan.md"), "expected release-plan referenced via @mention")
	})

	t.Run("function_anchor_matches_worker_call", func(t *testing.T) {
		fc, err := actions.BuildFileContext(workerPath, actions.FileContextParams{
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
		require.NotEmpty(t, fc.AnchorMatches, "expected go function anchor on worker call")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "go-push-updates-callers"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "go-push-updates-callers", "call site matched scope"))
	})

	t.Run("symbol_anchor_matches_definition", func(t *testing.T) {
		fc, err := actions.BuildFileContext(todoPath, actions.FileContextParams{
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
		require.NotEmpty(t, fc.AnchorMatches, "expected go symbol anchor on definition file")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "go-push-updates"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "go-push-updates", "symbol matched scope"))
	})
}

func anyRef(refs []coderefs.CodeRef, target string) bool {
	for _, r := range refs {
		if strings.Contains(r.Target, target) {
			return true
		}
	}
	return false
}
