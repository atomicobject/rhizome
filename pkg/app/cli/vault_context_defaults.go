package actions

// VaultContextSummaryDefaults defines the shared "shape defaults" for vault context
// summaries. These are used by both the CLI graph vault-context command and the
// MCP vault_context tool so they stay in sync.
type VaultContextSummaryDefaults struct {
	MaxCommunities          int
	CommunityTopNotes       int
	CommunityTopTags        int
	BridgeLimit             int
	TopOrphansLimit         int
	TopComponentsLimit      int
	TopGlobalAuthorityLimit int
}

// DefaultVaultContextSummaryDefaults is the canonical set of defaults for vault
// summaries. Keep this conservative: callers can opt in to larger payloads.
var DefaultVaultContextSummaryDefaults = VaultContextSummaryDefaults{
	MaxCommunities:          20,
	CommunityTopNotes:       5,
	CommunityTopTags:        5,
	BridgeLimit:             3,
	TopOrphansLimit:         10,
	TopComponentsLimit:      5,
	TopGlobalAuthorityLimit: 10,
}
