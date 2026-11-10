package runtime

// Global event kinds identify vault-wide changes delivered to live clients.
const (
	GlobalEventValidateChanged         = "validate.changed"
	GlobalEventIndexChanged            = "index.changed"
	GlobalEventNodeChanged             = "node.changed"
	GlobalEventValidationInvalidated   = "validation.invalidated"
	GlobalEventSchemaInvalidated       = "schema.invalidated"
	GlobalEventQueryRecipeInvalidated  = "query_recipe.invalidated"
	GlobalEventIndexInvalidated        = "index.invalidated"
	GlobalEventCapabilitiesInvalidated = "capabilities.invalidated"
	GlobalEventEditSessionInvalidated  = "edit_session.invalidated"
	GlobalEventViewsChanged            = "views.changed"
	GlobalEventViewPreferencesChanged  = "view_preferences.changed"
	GlobalEventStarterReadinessChanged = "starter-readiness.changed"

	// A failed reconciliation leaves validation stale without admitting a refresh.
	GlobalEventReasonReconcileFailed = "reconcile-failed"
)
