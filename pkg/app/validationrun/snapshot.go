package validationrun

import (
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/validate"
)

// BuildDiagnosticSnapshot preserves applicability evidence that is not part of
// the executed suite Result. Blocked and not-applicable checks therefore remain
// distinguishable from failed, skipped, and successfully completed checks in
// the durable public snapshot.
func BuildDiagnosticSnapshot(result ValidationResult, build validate.DiagnosticSnapshotContext) (semdb.ValidationSnapshot, error) {
	build.CheckOutcomes = make([]validate.DiagnosticCheckOutcome, 0, len(result.Outcomes))
	for _, outcome := range result.Outcomes {
		build.CheckOutcomes = append(build.CheckOutcomes, validate.DiagnosticCheckOutcome{
			Check: outcome.Check, Outcome: outcome.Outcome, Summary: outcome.Summary,
		})
		if outcome.Outcome == validate.CheckOutcomeBlocked && strings.TrimSpace(build.Completion) == "" {
			build.Completion = semdb.ValidationCompletionIncomplete
			if strings.TrimSpace(build.StaleReason) == "" {
				build.StaleReason = "one or more selected checks were blocked"
			}
		}
	}
	return validate.BuildDiagnosticSnapshot(result.Result, build)
}
