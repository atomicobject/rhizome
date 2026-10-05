package actions

// MetadataStoreFallbackPolicy controls whether an action may open its own
// metadata store when the caller did not supply one. The zero value preserves
// the established CLI and long-lived MCP behavior.
type MetadataStoreFallbackPolicy string

const (
	// MetadataStoreFallbackOpen permits the historical best-effort store open.
	MetadataStoreFallbackOpen MetadataStoreFallbackPolicy = ""
	// MetadataStoreFallbackLive keeps a planned one-shot action on live data
	// when its caller-managed index is unavailable. It must not create a store.
	MetadataStoreFallbackLive MetadataStoreFallbackPolicy = "live"
)

func (policy MetadataStoreFallbackPolicy) allowsStoreOpen() bool {
	return policy != MetadataStoreFallbackLive
}
