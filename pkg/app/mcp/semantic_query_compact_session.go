package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
)

func finalizeCompactSemanticQueryResponse(tracker *sessionTracker, encoded []byte, budgetChars int) ([]byte, error) {
	if tracker == nil {
		return encoded, nil
	}
	var rendered semanticQueryResponse
	if err := json.Unmarshal(encoded, &rendered); err != nil {
		return nil, err
	}
	if rendered.Compact == nil {
		return encoded, nil
	}
	items := make([]actions.DedupeItem, 0, len(rendered.Compact.Sources))
	for _, source := range rendered.Compact.Sources {
		if compactSourceNeedsDedupe(source) {
			items = append(items, actions.DedupeItem{Key: source.Ref, Fingerprint: fingerprintText(source.Body.Content)})
		}
	}
	// Reserve the final budget-selected representations atomically. Competing
	// requests cannot both emit them, and failed encoding releases ownership.
	reservation := tracker.Reserve(items)
	var emitted []actions.DedupeItem
	defer func() { reservation.Commit(emitted) }()
	for i := range rendered.Compact.Sources {
		source := &rendered.Compact.Sources[i]
		if !compactSourceNeedsDedupe(*source) {
			continue
		}
		if !reservation.Allowed(source.Ref, fingerprintText(source.Body.Content)) {
			source.Body.Content = ""
			source.Body.Deduped = true
			source.Included = false
			rendered.DedupeHits++
		}
	}
	// Do not shorten a reserved body again: its exact fingerprint owns delivery.
	// If the dedupe qualifications cannot fit, release every reservation instead.
	// Opt-in diagnostics ride outside the content budget, so measure the
	// budgeted payload without them.
	diagnostics := rendered.Diagnostics
	rendered.Diagnostics = nil
	budgeted, err := json.Marshal(rendered)
	if err != nil {
		return nil, err
	}
	if budgetChars <= 0 {
		budgetChars = DefaultBudgetChars()
	}
	if len(budgeted) > budgetChars {
		return nil, fmt.Errorf("compact response exceeds budget %d while preserving dedupe qualifications", budgetChars)
	}
	rendered.Diagnostics = diagnostics
	final, err := json.Marshal(rendered)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if reservation.Allowed(item.Key, item.Fingerprint) {
			emitted = append(emitted, item)
		}
	}
	return final, nil
}

func compactSourceNeedsDedupe(source SemanticCompactSource) bool {
	return source.Body != nil && !source.Body.Deduped && source.Body.Kind != string(planStub) && source.Body.Content != "" && strings.TrimSpace(source.Ref) != ""
}
