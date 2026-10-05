package bootstrap

// freshnessEvent is one processed freshness notification published to
// in-process subscribers. SPEC-0104 deletes follower mode, so these events no
// longer travel through a hint log: the vault runtime is the only process
// applying changes, and its web clients subscribe directly.
type freshnessEvent struct {
	Kind   string
	Paths  []string
	Reason string
}

func (w *unifiedSemanticWatcher) publishFreshnessEvents(events []freshnessEvent) {
	if w == nil || w.publishEvent == nil {
		return
	}
	for _, event := range events {
		w.publishEvent(event.Kind, freshnessEventData(event))
	}
}

func freshnessEventData(event freshnessEvent) any {
	data := map[string]any{}
	if len(event.Paths) > 0 {
		data["paths"] = event.Paths
	}
	if event.Reason != "" {
		data["reason"] = event.Reason
	}
	if len(data) == 0 {
		return nil
	}
	data["source"] = "vault-runtime"
	if event.Kind == "node.changed" {
		data["domains"] = []string{"metadata", "ontology", "code"}
	}
	return data
}

// publishProcessedEvents announces what this reconciliation absorbed.
func (w *unifiedSemanticWatcher) publishProcessedEvents(cacheResynced bool, noteChanges, noteDeletes, codeChanges, codeDeletes, internalChanges []string, forceMetadataRefresh, forceOntologyRefresh bool) {
	if w == nil {
		return
	}
	if cacheResynced {
		w.publishFreshnessEvents([]freshnessEvent{
			{Kind: globalEventIndexInvalidated, Reason: "resync"},
			{Kind: globalEventValidationInvalidated, Reason: "resync"},
		})
	}
	w.publishFreshnessEvents(processedFreshnessEvents(noteChanges, noteDeletes, codeChanges, codeDeletes, internalChanges, forceMetadataRefresh, forceOntologyRefresh))
}

// publishInvalidation reports a failed reconciliation. Validation consumers use
// the reason to keep the published snapshot stale rather than refreshing
// against an index that never absorbed the change.
func (w *unifiedSemanticWatcher) publishInvalidation(reason string) {
	if w == nil {
		return
	}
	w.publishFreshnessEvents([]freshnessEvent{
		{Kind: globalEventIndexInvalidated, Reason: reason},
		{Kind: globalEventValidationInvalidated, Reason: globalEventReasonReconcileFailed},
	})
}

func processedFreshnessEvents(noteChanges, noteDeletes, codeChanges, codeDeletes, internalChanges []string, forceMetadataRefresh, forceOntologyRefresh bool) []freshnessEvent {
	var events []freshnessEvent
	indexPaths := make([]string, 0, len(noteChanges)+len(noteDeletes)+len(codeChanges)+len(codeDeletes)+len(internalChanges))
	indexPaths = append(indexPaths, noteChanges...)
	indexPaths = append(indexPaths, noteDeletes...)
	indexPaths = append(indexPaths, codeChanges...)
	indexPaths = append(indexPaths, codeDeletes...)

	if len(indexPaths) > 0 {
		events = append(events, freshnessEvent{Kind: "node.changed", Paths: indexPaths})
	}
	if len(indexPaths) > 0 || len(internalChanges) > 0 || forceMetadataRefresh || forceOntologyRefresh {
		events = append(events, freshnessEvent{Kind: globalEventValidationInvalidated, Paths: append([]string{}, indexPaths...)})
	}
	if forceOntologyRefresh {
		events = append(events, freshnessEvent{Kind: globalEventSchemaInvalidated, Paths: append([]string{}, internalChanges...)})
		events = append(events, freshnessEvent{Kind: globalEventCapabilitiesInvalidated, Paths: append([]string{}, internalChanges...)})
	}
	for _, rel := range internalChanges {
		effect := classifyInternalChange(rel)
		if effect.invalidateCapabilities {
			events = append(events, freshnessEvent{Kind: globalEventCapabilitiesInvalidated, Paths: []string{rel}})
		}
		if effect.invalidateQueryRecipes {
			events = append(events, freshnessEvent{Kind: globalEventQueryRecipeInvalidated, Paths: []string{rel}})
		}
	}
	return events
}
