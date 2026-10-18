package query

import (
	"context"
	"fmt"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_ValidationCanonicalizesAliasesAndReportsUnavailableSelections(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links", "identifiers"}, IssueCount: 3,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{
			{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 2},
			{Check: "identifiers", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1},
		},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{
			{IssueKey: "broken:1", Check: "broken_links", Code: "broken"},
			{IssueKey: "broken:2", Check: "broken_links", Code: "broken"},
			{IssueKey: "duplicate:1", Check: "identifiers", Code: "duplicate"},
		},
	})

	prepared, errs := Prepare(env.execSchema, `{ validation(check: "aliases") { selectedChecks checks { name issueCount } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, []any{"identifiers"}, validation["selectedChecks"])
	checks := validation["checks"].([]any)
	require.Len(t, checks, 1)
	require.Equal(t, "identifiers", checks[0].(map[string]any)["name"])

	prepared, errs = Prepare(env.execSchema, `{ validation(check: "unknown") { selectedChecks checks { name } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "unknown_validation_check", result.Errors[0].Extensions["code"])
	require.Contains(t, result.Errors[0].Message, "unknown")

	prepared, errs = Prepare(env.execSchema, `{ validation(check: "views") { selectedChecks checks { name } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_check_unavailable", result.Errors[0].Extensions["code"])
	require.Contains(t, result.Errors[0].Message, "unavailable")
}

func TestExecute_ValidationIssuesHonorsNestedFirstArgument(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links", "identifiers"}, IssueCount: 3,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{
			{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 2},
			{Check: "identifiers", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1},
		},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{
			{IssueKey: "one", Check: "broken_links", Code: "one"},
			{IssueKey: "two", Check: "broken_links", Code: "two"},
			{IssueKey: "three", Check: "identifiers", Code: "three"},
		},
	})

	prepared, errs := Prepare(env.execSchema, `{ validation(firstIssues: 1) { checks { name issues(first: 2) { code } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	checks := result.Data["validation"].(map[string]any)["checks"].([]any)
	require.Len(t, checks, 2)
	firstIssues := checks[0].(map[string]any)["issues"].([]any)
	require.Len(t, firstIssues, 2)
	require.Equal(t, "one", firstIssues[0].(map[string]any)["code"])
	require.Equal(t, "two", firstIssues[1].(map[string]any)["code"])

	prepared, errs = Prepare(env.execSchema, `{ validation(firstIssues: 201) { checks { issues { code } } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_page_limit_invalid", result.Errors[0].Extensions["code"])

	prepared, errs = Prepare(env.execSchema, `{ validation { checks { issues(first: 201) { code } } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_page_limit_invalid", result.Errors[0].Extensions["code"])
}

func TestExecute_ValidationRetainsPublishedSnapshotDuringFailedRefresh(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links"}, IssueCount: 1,
		Checks:      []codeanchorsqlite.ValidationCheckSnapshot{{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{{IssueKey: "broken:1", Check: "broken_links", Code: "broken"}},
	})
	published, ok, err := env.store.GetPublishedValidationSnapshot(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.NotZero(t, published.Generation)
	refreshGeneration, err := env.store.SetValidationRunning(context.Background())
	require.NoError(t, err)
	updated, err := env.store.SetValidationError(context.Background(), refreshGeneration, "refresh failed", 8)
	require.NoError(t, err)
	require.True(t, updated)

	prepared, errs := Prepare(env.execSchema, `{ validation { status generation publishedGeneration completion error issueCount checks { name outcome issues { issueKey } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, codeanchorsqlite.ValidationStatusError, validation["status"])
	require.Equal(t, int(refreshGeneration), validation["generation"])
	require.Equal(t, int(published.Generation), validation["publishedGeneration"])
	require.Equal(t, codeanchorsqlite.ValidationCompletionComplete, validation["completion"])
	require.Equal(t, "refresh failed", validation["error"])
	require.Equal(t, 1, validation["issueCount"])
	require.Equal(t, "completed", validation["checks"].([]any)[0].(map[string]any)["outcome"])
}

func TestExecute_ValidationScopeSummaryAndDetailMatchRESTContract(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links"}, IssueCount: 2, AffectedFileCount: 2, AffectedNoteCount: 2, RepairActionCount: 1,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 2}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{
			{IssueKey: "broken:a", Check: "broken_links", Code: "broken", PrimaryPath: "a.md", AffectedPaths: []string{"a.md"}, AffectedNotePaths: []string{"a.md"}, AffectedNodeIDs: []string{"node-a"}, AffectedTypes: []string{"Story"}, AffectedInterfaces: []string{"WorkItem"}, ActionIDs: []string{"fix-a"}},
			{IssueKey: "broken:b", Check: "broken_links", Code: "broken", PrimaryPath: "b.md", AffectedPaths: []string{"b.md"}, AffectedNotePaths: []string{"b.md"}, AffectedNodeIDs: []string{"node-b"}},
		},
		Actions: []codeanchorsqlite.ValidationActionSnapshot{{ID: "fix-a", Check: "broken_links", Kind: "repair", Safety: "safe", Title: "Fix A", IssueKeys: []string{"broken:a"}, AffectedPaths: []string{"a.md"}}},
	})

	prepared, errs := Prepare(env.execSchema, `{ validation(check: "broken-links", scopeKind: "node", scopeKey: "node-a") { scope { kind key } issueCount affectedFileCount affectedNoteCount repairActionCount checks { name issueCount issues { issueKey affectedNodeIds } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, map[string]any{"kind": "node", "key": "node-a"}, validation["scope"])
	require.Equal(t, 1, validation["issueCount"])
	require.Equal(t, 1, validation["affectedFileCount"])
	require.Equal(t, 1, validation["affectedNoteCount"])
	require.Equal(t, 1, validation["repairActionCount"])
	check := validation["checks"].([]any)[0].(map[string]any)
	require.Equal(t, "broken_links", check["name"])
	require.Equal(t, 1, check["issueCount"])
	require.Equal(t, "broken:a", check["issues"].([]any)[0].(map[string]any)["issueKey"])

	for _, scope := range []struct {
		kind  string
		key   string
		count int
	}{
		{kind: "global", count: 2},
		{kind: "file", key: "a.md", count: 1},
		{kind: "note", key: "a.md", count: 1},
		{kind: "node", key: "node-a", count: 1},
		{kind: "type", key: "Story", count: 1},
		{kind: "interface", key: "WorkItem", count: 1},
	} {
		t.Run(scope.kind, func(t *testing.T) {
			arguments := fmt.Sprintf(`scopeKind: %q`, scope.kind)
			if scope.key != "" {
				arguments += fmt.Sprintf(`, scopeKey: %q`, scope.key)
			}
			prepared, errs := Prepare(env.execSchema, fmt.Sprintf(`{ validation(%s) { scope { kind key } ok issueCount checks { issueCount issues { issueKey } } } }`, arguments))
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			validation := result.Data["validation"].(map[string]any)
			require.Equal(t, scope.count, validation["issueCount"])
			require.Equal(t, scope.count == 0, validation["ok"])
			require.Equal(t, scope.count, validation["checks"].([]any)[0].(map[string]any)["issueCount"])
			require.Len(t, validation["checks"].([]any)[0].(map[string]any)["issues"], scope.count)
		})
	}

	prepared, errs = Prepare(env.execSchema, `{ validation(scopeKind: "note", scopeKey: "clean.md") { ok issueCount } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	cleanValidation := result.Data["validation"].(map[string]any)
	require.Equal(t, 0, cleanValidation["issueCount"])
	require.Equal(t, true, cleanValidation["ok"])

	prepared, errs = Prepare(env.execSchema, `{ validation(scopeKind: "node") { issueCount } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_scope_invalid", result.Errors[0].Extensions["code"])
}

func publishQueryValidationSnapshot(t *testing.T, store *codeanchorsqlite.Store, snapshot codeanchorsqlite.ValidationSnapshot) {
	t.Helper()
	generation, err := store.SetValidationRunning(context.Background())
	require.NoError(t, err)
	snapshot.Generation = generation
	snapshot.VaultIdentity = "query-test"
	snapshot.Scope = "vault"
	snapshot.Completion = codeanchorsqlite.ValidationCompletionComplete
	snapshot.Checks = append([]codeanchorsqlite.ValidationCheckSnapshot(nil), snapshot.Checks...)
	published, err := store.PublishValidationSnapshot(context.Background(), snapshot)
	require.NoError(t, err)
	require.True(t, published)
}
