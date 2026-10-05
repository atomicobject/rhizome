package agentcode

import (
	"encoding/json"
	"unicode/utf8"
)

// Preserve only the fixed note-mutation diagnostic, with a hard encoded bound.
// Source paths, move lists, summaries, arbitrary diagnostics and callbacks are
// not part of this status projection.
func boundedMutationStatus(diagnostic any) json.RawMessage {
	fields, ok := diagnostic.(map[string]any)
	if !ok {
		return nil
	}
	status, ok := fields["mutation"].(map[string]any)
	if !ok {
		return nil
	}
	current, ok := mutationOutcomeStatus(status["current"])
	if !ok {
		return nil
	}
	count, ok := mutationRecoveredCount(status["recoveredCount"])
	if !ok {
		return nil
	}
	truncated, ok := status["recoveredTruncated"].(bool)
	if !ok {
		return nil
	}
	values, ok := status["recovered"].([]any)
	if !ok {
		return nil
	}
	var recovered []any
	for _, value := range values[:min(len(values), 8)] {
		if item, valid := mutationOutcomeStatus(value); valid {
			recovered = append(recovered, item)
		} else {
			truncated = true
		}
	}
	truncated = truncated || len(values) > len(recovered) || count > len(recovered)
	projection := map[string]any{
		"current": current, "recovered": recovered,
		"recoveredCount": count, "recoveredTruncated": truncated,
	}
	const maxStatusBytes = 8 << 10
	for {
		raw, err := json.Marshal(projection)
		if err != nil {
			return nil
		}
		if len(raw) <= maxStatusBytes {
			return raw
		}
		if len(recovered) == 0 {
			return nil
		}
		recovered = recovered[:len(recovered)-1]
		projection["recovered"], projection["recoveredTruncated"] = recovered, true
	}
}

func mutationOutcomeStatus(value any) (map[string]any, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	decision, ok := fields["decision"].(string)
	if !ok {
		return nil, false
	}
	switch decision {
	case "not_started", "restored", "committed", "unresolved":
	default:
		return nil, false
	}
	tx, ok := fields["transactionId"].(string)
	if !ok || len(tx) > 128 || !utf8.ValidString(tx) {
		return nil, false
	}
	receipt, ok := fields["receiptPath"].(string)
	if !ok || len(receipt) > 1024 || !utf8.ValidString(receipt) {
		return nil, false
	}
	pending, ok := fields["recoveryPending"].(bool)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"decision": decision, "transactionId": tx, "receiptPath": receipt, "recoveryPending": pending,
	}, true
}

func mutationRecoveredCount(value any) (int, bool) {
	switch count := value.(type) {
	case int:
		return count, count >= 0 && count <= 1<<31-1
	case float64: // A remote runtime outcome has crossed JSON decoding.
		if count >= 0 && count <= 1<<31-1 && float64(int(count)) == count {
			return int(count), true
		}
	}
	return 0, false
}
