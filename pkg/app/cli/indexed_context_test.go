package actions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateIndexedContextFreshness(t *testing.T) {
	tests := []struct {
		name string
		in   IndexedContextFreshnessEvidence
		want IndexedContextFreshness
	}{
		{
			name: "available",
			in: IndexedContextFreshnessEvidence{
				SchemaCompatible:  true,
				HasIndexerVersion: true,
				IndexerVersion:    "v2",
				ExpectedVersion:   "v2",
				HasScopeHash:      true,
				ScopeHash:         "scope",
				ExpectedScopeHash: "scope",
			},
			want: IndexedContextFreshness{State: IndexedContextAvailable},
		},
		{
			name: "missing metadata",
			in: IndexedContextFreshnessEvidence{
				SchemaCompatible: true,
				ExpectedVersion:  "v2",
			},
			want: IndexedContextFreshness{
				State:       IndexedContextMissing,
				WarningCode: "indexed-context-missing",
				Remediation: "rzm index",
			},
		},
		{
			name: "incompatible schema wins",
			in: IndexedContextFreshnessEvidence{
				SchemaCompatible: false,
				ExpectedVersion:  "v2",
			},
			want: IndexedContextFreshness{
				State:       IndexedContextIncompatible,
				WarningCode: "indexed-context-incompatible",
				Remediation: "rzm index --rebuild",
			},
		},
		{
			name: "old indexer version is stale",
			in: IndexedContextFreshnessEvidence{
				SchemaCompatible:  true,
				HasIndexerVersion: true,
				IndexerVersion:    "v1",
				ExpectedVersion:   "v2",
				HasScopeHash:      true,
				ScopeHash:         "scope",
				ExpectedScopeHash: "scope",
			},
			want: IndexedContextFreshness{
				State:       IndexedContextStale,
				WarningCode: "indexed-context-stale",
				Remediation: "rzm index",
			},
		},
		{
			name: "ontology materialization mismatch uses full index remediation",
			in: IndexedContextFreshnessEvidence{
				SchemaCompatible:        true,
				HasIndexerVersion:       true,
				IndexerVersion:          "v2",
				ExpectedVersion:         "v2",
				HasScopeHash:            true,
				ScopeHash:               "scope",
				ExpectedScopeHash:       "scope",
				RequireOntology:         true,
				HasOntologyState:        true,
				OntologyReady:           true,
				OntologyVersion:         2,
				ExpectedOntologyVersion: 3,
			},
			want: IndexedContextFreshness{
				State:       IndexedContextStale,
				WarningCode: "indexed-context-stale",
				Remediation: "rzm index",
			},
		},
		{
			name: "missing required ontology state",
			in: IndexedContextFreshnessEvidence{
				SchemaCompatible:  true,
				HasIndexerVersion: true,
				IndexerVersion:    "v2",
				ExpectedVersion:   "v2",
				HasScopeHash:      true,
				ScopeHash:         "scope",
				ExpectedScopeHash: "scope",
				RequireOntology:   true,
			},
			want: IndexedContextFreshness{
				State:       IndexedContextMissing,
				WarningCode: "indexed-context-missing",
				Remediation: "rzm index",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, EvaluateIndexedContextFreshness(tt.in))
		})
	}
}
