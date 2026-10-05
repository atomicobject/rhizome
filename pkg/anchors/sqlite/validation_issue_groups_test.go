package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// variantStoreFixture publishes diagnostics whose codes are either all
// variant-bearing or all variant-free, so a summary filtered by check, code,
// and variant selects exactly one group.
func variantStoreFixture(t *testing.T, store *Store) int64 {
	t.Helper()
	ctx := context.Background()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	ambiguousAB := &ValidationIssueVariant{Key: "A+B", Label: "A, B"}
	ambiguousCD := &ValidationIssueVariant{Key: "C+D", Label: "C, D"}
	meetingOwner := &ValidationIssueVariant{Key: "Meeting.owner", Label: "Meeting · owner"}
	specSummary := &ValidationIssueVariant{Key: "Spec.summary", Label: "Spec · summary"}
	diagnostic := func(key, check, code string, variant *ValidationIssueVariant, paths ...string) ValidationDiagnostic {
		return ValidationDiagnostic{
			IssueKey: key, Check: check, Code: code, Message: code, Variant: variant,
			PrimaryPath: paths[0], AffectedPaths: paths, AffectedNotePaths: paths,
		}
	}
	diagnostics := []ValidationDiagnostic{
		diagnostic("i0", "ontology", "type_ambiguous", ambiguousAB, "a.md"),
		diagnostic("i1", "ontology", "type_ambiguous", ambiguousAB, "b.md"),
		diagnostic("i2", "ontology", "type_ambiguous", ambiguousCD, "c.md"),
		diagnostic("i3", "ontology", "missing_required_field", meetingOwner, "a.md"),
		diagnostic("i4", "views", "unexpected_field", nil, "views/x.yaml"),
		diagnostic("i5", "ontology", "type_ambiguous", ambiguousCD, "d.md"),
		diagnostic("i6", "ontology", "missing_required_field", meetingOwner, "e.md"),
		diagnostic("i7", "ontology", "missing_required_field", specSummary, "a.md", "f.md"),
		diagnostic("i8", "views", "unexpected_field", nil, "views/y.yaml"),
		diagnostic("i9", "ontology", "type_ambiguous", ambiguousCD, "c.md"),
	}
	snapshot := ValidationSnapshot{
		VaultIdentity: "vault", Generation: generation, Scope: "default", SelectedChecks: []string{"ontology", "views"},
		Completion: ValidationCompletionComplete, IssueCount: len(diagnostics),
		AffectedFileCount: 8, AffectedNoteCount: 8, RepairActionCount: 3,
		Checks: []ValidationCheckSnapshot{
			{Check: "ontology", Outcome: ValidationCheckOutcomeCompleted, IssueCount: 8},
			{Check: "views", Outcome: ValidationCheckOutcomeCompleted, IssueCount: 2},
		},
		Diagnostics: diagnostics,
		Actions: []ValidationActionSnapshot{
			{ID: "guide-types", Check: "ontology", IssueCode: "type_ambiguous", Kind: "agent", Safety: "agent_required", Title: "Choose types", IssueKeys: []string{"i0", "i1", "i2", "i5", "i9"}},
			{ID: "set-owner", Check: "ontology", IssueCode: "missing_required_field", Kind: "set", Safety: "safe", Title: "Set owner", IssueKeys: []string{"i3"}},
			{ID: "ask-owner", Check: "ontology", IssueCode: "missing_required_field", Kind: "set", Safety: "needs_confirmation", Title: "Ask owner", IssueKeys: []string{"i3", "i6"}},
		},
	}
	published, err := store.PublishValidationSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, published)
	return generation
}

func TestGetValidationIssueGroupsOrdersCodesAndVariants(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "groups-order.db"))
	require.NoError(t, err)
	defer store.Close()
	generation := variantStoreFixture(t, store)

	response, err := store.GetValidationIssueGroups(ctx, ValidationIssueGroupRequest{Generation: generation})
	require.NoError(t, err)
	require.Equal(t, generation, response.Generation)
	require.Equal(t, []ValidationIssueGroup{
		{Check: "ontology", Code: "type_ambiguous", Variant: &ValidationIssueVariant{Key: "C+D", Label: "C, D"}, IssueCount: 3, AffectedFileCount: 2},
		{Check: "ontology", Code: "type_ambiguous", Variant: &ValidationIssueVariant{Key: "A+B", Label: "A, B"}, IssueCount: 2, AffectedFileCount: 2},
		{Check: "ontology", Code: "missing_required_field", Variant: &ValidationIssueVariant{Key: "Meeting.owner", Label: "Meeting · owner"}, IssueCount: 2, AffectedFileCount: 2, ApplicableRepairCount: 2},
		{Check: "ontology", Code: "missing_required_field", Variant: &ValidationIssueVariant{Key: "Spec.summary", Label: "Spec · summary"}, IssueCount: 1, AffectedFileCount: 2},
		{Check: "views", Code: "unexpected_field", IssueCount: 2, AffectedFileCount: 2},
	}, response.Groups)

	_, err = store.GetValidationIssueGroups(ctx, ValidationIssueGroupRequest{Generation: generation + 100})
	require.ErrorIs(t, err, ErrValidationGenerationExpired)
	_, err = store.GetValidationIssueGroups(ctx, ValidationIssueGroupRequest{Generation: generation, Filter: ValidationDiagnosticFilter{Variant: "A+B"}})
	require.ErrorIs(t, err, ErrValidationScopeRequest)
}

