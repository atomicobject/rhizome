package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidationDiagnosticSnapshotPublishesAtomicallyAndPagesPastFiveHundred(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "diagnostics.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := validationStoreFixture(generation, 503)
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)

	summary, ok, err := store.GetPublishedValidationSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, generation, summary.Generation)
	require.Equal(t, 503, summary.IssueCount)
	require.Equal(t, 503, summary.AffectedFileCount)
	require.Equal(t, 1, summary.ErrorCount)

	first, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 200})
	require.NoError(t, err)
	require.Len(t, first.Diagnostics, 200)
	require.Equal(t, 200, first.Returned)
	require.NotEmpty(t, first.FilterIdentity)
	require.Equal(t, ValidationDiagnosticSortStable, first.Sort)
	require.NotEmpty(t, first.NextCursor)
	second, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 200, Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, second.Diagnostics, 200)
	require.NotEmpty(t, second.NextCursor)
	third, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 200, Cursor: second.NextCursor})
	require.NoError(t, err)
	require.Len(t, third.Diagnostics, 103)
	require.Empty(t, third.NextCursor)
	require.Equal(t, 503, second.Total)

	state, err := store.GetValidationState(ctx)
	require.NoError(t, err)
	require.Equal(t, generation, state.PublishedGeneration)
	require.Empty(t, state.ResultJSON)
	require.EqualValues(t, 314, state.DurationMs)

	_, err = store.SetValidationRunning(ctx)
	require.NoError(t, err)
	state, err = store.GetValidationState(ctx)
	require.NoError(t, err)
	require.Equal(t, ValidationStatusRunning, state.Status)
	require.Equal(t, generation, state.PublishedGeneration)
	_, ok, err = store.GetPublishedValidationSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestValidationStateSnapshotCannotMixPublishedGenerations(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "diagnostic-read-transaction.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(generation, 1))
	require.NoError(t, err)
	require.True(t, published)

	var writer sync.WaitGroup
	writerErr := make(chan error, 1)
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; i < 25; i++ {
			generation, runErr := store.SetValidationRunning(ctx)
			if runErr != nil {
				writerErr <- runErr
				return
			}
			published, publishErr := store.PublishValidationSnapshot(ctx, validationStoreFixture(generation, 1))
			if publishErr != nil {
				writerErr <- publishErr
				return
			}
			if !published {
				writerErr <- fmt.Errorf("generation %d was not published", generation)
				return
			}
		}
		writerErr <- nil
	}()
	for i := 0; i < 100; i++ {
		read, readErr := store.GetValidationStateSnapshot(ctx)
		require.NoError(t, readErr)
		if read.HasSnapshot {
			require.Equal(t, read.State.PublishedGeneration, read.Snapshot.Generation)
		}
	}
	writer.Wait()
	require.NoError(t, <-writerErr)
}

func TestValidationDiagnosticPageRejectsInvalidBoundsAndCursorReuse(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "diagnostic-page-contract.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := validationStoreFixture(generation, 3)
	snapshot.Diagnostics[0].Code = "first"
	snapshot.Diagnostics[1].Code = "second"
	snapshot.Actions = []ValidationActionSnapshot{{
		ID: "repair", Check: "ontology", Kind: "repair", Safety: "safe", Title: "Repair",
		IssueKeys: []string{snapshot.Diagnostics[0].IssueKey},
	}}
	snapshot.RepairActionCount = 1
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)

	defaultPage, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation})
	require.NoError(t, err)
	require.Equal(t, 3, defaultPage.Returned)

	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: -1})
	require.ErrorIs(t, err, ErrValidationPageLimit)
	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 201})
	require.ErrorIs(t, err, ErrValidationPageLimit)
	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Cursor: "garbage"})
	require.ErrorIs(t, err, ErrValidationPageCursor)

	first, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)
	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Limit: 1, Cursor: first.NextCursor,
		Filter: ValidationDiagnosticFilter{Code: "second"},
	})
	require.ErrorIs(t, err, ErrValidationPageCursor)

	repairable, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation,
		Filter:     ValidationDiagnosticFilter{RepairAvailability: ValidationRepairAvailabilityPresent},
	})
	require.NoError(t, err)
	require.Equal(t, 1, repairable.Total)
	require.Equal(t, snapshot.Diagnostics[0].IssueKey, repairable.Diagnostics[0].IssueKey)
	textMatch, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Filter: ValidationDiagnosticFilter{Text: "FIRST"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, textMatch.Total)
	require.Equal(t, snapshot.Diagnostics[0].IssueKey, textMatch.Diagnostics[0].IssueKey)

	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Filter: ValidationDiagnosticFilter{RepairAvailability: "sometimes"},
	})
	require.True(t, errors.Is(err, ErrValidationPageFilter))
	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
		Generation: generation, Filter: ValidationDiagnosticFilter{ScopeKind: ValidationScopeNode},
	})
	require.ErrorIs(t, err, ErrValidationPageFilter)
}

