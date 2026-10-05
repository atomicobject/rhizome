package dispatch

type ConflictOutcome string

const (
	ConflictAccepted ConflictOutcome = "accepted"
	ConflictReview   ConflictOutcome = "review"
)

func ResolveAssignmentConflict(observedVersion, currentVersion int) ConflictOutcome {
	if observedVersion != currentVersion {
		return ConflictReview
	}
	return ConflictAccepted
}
