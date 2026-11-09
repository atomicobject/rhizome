package readmodel

// EdgeEndpoint selects exactly one note-root or embedded endpoint.
type EdgeEndpoint struct{ Path, NodeID string }

// NeighborhoodQuery filters indexed edges before hydration. PerSourceLimit
// bounds note-target groups. Groups with embedded targets remain complete for
// canonical ordering. The limit must be zero when target-type filters need Go.
// DstTypes narrows outbound note targets to the listed indexed types; untyped
// and embedded targets still return for the caller's own type check.
type NeighborhoodQuery struct {
	Sources                           []EdgeEndpoint
	Direction                         string
	Relations, Provenance             []string
	DstTypes                          []string `json:",omitempty"`
	IncludeStructural, IncludeAmbient bool
	PerSourceLimit                    int
}
