package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func publishValidationFixture(t *testing.T, store *Store, edit func(*ValidationSnapshot)) int64 {
	t.Helper()
	ctx := context.Background()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := validationStoreFixture(generation, 3)
	edit(&snapshot)
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)
	return generation
}

func pageIssueKeys(page ValidationDiagnosticPage) []string {
	keys := make([]string, 0, len(page.Diagnostics))
	for _, diagnostic := range page.Diagnostics {
		keys = append(keys, diagnostic.IssueKey)
	}
	return keys
}

func TestValidationRepairAvailabilitySeparatesApplicableRepairsFromAgentGuidance(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "repair-availability.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation := publishValidationFixture(t, store, func(snapshot *ValidationSnapshot) {
		snapshot.Actions = []ValidationActionSnapshot{
			{ID: "confirm", Check: "ontology", Kind: "retarget", Safety: "needs_confirmation", Title: "Retarget", IssueKeys: []string{"issue-0000"}},
			{ID: "guide-0", Check: "ontology", Kind: "review", Safety: "agent_required", Title: "Review", IssueKeys: []string{"issue-0000"}},
			{ID: "guide-1", Check: "ontology", Kind: "review", Safety: "agent_required", Title: "Review", IssueKeys: []string{"issue-0001"}},
		}
		snapshot.RepairActionCount = 3
	})

	for availability, want := range map[string][]string{
		ValidationRepairAvailabilityPresent:      {"issue-0000", "issue-0001"},
		ValidationRepairAvailabilityAbsent:       {"issue-0002"},
		ValidationRepairAvailabilityApplicable:   {"issue-0000"},
		ValidationRepairAvailabilityInapplicable: {"issue-0001", "issue-0002"},
	} {
		page, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
			Generation: generation, Filter: ValidationDiagnosticFilter{RepairAvailability: availability},
		})
		require.NoError(t, err, availability)
		require.Equal(t, want, pageIssueKeys(page), availability)
		summaries, err := store.GetValidationScopeSummaries(ctx, ValidationScopeSummaryRequest{
			Generation: generation, Scopes: []ValidationScope{{Kind: ValidationScopeGlobal}},
			Filter: ValidationDiagnosticFilter{RepairAvailability: availability},
		})
		require.NoError(t, err, availability)
		require.Equal(t, len(want), summaries.Summaries[0].IssueCount, availability)
	}
}

func TestValidationDiagnosticFileSortGroupsFilesWithTotalsAcrossPages(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "file-sort.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation := publishValidationFixture(t, store, func(snapshot *ValidationSnapshot) {
		diagnostics := make([]ValidationDiagnostic, 0, 5)
		for _, item := range []struct{ key, primary string }{
			{"z-1", "notes/z.md"}, {"a-1", "notes/a.md"}, {"z-2", "notes/z.md"}, {"b-1", ""}, {"vault", ""},
		} {
			diagnostic := ValidationDiagnostic{IssueKey: item.key, Check: "ontology", Code: "missing_type", PrimaryPath: item.primary}
			if item.primary != "" {
				diagnostic.AffectedPaths = []string{item.primary}
			}
			diagnostics = append(diagnostics, diagnostic)
		}
		// A diagnostic without a primary path groups under its first affected path.
		diagnostics[3].AffectedPaths = []string{"notes/c.md", "notes/b.md"}
		snapshot.Diagnostics = diagnostics
		snapshot.IssueCount = len(diagnostics)
		snapshot.AffectedFileCount = 4
	})

	var keys []string
	totals := map[string]int{}
	cursor := ""
	for {
		page, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
			Generation: generation, Limit: 2, Cursor: cursor, Sort: ValidationDiagnosticSortFile,
		})
		require.NoError(t, err)
		require.Equal(t, ValidationDiagnosticSortFile, page.Sort)
		keys = append(keys, pageIssueKeys(page)...)
		for file, total := range page.FileTotals {
			totals[file] = total
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	require.Equal(t, []string{"vault", "a-1", "b-1", "z-1", "z-2"}, keys)
	require.Equal(t, map[string]int{"": 1, "notes/a.md": 1, "notes/b.md": 1, "notes/z.md": 2}, totals)

	first, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 1})
	require.NoError(t, err)
	require.Empty(t, first.FileTotals)
	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Limit: 1, Cursor: first.NextCursor, Sort: ValidationDiagnosticSortFile,
	})
	require.ErrorIs(t, err, ErrValidationPageCursor)
}

