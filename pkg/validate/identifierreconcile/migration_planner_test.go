package identifierreconcile

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestBuildMigrationPlanSequentialToDateTime(t *testing.T) {
	target := ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}
	members := []MigrationMember{
		migrationMember(t, "docs/2026-08-06-12-20-third.md", "^third", "EFF-0010"),
		migrationMember(t, "docs/2026-08-06-12-20-first.md", "^first", "EFF-0002"),
		migrationMember(t, "docs/2026-08-06-12-20-second.md", "^second", "EFF-0007"),
		migrationMember(t, "docs/2026-08-06-12-21-next.md", "^next", "EFF-0001"),
	}

	plan, err := BuildMigrationPlan(MigrationInput{TargetFormat: target, Members: members})
	require.NoError(t, err)
	require.Equal(t, ontology.IdentifierStrategySequential, plan.SourcePool.Strategy)
	require.Equal(t, ontology.IdentifierStrategyDateTime, plan.TargetPool.Strategy)
	require.Equal(t, []string{
		"EFF-2026-08-06-12-20",
		"EFF-2026-08-06-12-20-2",
		"EFF-2026-08-06-12-20-3",
		"EFF-2026-08-06-12-21",
	}, migrationNewValues(plan.Rewrites))
	require.NotEmpty(t, plan.Key)
	require.NotEmpty(t, plan.Fingerprint)

	snapshot, err := plan.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, plan.Rewrites, snapshot.Rewrites)
}

func TestBuildMigrationPlanDateTimeToSequentialUsesDenseChronologicalOrderAndTargetPad(t *testing.T) {
	target := ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategySequential, Prefix: "EFF", Separator: "-", Pad: 3}
	members := []MigrationMember{
		migrationMember(t, "docs/c.md", "^c", "EFF-2026-08-06-12-21"),
		migrationMember(t, "docs/b.md", "^b", "EFF-2026-08-06-12-20-2"),
		migrationMember(t, "docs/a.md", "^a", "EFF-2026-08-06-12-20"),
	}

	plan, err := BuildMigrationPlan(MigrationInput{TargetFormat: target, Members: members})
	require.NoError(t, err)
	require.Equal(t, []string{"EFF-001", "EFF-002", "EFF-003"}, migrationNewValues(plan.Rewrites))
	require.Equal(t, []string{"EFF-2026-08-06-12-20", "EFF-2026-08-06-12-20-2", "EFF-2026-08-06-12-21"}, migrationOldValues(plan.Rewrites))
}

func TestBuildMigrationPlanIsStableAcrossInputOrder(t *testing.T) {
	formats := []ontology.IdentifierFormat{
		{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"},
		{Strategy: ontology.IdentifierStrategySequential, Prefix: "EFF", Separator: "-", Pad: 4},
	}
	memberSets := [][]MigrationMember{
		{
			migrationMember(t, "docs/2026-08-06-12-20-b.md", "^b", "EFF-0002"),
			migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001"),
		},
		{
			migrationMember(t, "docs/b.md", "^b", "EFF-2026-08-06-12-20-2"),
			migrationMember(t, "docs/a.md", "^a", "EFF-2026-08-06-12-20"),
		},
	}
	for index := range formats {
		first, err := BuildMigrationPlan(MigrationInput{TargetFormat: formats[index], Members: memberSets[index]})
		require.NoError(t, err)
		reversed := slices.Clone(memberSets[index])
		slices.Reverse(reversed)
		second, err := BuildMigrationPlan(MigrationInput{TargetFormat: formats[index], Members: reversed})
		require.NoError(t, err)
		require.Equal(t, first.Fingerprint, second.Fingerprint)
		firstJSON, err := json.Marshal(first)
		require.NoError(t, err)
		secondJSON, err := json.Marshal(second)
		require.NoError(t, err)
		require.Equal(t, firstJSON, secondJSON)
	}
}

func TestBuildMigrationPlanEmptyPoolIsStableAndHasNoRewrites(t *testing.T) {
	target := ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}
	plan, err := BuildMigrationPlan(MigrationInput{TargetFormat: target})
	require.NoError(t, err)
	require.Empty(t, plan.Rewrites)
	require.NotEmpty(t, plan.Fingerprint)
}

