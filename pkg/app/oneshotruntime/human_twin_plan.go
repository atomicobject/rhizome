package oneshotruntime

import "github.com/atomicobject/rhizome/pkg/app/bootstrap"

// ViewPlan declares the optional persisted projection used by both view
// fronts. A missing projection preserves the existing live/read-only service
// behavior, so its unavailable policy remains degradation rather than a new
// command failure.
func ViewPlan() Plan {
	return Plan{
		Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
		// Views retain the established generic store opener, including its
		// migration behavior, so this is not an existing-only reader.
		StoreAccess:   StoreLiveReadWrite,
		NoteState:     NoteStateLive,
		OntologyState: OntologyStatePersisted,
		Session:       SessionNone,
		Unavailable:   UnavailableDegrade,
	}
}

// GraphContextPlan declares the real dependencies of the root graph-context
// service. It is intentionally distinct from the agent's indexed read-only
// file-context plan: root context reads live notes and may call its directly
// owned compressor. User-visible write authority for link-target apply belongs
// to the root command declaration, not to a fabricated SQLite session policy.
func GraphContextPlan(compressionConfigured bool) Plan {
	plan := Plan{
		NoteState:   NoteStateLive,
		Session:     SessionNone,
		Unavailable: UnavailableDegrade,
	}
	if compressionConfigured {
		plan.Capabilities = append(plan.Capabilities, bootstrap.RuntimeCapabilitySemantic)
	}
	return plan
}