func TestValidationTypeScopeIncludesIssuesInNotesOfThatType(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "type-scope-notes.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.db.ExecContext(ctx, `INSERT INTO ontology_note_types (note_path, type_name, schema_hash, updated_at)
		VALUES ('notes/project.md', 'Project', 'hash', 1), ('notes/person.md', 'Person', 'hash', 1)`)
	require.NoError(t, err)
	generation := publishValidationFixture(t, store, func(snapshot *ValidationSnapshot) {
		// A schema problem about Project, a broken link inside a Project note,
		// a problem that is both, and a problem in a Person note.
		snapshot.Diagnostics[0].AffectedTypes = []string{"Project"}
		snapshot.Diagnostics[1].AffectedPaths = []string{"notes/project.md", "notes/person.md"}
		snapshot.Diagnostics[1].AffectedNotePaths = []string{"notes/project.md"}
		snapshot.Diagnostics[2].AffectedPaths = []string{"notes/project.md"}
		snapshot.Diagnostics[2].AffectedNotePaths = []string{"notes/project.md"}
		snapshot.Diagnostics[2].AffectedTypes = []string{"Project"}
		snapshot.Diagnostics = append(snapshot.Diagnostics, ValidationDiagnostic{
			IssueKey: "issue-person", Check: "broken_links", Code: "broken_note_link", PrimaryPath: "notes/person.md",
			AffectedPaths: []string{"notes/person.md"}, AffectedNotePaths: []string{"notes/person.md"},
		})
		snapshot.IssueCount = len(snapshot.Diagnostics)
		snapshot.AffectedFileCount = 3
		snapshot.AffectedNoteCount = 2
	})

	summaries, err := store.GetValidationScopeSummaries(ctx, ValidationScopeSummaryRequest{
		Generation: generation,
		Scopes:     []ValidationScope{{Kind: ValidationScopeType, Key: "Project"}, {Kind: ValidationScopeType, Key: "Person"}},
	})
	require.NoError(t, err)
	require.Equal(t, []int{3, 1}, scopeIssueCounts(summaries.Summaries))

	page, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Filter: ValidationDiagnosticFilter{ScopeKind: ValidationScopeType, ScopeKey: "Project"},
	})
	require.NoError(t, err)
	require.Equal(t, 3, page.Total)
	require.Equal(t, []string{"issue-0000", "issue-0001", "issue-0002"}, pageIssueKeys(page))
}

func TestValidationInterfaceScopeIncludesIssuesInNotesOfImplementingTypes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "interface-scope-notes.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.db.ExecContext(ctx, `INSERT INTO ontology_note_types (note_path, type_name, schema_hash, updated_at)
		VALUES ('notes/bug.md', 'Bug', 'hash', 1), ('notes/task.md', 'Task', 'hash', 1), ('notes/person.md', 'Person', 'hash', 1)`)
	require.NoError(t, err)
	generation := publishValidationFixture(t, store, func(snapshot *ValidationSnapshot) {
		// A schema problem about the Work interface, a problem in a Bug note,
		// one in a Task note, and one in a Person note, which is not Work.
		snapshot.Diagnostics[0].AffectedInterfaces = []string{"Work"}
		snapshot.Diagnostics[1].AffectedPaths = []string{"notes/bug.md"}
		snapshot.Diagnostics[1].AffectedNotePaths = []string{"notes/bug.md"}
		snapshot.Diagnostics[2].AffectedPaths = []string{"notes/task.md"}
		snapshot.Diagnostics[2].AffectedNotePaths = []string{"notes/task.md"}
		snapshot.Diagnostics = append(snapshot.Diagnostics, ValidationDiagnostic{
			IssueKey: "issue-person", Check: "broken_links", Code: "broken_note_link", PrimaryPath: "notes/person.md",
			AffectedPaths: []string{"notes/person.md"}, AffectedNotePaths: []string{"notes/person.md"},
		})
		snapshot.IssueCount = len(snapshot.Diagnostics)
		snapshot.AffectedFileCount = 4
		snapshot.AffectedNoteCount = 3
	})
	implementors := map[string][]string{"Work": {"Bug", "Task"}}
	work := ValidationScope{Kind: ValidationScopeInterface, Key: "Work"}

	summaries, err := store.GetValidationScopeSummaries(ctx, ValidationScopeSummaryRequest{
		Generation: generation,
		Scopes:     []ValidationScope{work, {Kind: ValidationScopeType, Key: "Bug"}},
		Filter:     ValidationDiagnosticFilter{InterfaceImplementors: implementors},
	})
	require.NoError(t, err)
	require.Equal(t, []int{3, 1}, scopeIssueCounts(summaries.Summaries))

	page, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Filter: ValidationDiagnosticFilter{
			ScopeKind: ValidationScopeInterface, ScopeKey: "Work", InterfaceImplementors: implementors,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 3, page.Total)
	require.Equal(t, []string{"issue-0000", "issue-0001", "issue-0002"}, pageIssueKeys(page))

	groups, err := store.GetValidationIssueGroups(ctx, ValidationIssueGroupRequest{
		Generation: generation, Scope: work, Filter: ValidationDiagnosticFilter{InterfaceImplementors: implementors},
	})
	require.NoError(t, err)
	grouped := 0
	for _, group := range groups.Groups {
		grouped += group.IssueCount
	}
	require.Equal(t, 3, grouped)

	// Without implementors an interface scope still matches its explicit rows.
	page, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Filter: ValidationDiagnosticFilter{ScopeKind: ValidationScopeInterface, ScopeKey: "Work"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"issue-0000"}, pageIssueKeys(page))
}
