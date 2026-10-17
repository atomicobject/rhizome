package identifierreconcile

import (
	identifierrewrite "github.com/atomicobject/rhizome/pkg/validate/identifierreconcile/rewrite"
)

func appendLinkDiscovery(component *CollisionRepairIntent, rewrites []identifierrewrite.IdentifierRewrite, discovery *identifierLinkDiscoverySnapshot) error {
	for _, plan := range discovery.Plans {
		for _, edit := range plan.Edits {
			if _, ok := linkEditRewrite(edit, rewrites); !ok {
				continue
			}
			component.LinkEdits = append(component.LinkEdits, LinkRepairIntent{MembershipKeys: append([]string(nil), component.MembershipKeys...), Edit: edit})
		}
		for _, raw := range plan.Diagnostics {
			if !linkDiagnosticMatchesRewrites(raw, rewrites) {
				continue
			}
			diagnostic := raw
			blocking := diagnostic.Blocking || diagnostic.Kind == identifierrewrite.LinkRewriteDiagnosticUnresolvedTarget || diagnostic.Kind == identifierrewrite.LinkRewriteDiagnosticAmbiguousTarget
			component.Diagnostics = append(component.Diagnostics, RepairDiagnostic{MembershipKeys: append([]string(nil), component.MembershipKeys...), Kind: string(diagnostic.Kind), Blocking: blocking, Link: &diagnostic})
		}
	}
	for _, plan := range discovery.ReviewPlans {
		for index, candidate := range plan.Candidates {
			if !reviewCandidateMatchesRewrites(candidate, rewrites) {
				continue
			}
			diagnostic := plan.Diagnostics[index]
			component.Diagnostics = append(component.Diagnostics, RepairDiagnostic{
				MembershipKeys: append([]string(nil), component.MembershipKeys...),
				Kind:           string(identifierrewrite.IdentifierRewriteDiagnosticReviewOnly),
				Blocking:       false,
				Field:          &diagnostic,
			})
		}
	}
	return nil
}

func reviewCandidateMatchesRewrites(candidate identifierrewrite.IdentifierReviewCandidate, rewrites []identifierrewrite.IdentifierRewrite) bool {
	componentKeys := make(map[string]struct{}, len(rewrites))
	for _, rewrite := range rewrites {
		componentKeys[jsonKey(rewrite)] = struct{}{}
	}
	candidateRewrites, err := canonicalRepairRewriteSet(candidate.Rewrites)
	if err != nil {
		return false
	}
	for _, rewrite := range candidateRewrites {
		if _, found := componentKeys[jsonKey(rewrite)]; found {
			return true
		}
	}
	return false
}
