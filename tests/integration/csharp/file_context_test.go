//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestCSharp_FileContext(t *testing.T) {
	t.Setenv("RHIZOME_TS_PARSE_TIMEOUT", "15s")
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	if ws.CodeAnchor == nil || !ws.CodeAnchor.HasIndexer(codeanchor.LangCs) {
		t.Skip("C# indexer unavailable in this build")
	}
	t.Logf("HasIndexer(cs)=%v", ws.CodeAnchor.HasIndexer(codeanchor.LangCs))

	servicePath := ws.CodePath("csharp/TaskService.cs")
	syncPath := ws.CodePath("csharp/SyncClient.cs")
	workerPath := ws.CodePath("csharp/Worker.cs")
	modernWorkerPath := ws.CodePath("csharp/ModernWorker.cs")
	typedWorkerPath := ws.CodePath("csharp/TypedWorker.cs")
	staticConsumerPath := ws.CodePath("csharp/StaticConsumer.cs")

	syncSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, syncPath)
	require.NoError(t, err, "symbols for SyncClient.cs")
	require.NotEmpty(t, syncSyms, "supported C# parser produced no symbols for SyncClient.cs")
	workerSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, workerPath)
	require.NoError(t, err, "symbols for Worker.cs")
	require.NotEmpty(t, workerSyms, "supported C# parser produced no symbols for Worker.cs")
	modernWorkerSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, modernWorkerPath)
	require.NoError(t, err, "symbols for ModernWorker.cs")
	require.NotEmpty(t, modernWorkerSyms, "supported C# parser produced no symbols for ModernWorker.cs")
	typedWorkerSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, typedWorkerPath)
	require.NoError(t, err, "symbols for TypedWorker.cs")
	require.NotEmpty(t, typedWorkerSyms, "supported C# parser produced no symbols for TypedWorker.cs")
	staticConsumerSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, staticConsumerPath)
	require.NoError(t, err, "symbols for StaticConsumer.cs")
	require.NotEmpty(t, staticConsumerSyms, "supported C# parser produced no symbols for StaticConsumer.cs")

	caCtx, err := ws.CodeAnchor.NotesForFile(ctx, servicePath)
	require.NoError(t, err)
	t.Logf("codeanchor ctx (TaskService): anchors=%v trace=%v", caCtx.Anchors, caCtx.Trace)
	syms, err := ws.CodeAnchor.SymbolsForFile(ctx, servicePath)
	require.NoError(t, err)
	t.Logf("codeanchor symbols (TaskService): %v", syms)

	codeRefsByFile := ws.BuildCodeRefs(t, []string{servicePath, syncPath, workerPath, modernWorkerPath})

	serviceRel := fixture.RelPath(t, ws.CodeRoot, servicePath)
	require.NotEmpty(t, codeRefsByFile[serviceRel], "expected coderefs for TaskService.cs to be indexed")
	require.True(t, hasRef(codeRefsByFile[serviceRel], "notes/task-flow.md"))

	t.Run("symbol_anchor_matches_definition", func(t *testing.T) {
		fc, err := actions.BuildFileContext(servicePath, actions.FileContextParams{
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
		require.NotEmpty(t, fc.AnchorMatches, "expected csharp symbol anchor on definition file")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "cs-add-task"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "cs-add-task", "symbol matched scope"))
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
		require.NotEmpty(t, fc.AnchorMatches, "expected csharp function anchor on worker call")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "cs-push-updates-callers"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "cs-push-updates-callers", "call site matched scope"))
	})

	t.Run("function_anchor_matches_modern_worker_call", func(t *testing.T) {
		fc, err := actions.BuildFileContext(modernWorkerPath, actions.FileContextParams{
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
		require.NotEmpty(t, fc.AnchorMatches, "expected csharp function anchor on modern worker call")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "cs-push-updates-callers"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "cs-push-updates-callers", "call site matched scope"))
	})

	t.Run("symbol_anchor_matches_sync_definition", func(t *testing.T) {
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
		require.NotEmpty(t, fc.AnchorMatches, "expected csharp symbol anchor on sync client definition")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "cs-push-updates"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "cs-push-updates", "symbol matched scope"))
	})

	t.Run("typed_worker_fixture_is_indexed", func(t *testing.T) {
		syms, err := ws.CodeAnchor.SymbolsForFile(ctx, typedWorkerPath)
		require.NoError(t, err)
		require.Contains(t, syms, "Polyglot.Todo.Worker.SyncGateway")
		require.Contains(t, syms, "Polyglot.Todo.Worker.SyncGateway.Push")
		require.Contains(t, syms, "Polyglot.Todo.Worker.TypedWorker")
		require.Contains(t, syms, "Polyglot.Todo.Worker.TypedWorker.Run")
	})

	t.Run("static_member_and_global_alias_edges_are_indexed", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ws.DBPath)
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		require.Positive(t, countIntelEdgeToFQN(t, ctx, db, "csharp/StaticConsumer.cs", "member_ref", "Polyglot.Todo.Constants.LegalStatuses.Pcs.Active"))
		require.Positive(t, countIntelEdgeToFQN(t, ctx, db, "csharp/StaticConsumer.cs", "member_ref", "Polyglot.Todo.Constants.LegalStatuses.Pcs.TerminalStates"))
		require.Positive(t, countIntelEdgeToFQN(t, ctx, db, "csharp/StaticConsumer.cs", "type_ref", "Polyglot.Todo.Models.CourtView.CaseCommentOverflow"))
		require.Positive(t, countIntelEdgeToFQN(t, ctx, db, "csharp/StaticConsumer.cs", "member_ref", "Polyglot.Todo.Models.CourtView.CaseCommentOverflow.Text"))
	})
}

func hasRef(refs []coderefs.CodeRef, target string) bool {
	for _, r := range refs {
		if strings.Contains(filepath.ToSlash(r.Target), target) {
			return true
		}
	}
	return false
}

func countIntelEdgeToFQN(t *testing.T, ctx context.Context, db *sql.DB, srcPath, kind, dstFQN string) int {
	t.Helper()
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM intel_edges e
		JOIN intel_code_anchors src ON src.id = e.src_row_id
		JOIN intel_code_anchors dst ON dst.id = e.dst_row_id
		WHERE src.path = ? AND e.kind = ? AND dst.fqn = ?
		  AND e.src_type = 'anchor' AND e.dst_type = 'anchor'
	`, srcPath, kind, dstFQN).Scan(&count)
	require.NoError(t, err)
	return count
}
