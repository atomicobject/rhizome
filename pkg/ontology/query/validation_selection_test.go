package query

import (
	"context"
	"fmt"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestValidationInvalidCheckCannotBecomeCleanAfterScopeRead(t *testing.T) {
	for _, check := range []string{"unknown-check", "identifiers"} {
		t.Run(check, func(t *testing.T) {
			env := newQueryTestEnv(t)
			publishQueryValidationSnapshot(t, env.store, semdb.ValidationSnapshot{
				SelectedChecks: []string{"broken_links"},
				Checks:         []semdb.ValidationCheckSnapshot{{Check: "broken_links", Outcome: semdb.ValidationCheckOutcomeCompleted}},
			})
			prepared, errs := Prepare(env.execSchema, fmt.Sprintf(`{ validation(check: %q, scopeKind: "note", scopeKey: "clean.md") { ok issueCount } }`, check))
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Len(t, result.Errors, 1)
			require.Equal(t, false, result.Data["validation"].(map[string]any)["ok"])
		})
	}
}

func TestValidationInterfaceScopeCoversNotesOfImplementingTypes(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Work { title: String! }
type Bug implements Work @node(paths: ["notes/bugs/*.md"]) { title: String! }
type Task implements Work @node(paths: ["notes/tasks/*.md"]) { title: String! }
`, map[string]string{
		"notes/bugs/b.md":  "---\ntype: Bug\ntitle: B\n---\n",
		"notes/tasks/t.md": "---\ntype: Task\ntitle: T\n---\n",
	})
	diagnostic := func(key, path string) semdb.ValidationDiagnostic {
		return semdb.ValidationDiagnostic{
			IssueKey: key, Check: "broken_links", Code: "broken_note_link", PrimaryPath: path,
			AffectedPaths: []string{path}, AffectedNotePaths: []string{path},
		}
	}
	publishQueryValidationSnapshot(t, env.store, semdb.ValidationSnapshot{
		SelectedChecks: []string{"broken_links"}, IssueCount: 2, AffectedFileCount: 2, AffectedNoteCount: 2,
		Checks:      []semdb.ValidationCheckSnapshot{{Check: "broken_links", Outcome: semdb.ValidationCheckOutcomeCompleted, IssueCount: 2}},
		Diagnostics: []semdb.ValidationDiagnostic{diagnostic("issue-bug", "notes/bugs/b.md"), diagnostic("issue-task", "notes/tasks/t.md")},
	})
	prepared, errs := Prepare(env.execSchema, `{ validation(scopeKind: "interface", scopeKey: "Work") { issueCount checks { issueCount issues { issueKey } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, 2, validation["issueCount"])
	check := validation["checks"].([]any)[0].(map[string]any)
	require.Equal(t, 2, check["issueCount"])
	require.Len(t, check["issues"], 2)
}