func TestBuildMigrationPlanHandlesLargeSameMinutePoolDeterministically(t *testing.T) {
	const count = 500
	members := make([]MigrationMember, 0, count)
	for ordinal := count; ordinal >= 1; ordinal-- {
		path := fmt.Sprintf("docs/2026-08-06-12-20-effort-%04d.md", ordinal)
		members = append(members, migrationMember(t, path, fmt.Sprintf("^effort-%04d", ordinal), fmt.Sprintf("EFF-%04d", ordinal)))
	}
	plan, err := BuildMigrationPlan(MigrationInput{
		TargetFormat: ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"},
		Members:      members,
	})
	require.NoError(t, err)
	// Every member gets its own ordinal: the first is the bare minute and the
	// rest take -2 through -500, so an interior duplicate or skip is visible.
	want := make(map[string]string, count)
	for ordinal := 1; ordinal <= count; ordinal++ {
		value := "EFF-2026-08-06-12-20"
		if ordinal > 1 {
			value = fmt.Sprintf("%s-%d", value, ordinal)
		}
		want[fmt.Sprintf("docs/2026-08-06-12-20-effort-%04d.md", ordinal)] = value
	}
	assignments := func(rewrites []MigrationRewrite) map[string]string {
		got := make(map[string]string, len(rewrites))
		for _, rewrite := range rewrites {
			got[rewrite.Node.NotePath] = rewrite.NewIdentifier
		}
		return got
	}
	require.Len(t, plan.Rewrites, count)
	require.Equal(t, want, assignments(plan.Rewrites))

	reversed := slices.Clone(members)
	slices.Reverse(reversed)
	again, err := BuildMigrationPlan(MigrationInput{
		TargetFormat: ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"},
		Members:      reversed,
	})
	require.NoError(t, err)
	require.Equal(t, plan.Rewrites, again.Rewrites)
	require.Equal(t, plan.Fingerprint, again.Fingerprint)
}

func TestBuildMigrationPlanFailsClosed(t *testing.T) {
	datetimeTarget := ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"}
	sequentialTarget := ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategySequential, Prefix: "EFF", Separator: "-", Pad: 4}
	duplicateNode := migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001")
	tests := []struct {
		name       string
		input      MigrationInput
		wantCode   MigrationErrorCode
		wantTarget string
	}{
		{name: "mixed strategies", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001"),
			migrationMember(t, "docs/2026-08-06-12-21-b.md", "^b", "EFF-2026-08-06-12-21"),
		}}, wantCode: MigrationErrorMixedStrategy},
		{name: "malformed old value", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-nope"),
		}}, wantCode: MigrationErrorInvalidSourceValue},
		{name: "inconsistent sequential padding", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-1"),
			migrationMember(t, "docs/2026-08-06-12-21-b.md", "^b", "EFF-0002"),
		}}, wantCode: MigrationErrorMixedStrategy},
		{name: "invalid filename stamp", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			migrationMember(t, "docs/no-stamp.md", "^a", "EFF-0001"),
		}}, wantCode: MigrationErrorInvalidProspectivePath},
		{name: "filename path differs from node", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			{Node: migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001").Node, PreferredValue: "EFF-0001", ProspectivePath: "docs/2026-08-06-12-21-other.md"},
		}}, wantCode: MigrationErrorInvalidProspectivePath},
		{name: "duplicate node", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			duplicateNode, duplicateNode,
		}}, wantCode: MigrationErrorDuplicateNode},
		{name: "duplicate preferred", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001"),
			migrationMember(t, "docs/2026-08-06-12-21-b.md", "^b", "eff-0001"),
		}}, wantCode: MigrationErrorDuplicatePreferred},
		{name: "already target strategy", input: MigrationInput{TargetFormat: sequentialTarget, Members: []MigrationMember{
			migrationMember(t, "docs/a.md", "^a", "EFF-0001"),
		}}, wantCode: MigrationErrorNotRequired},
		{name: "external target reservation", input: MigrationInput{TargetFormat: datetimeTarget, Members: []MigrationMember{
			migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001"),
		}, TargetReservations: []ontology.IdentifierReservation{{Value: "EFF-2026-08-06-12-20", Owner: "external"}}}, wantCode: MigrationErrorTargetReserved},
		{name: "external sequential target reservation", input: MigrationInput{TargetFormat: sequentialTarget, Members: []MigrationMember{
			migrationMember(t, "docs/a.md", "^a", "EFF-2026-08-06-12-20"),
			migrationMember(t, "docs/b.md", "^b", "EFF-2026-08-06-12-21"),
		}, TargetReservations: []ontology.IdentifierReservation{{Value: "eff-0002", Owner: "external"}}}, wantCode: MigrationErrorTargetReserved},
		{name: "nonallocatable target", input: MigrationInput{TargetFormat: ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "", Separator: "-"}}, wantCode: MigrationErrorNonallocatablePool},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildMigrationPlan(test.input)
			require.Error(t, err)
			var migrationErr *MigrationError
			require.True(t, errors.As(err, &migrationErr))
			require.Equal(t, test.wantCode, migrationErr.Code)
		})
	}
}

