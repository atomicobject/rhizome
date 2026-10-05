//go:build integration
// +build integration

package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestPython_FileContext(t *testing.T) {
	t.Setenv("RHIZOME_TS_PARSE_TIMEOUT", "15s")
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	if ws.CodeAnchor == nil || !ws.CodeAnchor.HasIndexer(codeanchor.LangPy) {
		t.Skip("Python indexer unavailable in this build")
	}
	tasksPath := ws.CodePath("src/todoapp/services/tasks.py")
	syncPath := ws.CodePath("src/todoapp/services/sync.py")
	workerPath := ws.CodePath("scripts/worker.py")

	syncSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, syncPath)
	require.NoError(t, err, "symbols for sync.py")
	require.NotEmpty(t, syncSyms, "supported Python parser produced no symbols for sync.py")
	workerSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, workerPath)
	require.NoError(t, err, "symbols for worker.py")
	require.NotEmpty(t, workerSyms, "supported Python parser produced no symbols for worker.py")

	codeRefsByFile := ws.BuildCodeRefs(t, []string{tasksPath, syncPath, workerPath})

	tasksRel := fixture.RelPath(t, ws.CodeRoot, tasksPath)
	require.NotEmpty(t, codeRefsByFile[tasksRel], "expected coderefs for tasks.py to be indexed")

	tasksSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, tasksPath)
	require.NoError(t, err, "symbols for tasks.py")
	require.NotEmpty(t, tasksSyms, "supported Python parser produced no symbols for tasks.py")

	t.Run("includes_docs_coderefs_and_anchors", func(t *testing.T) {
		fc, err := actions.BuildFileContext(tasksPath, actions.FileContextParams{
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
		require.Equal(t, "code", fc.FileType)
		require.NotEmpty(t, fc.AncestorDocs, "expected nearby CONTEXT.md files")
		require.Contains(t, fc.AncestorDocs[0].Content, "todoapp")

		require.True(t, anyLinked(fc.LinkedNotes, func(n actions.LinkedNoteContext) bool {
			return strings.Contains(filepath.ToSlash(n.Path), "notes/task-flow.md")
		}), "expected linked note from coderefs")

		require.NotEmpty(t, fc.AnchorMatches, "expected codeanchor matches for add_task and push_updates")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "add_task"), "anchor label add_task should be present")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "push_updates"), "function anchor should match push_updates references")
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
		require.NotEmpty(t, fc.AnchorMatches, "expected function anchor on worker call")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "push_updates"), "worker call should trigger push_updates anchor")
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "push_updates", "call site matched scope"), "trace should record call-site reason")
	})

	t.Run("annotation_anchor_matches_sync_file", func(t *testing.T) {
		fc, err := actions.BuildFileContext(syncPath, actions.FileContextParams{
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
		require.NotEmpty(t, fc.AnchorMatches, "expected annotation anchor for sync instrumentation")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "instrument"), "sync file should include annotation anchor")
	})
}

func anyLinked(notes []actions.LinkedNoteContext, fn func(actions.LinkedNoteContext) bool) bool {
	for _, n := range notes {
		if fn(n) {
			return true
		}
	}
	return false
}
