package validationrun

import (
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestBuildDiagnosticSnapshotPreservesApplicabilityAndExecutionOutcomes(t *testing.T) {
	result := ValidationResult{
		Result: validate.Result{
			SelectedChecks: []string{"broken-links", "views", "code-anchors", "ontology"},
			ErrorCount:     1,
			Checks: []validate.CheckResult{
				{Name: "broken-links", OK: true},
				{Name: "views", Error: "query failed"},
			},
		},
		Outcomes: []ValidationCheckOutcome{
			{Check: "broken-links", Outcome: validate.CheckOutcomeCompleted},
			{Check: "views", Outcome: validate.CheckOutcomeCompleted},
			{Check: "code-anchors", Outcome: validate.CheckOutcomeBlocked, Summary: "index required"},
			{Check: "ontology", Outcome: validate.CheckOutcomeNotApplicable, Summary: "schema absent"},
		},
	}

	snapshot, err := BuildDiagnosticSnapshot(result, validate.DiagnosticSnapshotContext{
		VaultIdentity: "vault", Generation: 7,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		semdb.ValidationCheckOutcomeCompleted,
		semdb.ValidationCheckOutcomeFailed,
		semdb.ValidationCheckOutcomeBlocked,
		semdb.ValidationCheckOutcomeNotApplicable,
	}, []string{
		snapshot.Checks[0].Outcome,
		snapshot.Checks[1].Outcome,
		snapshot.Checks[2].Outcome,
		snapshot.Checks[3].Outcome,
	})
	require.Equal(t, "index required", snapshot.Checks[2].Summary)
	require.Equal(t, "schema absent", snapshot.Checks[3].Summary)
	require.Equal(t, semdb.ValidationCompletionIncomplete, snapshot.Completion)
}
