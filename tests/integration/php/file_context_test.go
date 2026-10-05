//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestPHP_FileContext(t *testing.T) {
	t.Setenv("RHIZOME_TS_PARSE_TIMEOUT", "15s")
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	if ws.CodeAnchor == nil || !ws.CodeAnchor.HasIndexer(codeanchor.LangPhp) {
		t.Skip("PHP indexer unavailable in this build")
	}
	t.Logf("HasIndexer(php)=%v", ws.CodeAnchor.HasIndexer(codeanchor.LangPhp))

	servicePath := ws.CodePath("php/TaskService.php")
	syncPath := ws.CodePath("php/SyncClient.php")
	workerPath := ws.CodePath("php/Worker.php")
	helpersPath := ws.CodePath("php/helpers.php")
	hooksPath := ws.CodePath("php/hooks.php")
	themePath := ws.CodePath("php/theme.php")
	controllerPath := ws.CodePath("php/RealWorldController.php")

	// Skip if indexer timed out on slow CI runners.
	syncSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, syncPath)
	require.NoError(t, err, "symbols for SyncClient.php")
	require.NotEmpty(t, syncSyms, "supported PHP parser produced no symbols for SyncClient.php")
	workerSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, workerPath)
	require.NoError(t, err, "symbols for Worker.php")
	require.NotEmpty(t, workerSyms, "supported PHP parser produced no symbols for Worker.php")

	codeRefsByFile := ws.BuildCodeRefs(t, []string{servicePath, syncPath, workerPath, helpersPath, controllerPath})

	t.Run("namespaced_symbol_is_indexed", func(t *testing.T) {
		syms, err := ws.CodeAnchor.SymbolsForFile(ctx, servicePath)
		require.NoError(t, err)
		require.Contains(t, syms, "Polyglot\\Todo\\TaskService")
		require.Contains(t, syms, "Polyglot\\Todo\\TaskService::addTask")
		require.Contains(t, syms, "Polyglot\\Todo\\Task")
	})

	t.Run("worker_calls_sync_client", func(t *testing.T) {
		// Worker::run calls $client->pushUpdates(...) — that's a member_call_expression.
		// Verify worker symbols indexed correctly with namespace.
		syms, err := ws.CodeAnchor.SymbolsForFile(ctx, workerPath)
		require.NoError(t, err)
		require.Contains(t, syms, "Polyglot\\Todo\\Worker\\Worker")
		require.Contains(t, syms, "Polyglot\\Todo\\Worker\\Worker::run")
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
		require.NotEmpty(t, fc.AnchorMatches, "expected PHP function anchor on worker call")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "php-push-updates-callers"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "php-push-updates-callers", "call site matched scope"))
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
		require.NotEmpty(t, fc.AnchorMatches, "expected PHP symbol anchor on SyncClient definition")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "php-push-updates"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "php-push-updates", "symbol matched scope"))
	})

	t.Run("symbol_anchor_matches_task_service_definition", func(t *testing.T) {
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
		require.NotEmpty(t, fc.AnchorMatches, "expected PHP symbol anchor on TaskService definition")
		require.True(t, fixture.AnchorMatch(fc.AnchorMatches, "php-add-task"))
	})

	t.Run("procedural_functions_are_indexed", func(t *testing.T) {
		syms, err := ws.CodeAnchor.SymbolsForFile(ctx, helpersPath)
		require.NoError(t, err)
		// Global-scope functions have bare FQNs.
		require.Contains(t, syms, "format_payload")
		require.Contains(t, syms, "notify")
	})

	t.Run("included_file_symbols_and_durable_ref", func(t *testing.T) {
		// PHP includes persist an import reference; resolved file-to-file
		// import edges are outside the current PHP graph contract.
		sharedSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, ws.CodePath("php/shared.php"))
		require.NoError(t, err)
		require.Contains(t, sharedSyms, "Logger")
		store, err := codeanchorsqlite.Open(ws.DBPath)
		require.NoError(t, err)
		defer store.Close()
		refs, err := store.ImportRefsByPaths(ctx, []string{"php/helpers.php"})
		require.NoError(t, err)
		require.Contains(t, refs["php/helpers.php"], codeanchor.ImportRefRow{SrcPath: "php/helpers.php", Module: "shared.php"})
	})

	t.Run("real_world_controller_surfaces_rich_edges", func(t *testing.T) {
		syms, err := ws.CodeAnchor.SymbolsForFile(ctx, controllerPath)
		require.NoError(t, err)
		require.Contains(t, syms, "Polyglot\\Todo\\Http\\TaskController")
		require.Contains(t, syms, "Polyglot\\Todo\\Http\\TaskController::store")
		require.Contains(t, syms, "Polyglot\\Todo\\Http\\TaskStatus")
		require.Contains(t, syms, "Polyglot\\Todo\\Http\\TaskStatus::Pending")

		fc, err := actions.BuildFileContext(controllerPath, actions.FileContextParams{
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
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "php-task-controller", "symbol matched scope"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "php-task-controller-subclasses", "symbol matched scope"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "php-route-attribute", "annotation matched scope"))
		require.True(t, fixture.AnchorMatchReason(fc.AnchorMatches, "php-push-updates-callers", "call site matched scope"))
		require.True(t, linkedPHPNotePathExists(fc.LinkedNotes, "notes/task-flow.md"))
		require.True(t, linkedPHPNotePathExists(fc.LinkedNotes, "notes/release-plan.md"))

		controllerRel := fixture.RelPath(t, ws.CodeRoot, controllerPath)
		require.True(t, hasPHPRef(codeRefsByFile[controllerRel], "notes/task-flow.md"))
		require.True(t, hasPHPRef(codeRefsByFile[controllerRel], "notes/release-plan.md"))
		require.False(t, hasPHPRef(codeRefsByFile[controllerRel], "support@example.com"))
	})

	t.Run("string_arg_callback_emits_call_edge", func(t *testing.T) {
		// hooks.php has add_action('init', 'theme_handler'); the inner string-literal arg
		// names a function defined in theme.php. The indexer should emit a CallSite that
		// resolves against the theme_handler symbol so the handler is no longer a graph
		// orphan. (The outer add_action call is unresolved because WordPress isn't part
		// of the fixture corpus; CallsFromFile only surfaces resolved edges.)
		themeSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, themePath)
		require.NoError(t, err)
		require.Contains(t, themeSyms, "theme_handler", "theme_handler must be indexed")

		fc, err := ws.CodeAnchor.NotesForFile(ctx, hooksPath)
		require.NoError(t, err)

		var foundHandler bool
		for _, c := range fc.Calls {
			if c.CalleeSymbol.Name == "theme_handler" && c.OwnerFQN == "Polyglot\\Todo\\bootstrap_theme" {
				foundHandler = true
			}
		}
		require.True(t, foundHandler, "expected string-arg callee theme_handler resolved from hooks.php: %+v", fc.Calls)
	})

	t.Run("file_scope_hook_registration_persists_symbol_refs", func(t *testing.T) {
		// default-filters-style.php has a file-scope `add_action('init', 'polyglot_theme_setup')`
		// — the canonical WordPress default-filters.php shape. The v1.6.0 walker missed these
		// because phpCollectCalls only fired inside function/method bodies. EFF-0037's two-pass
		// walker adds a stopAtBoundaries=true pass after the structured walk that picks up
		// file-scope calls + their string-arg callees with the namespace as owner FQN.
		//
		// File-scope refs persist to intel_symbol_refs with owner_symbol_id = NULL (no enclosing
		// function/method/class symbol). The higher-level CallsFromFile API joins through the
		// anchor table and requires both ends to have anchors, so it doesn't surface file-scope
		// calls today — exposing them is a follow-on architectural concern (file-scope-aware
		// caller API). For this effort the success grain is "refs persist", measured against the
		// raw intel_symbol_refs table the same way the wp-tst US4 acceptance criterion does.
		fixturePath := ws.CodePath("php/default-filters-style.php")
		fcSyms, err := ws.CodeAnchor.SymbolsForFile(ctx, fixturePath)
		require.NoError(t, err)
		require.Contains(t, fcSyms, "Polyglot\\Todo\\polyglot_theme_setup", "polyglot_theme_setup must be indexed")

		db, err := sql.Open("sqlite3", ws.DBPath)
		require.NoError(t, err)
		defer db.Close()

		var refCount int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM intel_symbol_refs r
			JOIN intel_symbol_ref_files f ON f.file_id = r.src_file_id
			WHERE f.path = ?
		`, "php/default-filters-style.php").Scan(&refCount)
		require.NoError(t, err)
		require.Greater(t, refCount, 0, "expected file-scope refs to persist for default-filters-style.php (got 0); v1.6.0 walker silently dropped these")

		// Confirm the inner callee (polyglot_theme_setup) shows up as a target. This is the
		// string-arg callback resolution applied at file scope.
		var calleeRefCount int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM intel_symbol_refs r
			JOIN intel_symbol_ref_files f ON f.file_id = r.src_file_id
			JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
			WHERE f.path = ? AND t.dst_name = ?
		`, "php/default-filters-style.php", "polyglot_theme_setup").Scan(&calleeRefCount)
		require.NoError(t, err)
		require.Greater(t, calleeRefCount, 0, "expected string-arg callee polyglot_theme_setup as a target ref from file-scope add_action")
	})

	t.Run("magic_methods_indexed_as_methods", func(t *testing.T) {
		// Container.php has __construct, __invoke, __toString. The method_declaration
		// walker applies no name filter, so PHP magic methods land as SymMethod with
		// FQN class::__name. Catalog 2026-06-12 listed magic methods as a potential
		// real-gap; this assertion pins v1 behavior as already-supported.
		containerPath := ws.CodePath("php/Container.php")
		syms, err := ws.CodeAnchor.SymbolsForFile(ctx, containerPath)
		require.NoError(t, err)
		require.Contains(t, syms, "Polyglot\\Todo\\Container::__construct")
		require.Contains(t, syms, "Polyglot\\Todo\\Container::__invoke")
		require.Contains(t, syms, "Polyglot\\Todo\\Container::__toString")
	})

	t.Run("phpdoc_wikilink_creates_coderef", func(t *testing.T) {
		// SyncClient.php has [[notes/sync-strategy]] inside the PHPDoc.
		// Confirm coderef extraction found it.
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
		require.True(t, linkedPHPNotePathExists(fc.LinkedNotes, "notes/sync-strategy.md"))
	})
}

func hasPHPRef(refs []coderefs.CodeRef, target string) bool {
	for _, r := range refs {
		if strings.Contains(r.Target, target) || strings.Contains(r.RawTarget, target) {
			return true
		}
	}
	return false
}

func linkedPHPNotePathExists(notes []actions.LinkedNoteContext, target string) bool {
	for _, note := range notes {
		if strings.Contains(note.Path, target) {
			return true
		}
	}
	return false
}
