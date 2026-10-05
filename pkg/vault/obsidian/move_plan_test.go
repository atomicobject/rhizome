package obsidian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlanGovernedIdentifierMove_RewritesExactBoundedTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		oldID      string
		newID      string
		wantTarget string
		wantRename bool
	}{
		{
			name:       "token at basename start",
			source:     "specs/SPEC-0075-contract.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/SPEC-0091-contract.md",
			wantRename: true,
		},
		{
			name:       "token at basename end",
			source:     "specs/contract-SPEC-0075.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/contract-SPEC-0091.md",
			wantRename: true,
		},
		{
			name:       "repeated bounded tokens",
			source:     "specs/SPEC-0075-to-SPEC-0075.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/SPEC-0091-to-SPEC-0091.md",
			wantRename: true,
		},
		{
			name:       "underscore and punctuation are boundaries",
			source:     "specs/a_SPEC-0075.version.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/a_SPEC-0091.version.md",
			wantRename: true,
		},
		{
			name:       "adjacent ASCII letter blocks replacement",
			source:     "specs/xSPEC-0075-contract.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/xSPEC-0075-contract.md",
		},
		{
			name:       "adjacent Unicode letter blocks replacement",
			source:     "specs/αSPEC-0075-contract.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/αSPEC-0075-contract.md",
		},
		{
			name:       "adjacent Unicode digit blocks replacement",
			source:     "specs/SPEC-0075١-contract.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/SPEC-0075١-contract.md",
		},
		{
			name:       "case mismatch is not governed",
			source:     "specs/spec-0075-contract.md",
			oldID:      "SPEC-0075",
			newID:      "SPEC-0091",
			wantTarget: "specs/spec-0075-contract.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, conflict, err := PlanGovernedIdentifierMove(GovernedMoveRequest{
				SourcePath: tt.source,
				OldID:      tt.oldID,
				NewID:      tt.newID,
			})
			require.NoError(t, err)
			require.Nil(t, conflict)
			require.Equal(t, tt.source, plan.SourcePath)
			require.Equal(t, tt.wantTarget, plan.DestinationPath)
			require.Equal(t, tt.wantRename, plan.Renamed)
		})
	}
}

func TestPlanGovernedIdentifierMove_NormalizesAndPreservesParentAndExtension(t *testing.T) {
	t.Parallel()

	plan, conflict, err := PlanGovernedIdentifierMove(GovernedMoveRequest{
		SourcePath: `./specs\technical\2026-SPEC-0075-contract.md`,
		OldID:      "SPEC-0075",
		NewID:      "SPEC-0091",
	})
	require.NoError(t, err)
	require.Nil(t, conflict)
	require.Equal(t, "specs/technical/2026-SPEC-0075-contract.md", plan.SourcePath)
	require.Equal(t, "specs/technical/2026-SPEC-0091-contract.md", plan.DestinationPath)
	require.True(t, plan.Renamed)
}

func TestPlanGovernedIdentifierMove_BlocksPortableSiblingCollisions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		siblings []string
		wantPath string
	}{
		{
			name:     "exact collision",
			siblings: []string{"specs/contract-SPEC-0091.md"},
			wantPath: "specs/contract-SPEC-0091.md",
		},
		{
			name:     "case fold collision",
			siblings: []string{"specs/CONTRACT-spec-0091.md"},
			wantPath: "specs/CONTRACT-spec-0091.md",
		},
		{
			name: "deterministic collision independent of input order",
			siblings: []string{
				"specs/contract-spec-0091.md",
				"specs/CONTRACT-SPEC-0091.md",
			},
			wantPath: "specs/CONTRACT-SPEC-0091.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, conflict, err := PlanGovernedIdentifierMove(GovernedMoveRequest{
				SourcePath:   "specs/contract-SPEC-0075.md",
				OldID:        "SPEC-0075",
				NewID:        "SPEC-0091",
				SiblingPaths: tt.siblings,
			})
			require.NoError(t, err)
			require.NotNil(t, conflict)
			require.Equal(t, "specs/contract-SPEC-0091.md", conflict.DestinationPath)
			require.Equal(t, tt.wantPath, conflict.ExistingPath)
			require.True(t, plan.Renamed)
		})
	}
}

func TestPlanGovernedIdentifierMove_IgnoresSourceAndOtherDirectories(t *testing.T) {
	t.Parallel()

	plan, conflict, err := PlanGovernedIdentifierMove(GovernedMoveRequest{
		SourcePath: "specs/contract-SPEC-0075.md",
		OldID:      "SPEC-0075",
		NewID:      "SPEC-0091",
		SiblingPaths: []string{
			"specs/contract-SPEC-0075.md",
			"archive/contract-SPEC-0091.md",
		},
	})
	require.NoError(t, err)
	require.Nil(t, conflict)
	require.Equal(t, "specs/contract-SPEC-0091.md", plan.DestinationPath)
	require.True(t, plan.Renamed)
}

func TestPlanGovernedIdentifierMove_RejectsMissingInputs(t *testing.T) {
	t.Parallel()

	for _, req := range []GovernedMoveRequest{
		{OldID: "SPEC-0075", NewID: "SPEC-0091"},
		{SourcePath: "specs/SPEC-0075.md", NewID: "SPEC-0091"},
		{SourcePath: "specs/SPEC-0075.md", OldID: "SPEC-0075"},
	} {
		_, _, err := PlanGovernedIdentifierMove(req)
		require.Error(t, err)
	}
}

func TestPlanGovernedIdentifierMove_RejectsPathsOutsideVault(t *testing.T) {
	t.Parallel()

	_, _, err := PlanGovernedIdentifierMove(GovernedMoveRequest{
		SourcePath: "../SPEC-0075.md",
		OldID:      "SPEC-0075",
		NewID:      "SPEC-0091",
	})
	require.ErrorContains(t, err, "vault-relative")

	_, _, err = PlanGovernedIdentifierMove(GovernedMoveRequest{
		SourcePath:   "specs/SPEC-0075.md",
		OldID:        "SPEC-0075",
		NewID:        "SPEC-0091",
		SiblingPaths: []string{"../SPEC-0091.md"},
	})
	require.ErrorContains(t, err, "vault-relative")
}

func TestPlanGovernedIdentifierMove_RejectsIdentifierPathSeparators(t *testing.T) {
	t.Parallel()

	for _, newID := range []string{"../SPEC-0091", "nested/SPEC-0091", `nested\SPEC-0091`} {
		_, _, err := PlanGovernedIdentifierMove(GovernedMoveRequest{
			SourcePath: "specs/SPEC-0075.md",
			OldID:      "SPEC-0075",
			NewID:      newID,
		})
		require.ErrorContains(t, err, "single filename token")
	}
}