func TestValidationDiagnosticPagingReturnsEveryFinalPageAtScale(t *testing.T) {
	for _, issueCount := range []int{501, 1001, 10000} {
		t.Run(fmt.Sprintf("issues_%d", issueCount), func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(currentSchemaTestDBPath(t, "diagnostic-scale.db"))
			require.NoError(t, err)
			defer func() { _ = store.Close() }()
			generation, err := store.SetValidationRunning(ctx)
			require.NoError(t, err)
			published, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(generation, issueCount))
			require.NoError(t, err)
			require.True(t, published)

			cursor := ""
			seen := 0
			for {
				page, pageErr := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
					Generation: generation, Limit: ValidationDiagnosticMaxPageSize, Cursor: cursor,
				})
				require.NoError(t, pageErr)
				require.Equal(t, issueCount, page.Total)
				seen += page.Returned
				if page.NextCursor == "" {
					require.LessOrEqual(t, page.Returned, ValidationDiagnosticMaxPageSize)
					break
				}
				cursor = page.NextCursor
			}
			require.Equal(t, issueCount, seen)
			_, filterIdentity, normalizeErr := normalizeValidationDiagnosticPageRequest(ValidationDiagnosticPageRequest{Generation: generation, Limit: 200})
			require.NoError(t, normalizeErr)
			exhaustedCursor, cursorErr := encodeValidationDiagnosticCursor(validationDiagnosticCursor{
				Version: 1, Generation: generation, FilterIdentity: filterIdentity,
				Sort: ValidationDiagnosticSortStable, After: issueCount - 1,
			})
			require.NoError(t, cursorErr)
			empty, pageErr := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{
				Generation: generation, Limit: 200, Cursor: exhaustedCursor,
			})
			require.NoError(t, pageErr)
			require.Zero(t, empty.Returned)
			require.NotNil(t, empty.Diagnostics)
			require.Empty(t, empty.NextCursor)
		})
	}
}