func TestGetValidationIssueGroupsMatchSummariesForTheSameFilter(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "groups-summaries.db"))
	require.NoError(t, err)
	defer store.Close()
	generation := variantStoreFixture(t, store)

	for _, test := range []struct {
		name   string
		scope  ValidationScope
		filter ValidationDiagnosticFilter
	}{
		{name: "global", scope: ValidationScope{Kind: ValidationScopeGlobal}},
		{name: "file", scope: ValidationScope{Kind: ValidationScopeFile, Key: "a.md"}},
		{name: "note", scope: ValidationScope{Kind: ValidationScopeNote, Key: "c.md"}},
		{name: "check filter", scope: ValidationScope{Kind: ValidationScopeGlobal}, filter: ValidationDiagnosticFilter{Check: "ontology"}},
		{name: "text filter", scope: ValidationScope{Kind: ValidationScopeGlobal}, filter: ValidationDiagnosticFilter{Text: "a.md"}},
		{name: "code and variant filter", scope: ValidationScope{Kind: ValidationScopeGlobal}, filter: ValidationDiagnosticFilter{Code: "type_ambiguous", Variant: "C+D"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := store.GetValidationIssueGroups(ctx, ValidationIssueGroupRequest{Generation: generation, Scope: test.scope, Filter: test.filter})
			require.NoError(t, err)
			require.NotEmpty(t, response.Groups)
			total := 0
			for _, group := range response.Groups {
				total += group.IssueCount
				narrowed := test.filter
				narrowed.Check, narrowed.Code = group.Check, group.Code
				if group.Variant != nil {
					narrowed.Variant = group.Variant.Key
				}
				summary := validationScopeSummary(t, store, generation, test.scope, narrowed)
				require.Equal(t, summary.IssueCount, group.IssueCount, "%+v", group)
				require.Equal(t, summary.AffectedFileCount, group.AffectedFileCount, "%+v", group)
				narrowed.RepairAvailability = ValidationRepairAvailabilityApplicable
				applicable := validationScopeSummary(t, store, generation, test.scope, narrowed)
				require.Equal(t, applicable.IssueCount, group.ApplicableRepairCount, "%+v", group)
			}
			require.Equal(t, validationScopeSummary(t, store, generation, test.scope, test.filter).IssueCount, total)
		})
	}
}

func validationScopeSummary(t *testing.T, store *Store, generation int64, scope ValidationScope, filter ValidationDiagnosticFilter) ValidationScopeSummary {
	t.Helper()
	response, err := store.GetValidationScopeSummaries(context.Background(), ValidationScopeSummaryRequest{
		Generation: generation, Scopes: []ValidationScope{scope}, Filter: filter,
	})
	require.NoError(t, err)
	require.Len(t, response.Summaries, 1)
	return response.Summaries[0]
}

func TestValidationDiagnosticVariantFilterRequiresCodeAndKeysCursors(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "variant-filter.db"))
	require.NoError(t, err)
	defer store.Close()
	generation := variantStoreFixture(t, store)

	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Filter: ValidationDiagnosticFilter{Variant: "C+D"}})
	require.ErrorIs(t, err, ErrValidationPageFilter)
	_, err = store.GetValidationScopeSummaries(ctx, ValidationScopeSummaryRequest{
		Generation: generation, Scopes: []ValidationScope{{Kind: ValidationScopeGlobal}}, Filter: ValidationDiagnosticFilter{Variant: "C+D"},
	})
	require.ErrorIs(t, err, ErrValidationScopeRequest)

	variantFilter := ValidationDiagnosticFilter{Code: "type_ambiguous", Variant: "C+D"}
	first, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 2, Filter: variantFilter})
	require.NoError(t, err)
	require.Equal(t, 3, first.Total)
	require.Equal(t, []string{"i2", "i5"}, []string{first.Diagnostics[0].IssueKey, first.Diagnostics[1].IssueKey})
	require.Equal(t, &ValidationIssueVariant{Key: "C+D", Label: "C, D"}, first.Diagnostics[0].Variant)
	require.NotEmpty(t, first.NextCursor)

	codeOnly, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 2, Filter: ValidationDiagnosticFilter{Code: "type_ambiguous"}})
	require.NoError(t, err)
	require.Equal(t, 5, codeOnly.Total)
	require.NotEqual(t, codeOnly.FilterIdentity, first.FilterIdentity)
	_, err = store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 2, Cursor: first.NextCursor, Filter: ValidationDiagnosticFilter{Code: "type_ambiguous"}})
	require.ErrorIs(t, err, ErrValidationPageCursor)

	second, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 2, Cursor: first.NextCursor, Filter: variantFilter})
	require.NoError(t, err)
	require.Len(t, second.Diagnostics, 1)
	require.Equal(t, "i9", second.Diagnostics[0].IssueKey)

	views, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Filter: ValidationDiagnosticFilter{Code: "unexpected_field"}})
	require.NoError(t, err)
	require.Nil(t, views.Diagnostics[0].Variant, "variant-free diagnostics read back without a variant")
}

