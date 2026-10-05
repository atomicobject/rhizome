package identifierreconcile

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology/reference"
)

func validateReviewDiagnosticsPreserved(components []CollisionRepairIntent, discovery *identifierLinkDiscoverySnapshot) error {
	expected := make(map[string]map[string]struct{})
	for _, plan := range discovery.ReviewPlans {
		for index, candidate := range plan.Candidates {
			diagnostic := plan.Diagnostics[index]
			physical := RepairDiagnostic{Kind: string(reference.IdentifierRewriteDiagnosticReviewOnly), Blocking: false, Field: &diagnostic}
			key := repairDiagnosticPhysicalKey(physical)
			for _, component := range components {
				if reviewCandidateMatchesRewrites(candidate, componentRewriteValues(component)) {
					addIntentMembership(expected, key, component.MembershipKeys[0])
				}
			}
		}
	}

	actual := make(map[string]map[string]struct{})
	for _, component := range components {
		for _, diagnostic := range component.Diagnostics {
			if diagnostic.Kind == string(reference.IdentifierRewriteDiagnosticReviewOnly) && !diagnostic.Blocking && diagnostic.Field != nil {
				key := repairDiagnosticPhysicalKey(diagnostic)
				if _, tracked := expected[key]; tracked {
					addIntentMembership(actual, key, component.MembershipKeys[0])
				}
			}
		}
	}
	if jsonKey(expected) != jsonKey(actual) {
		return fmt.Errorf("identifier review diagnostics were not preserved with exact collision membership")
	}
	return nil
}

func componentRewriteValues(component CollisionRepairIntent) []reference.IdentifierRewrite {
	out := make([]reference.IdentifierRewrite, len(component.Rewrites))
	for index := range component.Rewrites {
		out[index] = component.Rewrites[index].Rewrite
	}
	return out
}
