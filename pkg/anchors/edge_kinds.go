package codeanchor

import "strings"

// EdgeDomain identifies which graph domain an edge belongs to.
type EdgeDomain string

const (
	EdgeDomainDoc  EdgeDomain = "doc"  // edges involving notes
	EdgeDomainCode EdgeDomain = "code" // code-to-code edges
)

// EdgeKindDef defines the semantics for an edge kind.
type EdgeKindDef struct {
	Domain    EdgeDomain // doc or code
	Weight    float64    // base weight for PPR algorithms
	Priority  int        // visualization priority (lower = more important)
	IsCodeRef bool       // true for code→code reference edges
	Clamp     int        // max edge count for weight calculation (0 = no clamp)
}

// EdgeKinds is the registry of all edge kinds and their properties.
//
// Weight rationale (for PPR diffusion):
//   - Doc edges (1.0): Human-curated links are high signal
//   - calls (0.25): Direct behavioral coupling, strongest code relationship
//   - type_ref (0.15): Structural coupling via types
//   - member_ref (0.12): Static/value symbol reads
//   - imports (0.10): Package-level dependency, often incidental
//   - tests (0.08): One-directional (test→production), lower signal for context discovery
//
// Clamp rationale: Higher clamp for calls since frequent callers have genuinely stronger coupling.
var EdgeKinds = map[string]EdgeKindDef{
	// Doc-domain edges
	"wikilink": {Domain: EdgeDomainDoc, Weight: 1.0, Priority: 1},
	"coderef":  {Domain: EdgeDomainDoc, Weight: 1.0, Priority: 2},
	"mentions": {Domain: EdgeDomainDoc, Weight: 1.0, Priority: 3},

	// Code-domain edges (weights reflect semantic coupling strength)
	"calls":      {Domain: EdgeDomainCode, Weight: 0.25, Priority: 4, IsCodeRef: true, Clamp: 5},
	"type_ref":   {Domain: EdgeDomainCode, Weight: 0.15, Priority: 5, IsCodeRef: true, Clamp: 4},
	"member_ref": {Domain: EdgeDomainCode, Weight: 0.12, Priority: 6, IsCodeRef: true, Clamp: 4},
	"imports":    {Domain: EdgeDomainCode, Weight: 0.10, Priority: 7, IsCodeRef: true, Clamp: 3},
	"tests":      {Domain: EdgeDomainCode, Weight: 0.08, Priority: 8, IsCodeRef: true, Clamp: 3},
}

// EdgeWeight returns the weight for an edge kind, applying clamp if defined.
// Unknown edge kinds return 0.
func EdgeWeight(kind string, count int) float64 {
	kind = strings.ToLower(strings.TrimSpace(kind))
	def, ok := EdgeKinds[kind]
	if !ok {
		return 0
	}
	if def.Clamp > 0 && count > def.Clamp {
		count = def.Clamp
	}
	return def.Weight * float64(count)
}

// IsCodeRefEdge returns true if the kind is a code→code reference edge.
func IsCodeRefEdge(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	def, ok := EdgeKinds[kind]
	return ok && def.IsCodeRef
}

// EdgePriority returns the priority for visualization (lower = more important).
// Unknown edge kinds return 999.
func EdgePriority(kind string) int {
	kind = strings.ToLower(strings.TrimSpace(kind))
	def, ok := EdgeKinds[kind]
	if !ok {
		return 999
	}
	return def.Priority
}

// IsDocDomainEdge returns true if the edge involves notes.
func IsDocDomainEdge(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	def, ok := EdgeKinds[kind]
	return ok && def.Domain == EdgeDomainDoc
}