func TestBuildMigrationPlanAllowsNoncollidingRetainedTargetReservations(t *testing.T) {
	tests := []MigrationInput{
		{
			TargetFormat:       ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"},
			Members:            []MigrationMember{migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001")},
			TargetReservations: []ontology.IdentifierReservation{{Value: "EFF-2026-08-06-12-21", Owner: "retained"}},
		},
		{
			TargetFormat:       ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategySequential, Prefix: "EFF", Separator: "-", Pad: 4},
			Members:            []MigrationMember{migrationMember(t, "docs/a.md", "^a", "EFF-2026-08-06-12-20")},
			TargetReservations: []ontology.IdentifierReservation{{Value: "EFF-0002", Owner: "retained"}},
		},
	}
	for _, input := range tests {
		plan, err := BuildMigrationPlan(input)
		require.NoError(t, err)
		require.Len(t, plan.Rewrites, 1)
	}
}

func TestBuildMigrationPlanFailureEvidenceIsStableAcrossInputOrder(t *testing.T) {
	input := MigrationInput{
		TargetFormat: ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"},
		Members: []MigrationMember{
			migrationMember(t, "docs/2026-08-06-12-21-b.md", "^b", "EFF-2026-08-06-12-21"),
			migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001"),
		},
	}
	_, firstErr := BuildMigrationPlan(input)
	slices.Reverse(input.Members)
	_, secondErr := BuildMigrationPlan(input)
	var first, second *MigrationError
	require.ErrorAs(t, firstErr, &first)
	require.ErrorAs(t, secondErr, &second)
	require.Equal(t, first, second)
}

func TestMigrationPlanValidatedSnapshotRejectsMutation(t *testing.T) {
	plan, err := BuildMigrationPlan(MigrationInput{
		TargetFormat: ontology.IdentifierFormat{Strategy: ontology.IdentifierStrategyDateTime, Prefix: "EFF", Separator: "-"},
		Members:      []MigrationMember{migrationMember(t, "docs/2026-08-06-12-20-a.md", "^a", "EFF-0001")},
	})
	require.NoError(t, err)
	plan.Rewrites[0].NewIdentifier = "EFF-2026-08-06-12-21"
	_, err = plan.ValidatedSnapshot()
	require.ErrorContains(t, err, "fingerprint")
}

func migrationMember(t *testing.T, notePath, fragment, preferred string) MigrationMember {
	t.Helper()
	node, err := NewCanonicalNodeKey(notePath, fragment, "EffortNote", "id")
	require.NoError(t, err)
	return MigrationMember{Node: node, PreferredValue: preferred, ProspectivePath: notePath}
}

func migrationOldValues(rewrites []MigrationRewrite) []string {
	values := make([]string, 0, len(rewrites))
	for _, rewrite := range rewrites {
		values = append(values, rewrite.OldIdentifier)
	}
	return values
}

func migrationNewValues(rewrites []MigrationRewrite) []string {
	values := make([]string, 0, len(rewrites))
	for _, rewrite := range rewrites {
		values = append(values, rewrite.NewIdentifier)
	}
	return values
}