func TestValidationScopeSummariesDeduplicateExplicitMemberships(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "diagnostic-scopes.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	snapshot := validationStoreFixture(generation, 3)
	snapshot.Diagnostics[0].AffectedPaths = []string{"notes/untyped.md", "config/schema.yml"}
	snapshot.Diagnostics[0].AffectedNotePaths = []string{"notes/untyped.md"}
	snapshot.Diagnostics[1].AffectedPaths = []string{"notes/typed.md"}
	snapshot.Diagnostics[1].AffectedNotePaths = []string{"notes/typed.md"}
	snapshot.Diagnostics[1].AffectedNodeIDs = []string{"story-1", "story-2", "story-1"}
	snapshot.Diagnostics[1].AffectedTypes = []string{"UserStory"}
	snapshot.Diagnostics[1].AffectedInterfaces = []string{"WorkItem"}
	snapshot.Diagnostics[2].AffectedPaths = []string{"notes/typed.md"}
	snapshot.Diagnostics[2].AffectedNotePaths = []string{"notes/typed.md"}
	snapshot.Diagnostics[2].AffectedNodeIDs = []string{"story-1"}
	snapshot.Diagnostics[2].AffectedTypes = []string{"UserStory"}
	snapshot.Diagnostics[2].AffectedInterfaces = []string{"WorkItem"}
	snapshot.AffectedFileCount = 3
	snapshot.AffectedNoteCount = 2
	snapshot.Actions = []ValidationActionSnapshot{{
		ID: "shared-repair", Check: "ontology", Kind: "repair", Safety: "safe", Title: "Repair stories",
		IssueKeys: []string{snapshot.Diagnostics[1].IssueKey, snapshot.Diagnostics[2].IssueKey},
	}}
	snapshot.RepairActionCount = 1
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)

	response, err := store.GetValidationScopeSummaries(ctx, ValidationScopeSummaryRequest{
		Generation: generation,
		Scopes: []ValidationScope{
			{Kind: ValidationScopeGlobal},
			{Kind: ValidationScopeFile, Key: "notes/typed.md"},
			{Kind: ValidationScopeNote, Key: "notes/untyped.md"},
			{Kind: ValidationScopeNode, Key: "story-1"},
			{Kind: ValidationScopeType, Key: "UserStory"},
			{Kind: ValidationScopeInterface, Key: "WorkItem"},
			{Kind: ValidationScopeFile, Key: "missing.md"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, generation, response.Generation)
	require.Equal(t, []int{3, 2, 1, 2, 2, 2, 0}, scopeIssueCounts(response.Summaries))
	require.Equal(t, 3, response.Summaries[0].AffectedFileCount)
	require.Equal(t, 2, response.Summaries[0].AffectedNoteCount)
	require.Equal(t, 1, response.Summaries[0].RepairActionCount)
	require.Equal(t, 1, response.Summaries[3].AffectedFileCount)
	require.Equal(t, 1, response.Summaries[3].AffectedNoteCount)
	require.Equal(t, 1, response.Summaries[3].RepairActionCount)

	page, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []string{"story-1", "story-2"}, page.Diagnostics[1].AffectedNodeIDs)
	require.Equal(t, []string{"UserStory"}, page.Diagnostics[1].AffectedTypes)
	require.Equal(t, []string{"WorkItem"}, page.Diagnostics[1].AffectedInterfaces)
	tooMany := make([]ValidationScope, ValidationScopeBatchMax+1)
	for index := range tooMany {
		tooMany[index] = ValidationScope{Kind: ValidationScopeFile, Key: fmt.Sprintf("notes/%d.md", index)}
	}
	_, err = store.GetValidationScopeSummaries(ctx, ValidationScopeSummaryRequest{Generation: generation, Scopes: tooMany})
	require.ErrorIs(t, err, ErrValidationScopeRequest)
}

func TestValidationSnapshotCanceledPublicationLeavesPriorGenerationIntact(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "diagnostic-canceled-publish.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	first, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(first, 1))
	require.NoError(t, err)
	require.True(t, published)
	second, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.PublishValidationSnapshot(canceled, validationStoreFixture(second, 1001))
	require.ErrorIs(t, err, context.Canceled)
	read, err := store.GetValidationStateSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, read.HasSnapshot)
	require.Equal(t, first, read.Snapshot.Generation)
}

func scopeIssueCounts(summaries []ValidationScopeSummary) []int {
	counts := make([]int, len(summaries))
	for index := range summaries {
		counts[index] = summaries[index].IssueCount
	}
	return counts
}

func TestValidationSnapshotInvalidationRetainsPublishedGenerationAsStale(t *testing.T) {
	store, err := Open(currentSchemaTestDBPath(t, "validation-stale.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(generation, 1))
	require.NoError(t, err)
	require.True(t, published)

	updated, err := store.MarkPublishedValidationStale(ctx, "vault changed")
	require.NoError(t, err)
	require.True(t, updated)
	read, err := store.GetValidationStateSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, read.HasSnapshot)
	require.Equal(t, generation, read.Snapshot.Generation)
	require.Equal(t, "vault changed", read.Snapshot.StaleReason)
}

func TestValidationDiagnosticSnapshotRejectsStaleGenerationAndKeepsLastTwoPublished(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "diagnostic-generations.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var generations []int64
	for i := 0; i < 3; i++ {
		generation, runErr := store.SetValidationRunning(ctx)
		require.NoError(t, runErr)
		generations = append(generations, generation)
		published, publishErr := store.PublishValidationSnapshot(ctx, validationStoreFixture(generation, i+1))
		require.NoError(t, publishErr)
		require.True(t, published)
	}

	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generations[0], Limit: 10})
	require.ErrorIs(t, err, ErrValidationGenerationExpired)
	for _, generation := range generations[1:] {
		_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 10})
		require.NoError(t, err)
	}

	published, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(generations[0], 9))
	require.NoError(t, err)
	require.False(t, published)
	summary, ok, err := store.GetPublishedValidationSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, generations[2], summary.Generation)
}

