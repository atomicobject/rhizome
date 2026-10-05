package actions

import "github.com/atomicobject/rhizome/pkg/validate"

// This small diagnostic survives transport limits; the full result remains in
// Payload. Earlier recovered requests never become the current request's status.
func noteNamespaceMutationStatus(result validate.NamespaceMutationResult) map[string]any {
	outcome := func(value validate.NamespaceOutcome) map[string]any {
		decision := value.Decision
		if decision == "" {
			decision = validate.NamespaceNotStarted
		}
		return map[string]any{
			"decision": string(decision), "transactionId": value.TransactionID,
			"receiptPath": value.ReceiptPath, "recoveryPending": value.RecoveryPending,
		}
	}
	const recoveredLimit = 8
	recovered := make([]any, 0, min(len(result.Recovered), recoveredLimit))
	for _, value := range result.Recovered[:min(len(result.Recovered), recoveredLimit)] {
		recovered = append(recovered, outcome(value))
	}
	return map[string]any{
		"current": outcome(result.Current), "recovered": recovered,
		"recoveredCount": len(result.Recovered), "recoveredTruncated": len(result.Recovered) > recoveredLimit,
	}
}