func TestOpen_UpgradesV69WithValidationDiagnosticVariants(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "validation-variants-v69.db")
	store, err := Open(path)
	require.NoError(t, err)
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	published, err := store.PublishValidationSnapshot(ctx, validationStoreFixture(generation, 2))
	require.NoError(t, err)
	require.True(t, published)
	_, err = store.db.ExecContext(ctx, `
		DROP INDEX idx_validation_diagnostics_variant;
		ALTER TABLE validation_diagnostics DROP COLUMN variant_label;
		ALTER TABLE validation_diagnostics DROP COLUMN variant_key;
		UPDATE schema_version SET version = 69;
		UPDATE rzm_migration_state SET version = 69 WHERE domain = 'intel';
		DELETE FROM rzm_migration_log WHERE domain = 'intel' AND from_version >= 69;
	`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	var version int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT version FROM rzm_migration_state WHERE domain = 'intel'`).Scan(&version))
	require.Equal(t, currentSchemaVersion, version)
	exists, err := indexExistsQuery(ctx, store.db, "idx_validation_diagnostics_variant")
	require.NoError(t, err)
	require.True(t, exists)

	page, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation})
	require.NoError(t, err)
	require.Equal(t, 2, page.Total, "published diagnostics survive the migration")
	require.Nil(t, page.Diagnostics[0].Variant, "migrated rows default to no variant")
	groups, err := store.GetValidationIssueGroups(ctx, ValidationIssueGroupRequest{Generation: generation})
	require.NoError(t, err)
	require.Equal(t, []ValidationIssueGroup{{Check: "ontology", Code: "missing_type", IssueCount: 2, AffectedFileCount: 2}}, groups.Groups)
}

func TestGetValidationIssueGroupsRollsUpVariantsBeyondTheLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "groups-limit.db"))
	require.NoError(t, err)
	defer store.Close()
	generation, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)

	// The first target has two links in one note; every other target has one.
	extra := 2
	targets := ValidationIssueGroupVariantLimit + extra
	diagnostics := make([]ValidationDiagnostic, 0, targets+1)
	for index := range targets {
		target := fmt.Sprintf("Target %02d", index)
		diagnostics = append(diagnostics, ValidationDiagnostic{
			IssueKey: fmt.Sprintf("link-%02d", index), Check: "broken_links", Code: "broken_note_link",
			Variant: &ValidationIssueVariant{Key: target, Label: target}, PrimaryPath: "shared.md", AffectedPaths: []string{"shared.md"},
		})
	}
	diagnostics = append(diagnostics, ValidationDiagnostic{
		IssueKey: "link-00b", Check: "broken_links", Code: "broken_note_link",
		Variant: &ValidationIssueVariant{Key: "Target 00", Label: "Target 00"}, PrimaryPath: "other.md", AffectedPaths: []string{"other.md"},
	})
	published, err := store.PublishValidationSnapshot(ctx, ValidationSnapshot{
		VaultIdentity: "vault", Generation: generation, Scope: "default", SelectedChecks: []string{"broken_links"},
		Completion: ValidationCompletionComplete, IssueCount: len(diagnostics), AffectedFileCount: 2,
		Checks:      []ValidationCheckSnapshot{{Check: "broken_links", Outcome: ValidationCheckOutcomeCompleted, IssueCount: len(diagnostics)}},
		Diagnostics: diagnostics,
	})
	require.NoError(t, err)
	require.True(t, published)

	response, err := store.GetValidationIssueGroups(ctx, ValidationIssueGroupRequest{Generation: generation})
	require.NoError(t, err)
	require.Len(t, response.Groups, ValidationIssueGroupVariantLimit+1)
	require.Equal(t, ValidationIssueGroup{
		Check: "broken_links", Code: "broken_note_link", Variant: &ValidationIssueVariant{Key: "Target 00", Label: "Target 00"},
		IssueCount: 2, AffectedFileCount: 2,
	}, response.Groups[0])
	// Ties order by key, so the last targets fall past the limit.
	require.Equal(t, ValidationIssueGroup{
		Check: "broken_links", Code: "broken_note_link", IssueCount: extra, AffectedFileCount: 1, OtherVariants: extra,
	}, response.Groups[ValidationIssueGroupVariantLimit])
	total := 0
	for _, group := range response.Groups {
		total += group.IssueCount
	}
	require.Equal(t, len(diagnostics), total)
}
