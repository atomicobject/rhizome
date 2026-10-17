package planner

import (
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
)

// targetRetrievers selects exact navigation and relationship-specific lanes.
func (p *Planner) targetRetrievers(spec search.QuerySpec) []search.Retriever {
	var retrieversOut []search.Retriever
	intent := spec.Intent

	switch intent {
	case search.IntentGoToDef:
		if p.Deps.IntelStore != nil {
			retrieversOut = append(retrieversOut, &retrieval.DefinitionRetriever{
				Store:     p.Deps.IntelStore,
				VaultPath: p.Deps.VaultPath,
				Limit:     spec.Limits.Total,
			})
		}
	case search.IntentTestsForCode:
		retrieversOut = append(retrieversOut, &retrieval.TestsForCodeRetriever{
			Store:     p.Deps.IntelStore,
			VaultPath: p.Deps.VaultPath,
			Limit:     spec.Limits.Total,
		})
	case search.IntentImplementers:
		if p.Deps.IntelStore != nil {
			retrieversOut = append(retrieversOut, &retrieval.ImplementersRetriever{
				Store: p.Deps.IntelStore,
				Limit: spec.Limits.Total,
			})
		}
	case search.IntentRefactorImpact:
		if p.Deps.IntelStore != nil {
			// Impact is target-shaped evidence collection: callers describe inbound
			// blast radius, while callees describe dependencies that constrain edits.
			retrieversOut = append(retrieversOut,
				&retrieval.CallEdgesRetriever{
					Store:        p.Deps.IntelStore,
					Mode:         retrieval.CallEdgesCallers,
					Limit:        spec.Limits.Total,
					PerSeedLimit: 8,
				},
				&retrieval.CallEdgesRetriever{
					Store:        p.Deps.IntelStore,
					Mode:         retrieval.CallEdgesCallees,
					Limit:        spec.Limits.Total,
					PerSeedLimit: 8,
				},
			)
		}
		retrieversOut = append(retrieversOut, &retrieval.TestsForCodeRetriever{
			Store:     p.Deps.IntelStore,
			VaultPath: p.Deps.VaultPath,
			Limit:     spec.Limits.Total,
		})
	case search.IntentFindUsages, search.IntentCallers, search.IntentCallees:
		if p.Deps.IntelStore != nil {
			mode := retrieval.CallEdgesCallers
			if intent == search.IntentCallees {
				mode = retrieval.CallEdgesCallees
			}
			retrieversOut = append(retrieversOut, &retrieval.CallEdgesRetriever{
				Store:        p.Deps.IntelStore,
				Mode:         mode,
				Limit:        spec.Limits.Total,
				PerSeedLimit: 8,
			})
		}
	}
	return retrieversOut
}
