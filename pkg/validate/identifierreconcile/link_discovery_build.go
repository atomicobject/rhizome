package identifierreconcile

import (
	"context"
	"fmt"
	"sort"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// IdentifierLinkDiscoveryRequest supplies live sources and the matching
// sealed field inventory. Callers cannot inject paths, scans, resolutions, or
// authored ranges.
type IdentifierLinkDiscoveryRequest struct {
	FieldDiscovery *IdentifierFieldDiscovery
}

type identifierLinkDiscoveryDependencies struct {
	scan func(string) obsidian.StructuredLinkScanSnapshot
}

type identifierLinkSourceWork struct {
	source      notemeta.NoteSourceSnapshot
	scan        obsidian.StructuredLinkScanSnapshot
	resolutions []reference.StructuredLinkResolution
}

// DiscoverIdentifierLinks derives complete automatic-link and review-only
// plans from every current Markdown note. Empty scans remain represented by a
// sealed per-note plan, so completeness never depends on rescanning content.
func DiscoverIdentifierLinks(ctx context.Context, req IdentifierLinkDiscoveryRequest) (*IdentifierLinkDiscovery, error) {
	return discoverIdentifierLinks(ctx, req, identifierLinkDiscoveryDependencies{scan: obsidian.ScanStructuredLinkSnapshot})
}

func discoverIdentifierLinks(ctx context.Context, req IdentifierLinkDiscoveryRequest, deps identifierLinkDiscoveryDependencies) (*IdentifierLinkDiscovery, error) {
	if deps.scan == nil {
		return nil, fmt.Errorf("structured link scanner is required")
	}
	if req.FieldDiscovery == nil || req.FieldDiscovery.source == nil || req.FieldDiscovery.source.schema == nil {
		return nil, fmt.Errorf("production identifier field discovery is required")
	}
	canonicalRewrites := cloneIdentifierRewrites(req.FieldDiscovery.rewrites)
	rewriteFingerprint, err := repairRewriteSetFingerprint(canonicalRewrites)
	if err != nil {
		return nil, err
	}
	fieldSnapshot, err := req.FieldDiscovery.validatedSnapshotFor(canonicalRewrites)
	if err != nil {
		return nil, err
	}
	if fieldSnapshot.SchemaHash != req.FieldDiscovery.source.schema.Hash {
		return nil, fmt.Errorf("identifier field discovery does not match compiled schema")
	}
	sources := req.FieldDiscovery.source.sources
	preconditions := append([]SourcePrecondition(nil), fieldSnapshot.SourcePreconditions...)
	projections := req.FieldDiscovery.source.projections
	if !sameSourcePreconditions(preconditions, fieldSnapshot.SourcePreconditions) {
		return nil, fmt.Errorf("identifier link and field discoveries do not share one complete source snapshot")
	}
	inventory, err := newIdentifierLinkResolutionInventory(sources, projections, fieldSnapshot.Occurrences, canonicalRewrites)
	if err != nil {
		return nil, err
	}

	work := make([]identifierLinkSourceWork, len(sources))
	for index := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		work[index].source = sources[index]
		work[index].scan = deps.scan(sources[index].Content)
		if snapshot, scanErr := work[index].scan.ValidatedSnapshot(); scanErr != nil || snapshot.SourceFingerprint != sources[index].ContentHash {
			return nil, fmt.Errorf("scan structured links for %s: sealed source mismatch", sources[index].Path)
		}
		work[index].resolutions = inventory.resolutionsFor(sources[index].Path.String(), work[index].scan.Links)
	}
	provenanceDecisions, err := applyAmbiguousLinkProvenance(ctx, req.FieldDiscovery.source.vaultDef.BasePath(), work, inventory, AmbiguousLinkGitResolver{schema: req.FieldDiscovery.source.schema}.Resolve)
	if err != nil {
		return nil, err
	}

	handledRanges := handledFieldRangesByNote(fieldSnapshot.Occurrences)
	plans := make([]reference.StructuredLinkRewritePlan, 0, len(work))
	reviewPlans := make([]reference.IdentifierReviewCandidatePlan, 0, len(work))
	for index := range work {
		notePath := work[index].source.Path.String()
		plan := reference.PlanStructuredLinkRewrites(reference.StructuredLinkRewriteInput{
			NotePath: notePath, Content: work[index].source.Content, LinkSnapshot: &work[index].scan,
			Rewrites: canonicalRewrites, Resolutions: work[index].resolutions,
		})
		if _, err := plan.ValidatedSnapshot(); err != nil {
			return nil, fmt.Errorf("seal structured link plan for %s: %w", notePath, err)
		}
		plans = append(plans, plan)
		review := reference.PlanIdentifierReviewCandidates(reference.IdentifierReviewCandidateInput{
			NotePath: notePath, Content: work[index].source.Content, LinkSnapshot: &work[index].scan,
			Rewrites: canonicalRewrites, HandledFieldRanges: handledRanges[notePath],
		})
		if _, err := review.ValidatedSnapshot(); err != nil {
			return nil, fmt.Errorf("seal identifier review plan for %s: %w", notePath, err)
		}
		reviewPlans = append(reviewPlans, review)
	}
	return sealIdentifierLinkDiscovery(preconditions, req.FieldDiscovery.source.schema.Hash, rewriteFingerprint, plans, reviewPlans, provenanceDecisions)
}