func TestValidationDiagnosticSnapshotFailedPublicationPreservesPublishedPointerAndRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "diagnostic-atomic.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	firstGeneration, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(firstGeneration, 1))
	require.NoError(t, err)
	require.True(t, published)

	secondGeneration, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	broken := validationStoreFixture(secondGeneration, 1)
	broken.RepairActionCount = 2
	broken.Actions = []ValidationActionSnapshot{
		{ID: "duplicate", Check: "ontology", Kind: "repair", Safety: "safe", Title: "First", IssueKeys: []string{"issue-0000"}},
		{ID: "duplicate", Check: "ontology", Kind: "repair", Safety: "safe", Title: "Second", IssueKeys: []string{"issue-0000"}},
	}
	_, err = store.PublishValidationSnapshot(ctx, broken)
	require.Error(t, err)

	summary, ok, err := store.GetPublishedValidationSnapshot(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, firstGeneration, summary.Generation)
	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: secondGeneration, Limit: 10})
	require.ErrorIs(t, err, ErrValidationGenerationExpired)
}

func TestValidationDiagnosticMigrationClearsLegacyResultAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-validation.db")
	legacy, err := openWithOptionsAtSchemaVersion(path, OpenOptions{}, 65)
	require.NoError(t, err)
	_, err = legacy.db.ExecContext(ctx, `
		INSERT INTO validation_state(id, status, result_json, generation, duration_ms)
		VALUES(1, 'ok', '{"issueCount":500}', 7, 10)
		ON CONFLICT(id) DO UPDATE SET status='ok', result_json='{"issueCount":500}', generation=7
	`)
	require.NoError(t, err)
	require.NoError(t, legacy.Close())

	store, err := Open(path)
	require.NoError(t, err)
	state, err := store.GetValidationState(ctx)
	require.NoError(t, err)
	require.Equal(t, ValidationStatusNeverRan, state.Status)
	require.Zero(t, state.PublishedGeneration)
	require.Empty(t, state.ResultJSON)
	require.NoError(t, store.ensureSchemaVersion(ctx, currentSchemaVersion))
	require.NoError(t, store.Close())

	reopened, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, reopened.Close())
}

func validationStoreFixture(generation int64, issueCount int) ValidationSnapshot {
	diagnostics := make([]ValidationDiagnostic, issueCount)
	for i := range diagnostics {
		diagnostics[i] = ValidationDiagnostic{
			IssueKey: fmt.Sprintf("issue-%04d", i), Check: "ontology", Code: "missing_type",
			Message: "missing type", PrimaryPath: fmt.Sprintf("notes/%04d.md", i),
			AffectedPaths: []string{fmt.Sprintf("notes/%04d.md", i)},
		}
	}
	return ValidationSnapshot{
		VaultIdentity: "vault", Generation: generation, Scope: "default", SelectedChecks: []string{"ontology", "views"},
		Completion: ValidationCompletionComplete, StartedAt: 10, FinishedAt: 20, DurationMs: 314,
		IssueCount: issueCount, ErrorCount: 1, AffectedFileCount: issueCount,
		Checks:      []ValidationCheckSnapshot{{Check: "ontology", Outcome: ValidationCheckOutcomeCompleted, IssueCount: issueCount}, {Check: "views", Outcome: ValidationCheckOutcomeFailed, Error: "schema unavailable"}},
		Diagnostics: diagnostics,
	}
}
