package actions

type IndexedContextTargetKind string

const (
	IndexedContextTargetFile      IndexedContextTargetKind = "file"
	IndexedContextTargetDirectory IndexedContextTargetKind = "directory"
)

type IndexedContextState string

const (
	IndexedContextAvailable    IndexedContextState = "available"
	IndexedContextStale        IndexedContextState = "stale"
	IndexedContextIncompatible IndexedContextState = "incompatible"
	IndexedContextMissing      IndexedContextState = "missing"
)

type IndexedContextFreshness struct {
	State       IndexedContextState `json:"state"`
	WarningCode string              `json:"warningCode,omitempty"`
	Remediation string              `json:"remediation,omitempty"`
}

type IndexedContextWarning struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// IndexedContextFreshnessEvidence is populated from existing migration-open,
// index_metadata, and ontology materialization evidence. Evaluation is
// intentionally pure: startup reads state and recommends index ownership; it
// never repairs state. WHY: persisted timestamps are observability, not proof
// of live-worktree freshness; SPEC-0082.US10-US11 require deterministic
// durable-proof degradation without synchronous discovery.
type IndexedContextFreshnessEvidence struct {
	SchemaCompatible bool

	HasIndexerVersion bool
	IndexerVersion    string
	ExpectedVersion   string

	HasScopeHash      bool
	ScopeHash         string
	ExpectedScopeHash string

	RequireOntology         bool
	HasOntologyState        bool
	OntologyReady           bool
	OntologyVersion         int
	ExpectedOntologyVersion int
}

func EvaluateIndexedContextFreshness(e IndexedContextFreshnessEvidence) IndexedContextFreshness {
	if !e.SchemaCompatible {
		remediation := "rzm index --rebuild"
		if e.RequireOntology {
			remediation = "rzm index --rebuild"
		}
		return unavailableIndexedContext(IndexedContextIncompatible, "indexed-context-incompatible", remediation)
	}
	if !e.HasIndexerVersion || !e.HasScopeHash {
		return unavailableIndexedContext(IndexedContextMissing, "indexed-context-missing", "rzm index")
	}
	if e.IndexerVersion != e.ExpectedVersion || e.ScopeHash != e.ExpectedScopeHash {
		return unavailableIndexedContext(IndexedContextStale, "indexed-context-stale", "rzm index")
	}
	if e.RequireOntology {
		if !e.HasOntologyState {
			return unavailableIndexedContext(IndexedContextMissing, "indexed-context-missing", "rzm index")
		}
		if !e.OntologyReady || e.OntologyVersion != e.ExpectedOntologyVersion {
			return unavailableIndexedContext(IndexedContextStale, "indexed-context-stale", "rzm index")
		}
	}
	return IndexedContextFreshness{State: IndexedContextAvailable}
}

func unavailableIndexedContext(state IndexedContextState, code, remediation string) IndexedContextFreshness {
	return IndexedContextFreshness{
		State:       state,
		WarningCode: code,
		Remediation: remediation,
	}
}