func sameSourcePreconditions(left, right []SourcePrecondition) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func handledFieldRangesByNote(occurrences []reference.StructuredFieldOccurrence) map[string][]ontology.ByteRange {
	out := make(map[string][]ontology.ByteRange)
	for _, occurrence := range occurrences {
		if occurrence.Range.Len() == 0 {
			continue
		}
		out[occurrence.OwnerRef.NotePath] = append(out[occurrence.OwnerRef.NotePath], occurrence.Range)
	}
	for notePath := range out {
		sort.Slice(out[notePath], func(i, j int) bool {
			if out[notePath][i].Start != out[notePath][j].Start {
				return out[notePath][i].Start < out[notePath][j].Start
			}
			return out[notePath][i].End < out[notePath][j].End
		})
	}
	return out
}

func sealIdentifierLinkDiscovery(sources []SourcePrecondition, schemaHash, rewriteFingerprint string, plans []reference.StructuredLinkRewritePlan, reviewPlans []reference.IdentifierReviewCandidatePlan, provenanceDecisions []AmbiguousLinkProvenanceDecision) (*IdentifierLinkDiscovery, error) {
	snapshot := identifierLinkDiscoverySnapshot{
		SourceFingerprint: sourcePreconditionFingerprint(sources), SourcePreconditions: append([]SourcePrecondition(nil), sources...),
		SchemaHash: schemaHash, RewriteFingerprint: rewriteFingerprint,
		Plans: append([]reference.StructuredLinkRewritePlan(nil), plans...), ReviewPlans: append([]reference.IdentifierReviewCandidatePlan(nil), reviewPlans...),
		ProvenanceDecisions: append([]AmbiguousLinkProvenanceDecision(nil), provenanceDecisions...),
	}
	sort.Slice(snapshot.Plans, func(i, j int) bool { return snapshot.Plans[i].NotePath < snapshot.Plans[j].NotePath })
	sort.Slice(snapshot.ReviewPlans, func(i, j int) bool { return snapshot.ReviewPlans[i].NotePath < snapshot.ReviewPlans[j].NotePath })
	sort.Slice(snapshot.ProvenanceDecisions, func(i, j int) bool {
		if snapshot.ProvenanceDecisions[i].NotePath != snapshot.ProvenanceDecisions[j].NotePath {
			return snapshot.ProvenanceDecisions[i].NotePath < snapshot.ProvenanceDecisions[j].NotePath
		}
		return snapshot.ProvenanceDecisions[i].LinkIndex < snapshot.ProvenanceDecisions[j].LinkIndex
	})
	sealed, err := identifierLinkDiscoveryFingerprint(snapshot)
	if err != nil {
		return nil, err
	}
	return &IdentifierLinkDiscovery{
		sourceFingerprint: snapshot.SourceFingerprint, sourcePreconditions: snapshot.SourcePreconditions,
		schemaHash: snapshot.SchemaHash, rewriteFingerprint: snapshot.RewriteFingerprint,
		plans: snapshot.Plans, reviewPlans: snapshot.ReviewPlans, provenanceDecisions: snapshot.ProvenanceDecisions, sealed: sealed,
	}, nil
}
