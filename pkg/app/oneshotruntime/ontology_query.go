package oneshotruntime

import "github.com/atomicobject/rhizome/pkg/app/bootstrap"

// OntologyQueryPlan declares the one-shot work needed after a query compiles.
// Current ontology projection remains authoritative, so the plan keeps the
// existing live store access while omitting background and provider work that
// cannot affect a non-semantic result. The unified search root requests the
// Semantic capability without Semantic readiness: configured embeddings are
// initialized when present, and their absence degrades instead of failing.
func OntologyQueryPlan(usesSemantic, usesSearch bool) Plan {
	plan := Plan{
		Capabilities:  []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
		Readiness:     []Readiness{ReadinessCodeIndex},
		StoreAccess:   StoreLiveReadWrite,
		OntologyState: OntologyStateCurrentProjection,
		Unavailable:   UnavailableFail,
	}
	switch {
	case usesSemantic:
		plan.Capabilities = append(plan.Capabilities, bootstrap.RuntimeCapabilitySemantic)
		plan.Readiness = append(plan.Readiness, ReadinessSemantic)
	case usesSearch:
		// The search root degrades to lexical when embeddings are absent, so it
		// initializes the optional capability without awaiting its readiness.
		plan.Capabilities = append(plan.Capabilities, bootstrap.RuntimeCapabilitySemantic)
	}
	return plan
}
