package search

import (
	"context"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
)

type TargetResolverStore interface {
	IntelAnchorsBySymbol(ctx context.Context, symbol string, limit int) ([]codeanchor.IntelAnchor, error)
	IntelAnchorsByFQNsLimited(ctx context.Context, fqns []string, limitPerFQN int) (map[string][]codeanchor.IntelAnchor, error)
	IntelNotePaths(ctx context.Context) ([]string, error)
	ListFiles(ctx context.Context, limit int) ([]string, error)
}

// ResolveQuerySpecTargets turns query-only local target phrases into explicit
// path/anchor seeds before planning.
//
// Precision modes fail closed when the target is ambiguous/unresolved; broad
// modes may downgrade with warnings. This keeps "go to definition" style calls
// from silently becoming global semantic search.
//
// Docs: [[Search - Seed expansion strategies#^search-seed-target-resolution-contract]]
func ResolveQuerySpecTargets(ctx context.Context, vaultPath string, store TargetResolverStore, spec QuerySpec) (QuerySpec, []Warning) {
	if spec.targetsResolved {
		return spec, nil
	}
	spec = normalizeTargetResolutionState(spec)
	if !intentNeedsRepair(spec.Intent) {
		spec.targetsResolved = true
		return spec, nil
	}
	if spec.HasExplicitSeeds {
		if spec.TargetStatus == TargetStatusNone {
			spec.TargetStatus = TargetStatusExplicitPath
			spec.ResolutionConfidence = 1.0
		}
		spec.targetsResolved = true
		return spec, nil
	}
	if len(spec.Seeds) > 0 && spec.TargetStatus == TargetStatusInferredPath {
		spec.targetsResolved = true
		return spec, nil
	}
	// Explicit paths retain precedence. Path-free named symbols can resolve
	// directly without loading every note/code path in the repository.
	pathsPrepared := QueryNeedsPathOwnership(spec)
	var pathMatches []string
	if pathsPrepared {
		if spec.PathKinds == nil {
			spec.PathKinds = pathKindsFromTargetResolver(ctx, store)
		}
		pathMatches = inferExactPathMatches(spec.PathKinds, spec.Text, 8)
	}
	var warnings []Warning

	preferPath := hasExplicitPathHint(spec.Text, pathMatches)
	switch {
	case preferPath && len(pathMatches) == 1:
		spec = applyResolvedPaths(spec, pathMatches, TargetStatusInferredPath, 0.88)
		spec.targetsResolved = true
		return spec, warnings
	case preferPath && len(pathMatches) > 1:
		spec.TargetCandidates = append(spec.TargetCandidates, targetCandidatesFromPaths(pathMatches, "exact_path_match")...)
	}

	if !targetResolverStoreAvailable(store) {
		spec, warnings = finalizeUnresolvedTarget(spec, warnings, true)
		spec.targetsResolved = true
		return spec, warnings
	}

	resolvedSpec, resolvedWarns, resolved := resolveFromIndexedSymbols(ctx, store, spec)
	warnings = append(warnings, resolvedWarns...)
	if resolved {
		resolvedSpec.targetsResolved = true
		return resolvedSpec, warnings
	}
	if !pathsPrepared {
		if resolvedSpec.PathKinds == nil {
			resolvedSpec.PathKinds = pathKindsFromTargetResolver(ctx, store)
		}
		pathMatches = inferExactPathMatches(resolvedSpec.PathKinds, resolvedSpec.Text, 8)
	}

	// A speculative candidate is not a target the user named, so the document and
	// path lanes still get their turn.
	unnamedTarget := len(resolvedSpec.TargetCandidates) == 0 || speculativeTargetCandidatesOnly(resolvedSpec.TargetCandidates)
	// Exact document identity is a peer of code-symbol identity: a request whose
	// whole phrase names an indexed note resolves to that note rather than to a
	// code member that merely shares one of its words.
	if unnamedTarget && IsPrecisionIntent(resolvedSpec.Intent) {
		if docPath := inferExactDocumentMatch(resolvedSpec.PathKinds, resolvedSpec.Text); docPath != "" {
			resolvedSpec = applyResolvedPaths(resolvedSpec, []string{docPath}, TargetStatusInferredPath, 0.95)
			if resolvedSpec.ResolvedTarget != nil {
				resolvedSpec.ResolvedTarget.Reason = "note_title_exact"
			}
			resolvedSpec.targetsResolved = true
			return resolvedSpec, warnings
		}
	}
	if unnamedTarget {
		switch {
		case len(pathMatches) == 1:
			resolvedSpec = applyResolvedPaths(resolvedSpec, pathMatches, TargetStatusInferredPath, 0.88)
			resolvedSpec.targetsResolved = true
			return resolvedSpec, warnings
		case len(pathMatches) > 1:
			resolvedSpec.TargetCandidates = append(resolvedSpec.TargetCandidates, targetCandidatesFromPaths(pathMatches, "exact_path_match")...)
		}
	}
	resolvedSpec, warnings = finalizeUnresolvedTarget(resolvedSpec, warnings, false)
	resolvedSpec.targetsResolved = true
	return resolvedSpec, warnings
}

func ShouldBlockPrecisionFallback(spec QuerySpec) bool {
	// Precision intents need a concrete local target. Returning no results plus
	// warnings is more truthful than broad fallback when a symbol/path cannot be
	// resolved.
	return IsPrecisionIntent(spec.Intent) &&
		(spec.TargetStatus == TargetStatusAmbiguous || spec.TargetStatus == TargetStatusUnresolved) &&
		len(spec.Seeds) == 0 &&
		!spec.HasExplicitSeeds &&
		!speculativeTargetCandidatesOnly(spec.TargetCandidates)
}

// speculativeTargetReason marks a candidate that exists only because a prose
// word in the query collides with an indexed symbol name. Such candidates are
// informational: they never seed retrieval and never block precision fallback.
const speculativeTargetReason = "speculative_symbol_match"

// speculativeTargetCandidatesOnly reports whether every candidate came from a
// word the user never spelled as code. Ambiguity that exists only between those
// guesses must not fail a precision request closed.
func speculativeTargetCandidatesOnly(candidates []TargetCandidate) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if candidate.Reason != speculativeTargetReason {
			return false
		}
	}
	return true
}

func normalizeTargetResolutionState(spec QuerySpec) QuerySpec {
	spec.TargetCandidates = dedupeTargetCandidates(spec.TargetCandidates)
	if spec.HasExplicitSeeds {
		if spec.TargetStatus == TargetStatusNone {
			spec.TargetStatus = TargetStatusExplicitPath
		}
		if spec.ResolutionConfidence <= 0 {
			spec.ResolutionConfidence = 1.0
		}
	}
	if spec.TargetStatus == TargetStatusNone && len(spec.ExplicitSeedPaths) > 0 {
		spec.TargetStatus = TargetStatusInferredPath
		if spec.ResolutionConfidence <= 0 {
			spec.ResolutionConfidence = 0.92
		}
	}
	return spec
}

func applyResolvedAnchors(spec QuerySpec, anchors []codeanchor.IntelAnchor, confidence float64, candidates *[]TargetCandidate) (QuerySpec, bool) {
	anchors = dedupeAnchors(anchors)
	if len(anchors) == 0 {
		return spec, false
	}

	paths := uniqueAnchorPaths(anchors)
	if IsPrecisionIntent(spec.Intent) && distinctAnchorTargetCount(anchors) > 1 {
		for _, anchor := range sortAmbiguousAnchors(anchors) {
			*candidates = append(*candidates, targetCandidateFromAnchor(anchor, "indexed_symbol_match"))
		}
		return spec, false
	}
	if len(paths) == 1 {
		spec.PathKinds = MergePathKinds(spec.PathKinds, codeAnchorPathKinds(paths))
		spec = applyResolvedPaths(spec, paths, TargetStatusInferredSymbol, confidence)
		selected := TargetCandidate{Path: NormalizeLocalityPath(anchors[0].Path), Symbol: strings.TrimSpace(anchors[0].Symbol), FQN: strings.TrimSpace(anchors[0].FQN), Kind: strings.TrimSpace(anchors[0].Kind), Reason: "resolved_symbol"}
		spec.ResolvedTarget = &selected
		spec.Seeds = appendDedupeHandles(spec.Seeds, knowledge.AnchorHandle(anchors[0].AnchorID))
		return spec, true
	}

	commonPrefix := dominantModulePath(paths)
	if commonPrefix != "" && !IsPrecisionIntent(spec.Intent) {
		spec.PathKinds = MergePathKinds(spec.PathKinds, codeAnchorPathKinds(paths))
		spec = applyResolvedPaths(spec, []string{commonPrefix}, TargetStatusInferredSymbol, confidence-0.05)
		for _, anchor := range anchors {
			spec.Seeds = appendDedupeHandles(spec.Seeds, knowledge.AnchorHandle(anchor.AnchorID))
		}
		return spec, true
	}

	for _, anchor := range anchors {
		*candidates = append(*candidates, targetCandidateFromAnchor(anchor, "indexed_symbol_match"))
	}
	return spec, false
}

func applyResolvedPaths(spec QuerySpec, paths []string, status TargetStatus, confidence float64) QuerySpec {
	spec.ExplicitSeedPaths = mergeNormalizedSeedPaths(spec.ExplicitSeedPaths, paths)
	spec.HasExplicitSeeds = len(spec.ExplicitSeedPaths) > 0
	var handles []knowledge.Handle
	for _, path := range paths {
		if handle := handleForExplicitSeedPath(path, pathKindForRawSeed(path, spec.PathKinds)); handle.Kind != "" {
			handles = append(handles, handle)
		}
	}
	spec.Seeds = appendDedupeHandles(spec.Seeds, handles...)
	spec.TargetStatus = status
	if len(paths) == 1 {
		selected := TargetCandidate{Path: paths[0], Reason: "resolved_path"}
		spec.ResolvedTarget = &selected
	}
	if confidence > spec.ResolutionConfidence {
		spec.ResolutionConfidence = confidence
	}
	spec.TargetCandidates = nil
	return spec
}

func finalizeUnresolvedTarget(spec QuerySpec, warnings []Warning, indexUnavailable bool) (QuerySpec, []Warning) {
	if spec.HasExplicitSeeds || len(spec.Seeds) > 0 {
		return spec, warnings
	}
	if indexUnavailable {
		warnings = append(warnings, Warning{
			Code:    "index_unavailable",
			Kind:    "index_unavailable",
			Source:  "target_resolution",
			Message: "Code intel was unavailable during target resolution; query-only target inference is incomplete.",
		})
	}
	if speculativeTargetCandidatesOnly(spec.TargetCandidates) {
		// The only "targets" are prose words that collide with symbol names.
		// Keep them as informational candidates, report low confidence, and let
		// the document and lexical lanes answer the request.
		spec.ResolvedTarget = nil
		spec.TargetStatus = TargetStatusUnresolved
		spec.ResolutionConfidence = 0.2
		return spec, append(warnings, Warning{
			Code:    "target_speculative",
			Kind:    "target_unresolved",
			Source:  "target_resolution",
			Message: "No local target was named; query words matched symbol names only incidentally, so broader retrieval ran.",
		})
	}
	if len(spec.TargetCandidates) > 0 {
		spec.ResolvedTarget = nil
		spec.TargetStatus = TargetStatusAmbiguous
		spec.ResolutionConfidence = 0.3
		warnings = append(warnings, Warning{
			Code:    "target_ambiguous",
			Kind:    "target_ambiguous",
			Source:  "target_resolution",
			Message: "Query matched multiple plausible local targets; pass --path or use a more specific symbol/module name.",
		})
	} else {
		spec.ResolvedTarget = nil
		spec.TargetStatus = TargetStatusUnresolved
		spec.ResolutionConfidence = 0
		warnings = append(warnings, Warning{
			Code:    "target_unresolved",
			Kind:    "target_unresolved",
			Source:  "target_resolution",
			Message: "Could not resolve a local target from the query text.",
		})
	}
	if IsPrecisionIntent(spec.Intent) {
		warnings = append(warnings, Warning{
			Code:    "precision_fallback_blocked",
			Kind:    "precision_fallback_blocked",
			Source:  "target_resolution",
			Message: "Blocked broad fallback for a precision mode because the defining target was not resolved.",
		})
		return spec, warnings
	}
	if spec.Intent == IntentSubsystemOverview {
		spec.Intent = IntentOverview
		spec.TargetStatus = TargetStatusDowngraded
		warnings = append(warnings, Warning{
			Code:    "subsystem_overview_downgraded",
			Kind:    "mode_downgraded",
			Source:  "target_resolution",
			Message: "No credible local target was resolved; downgraded subsystem_overview to overview.",
		})
	}
	return spec, warnings
}

func dedupeTargetCandidates(in []TargetCandidate) []TargetCandidate {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]TargetCandidate, 0, len(in))
	for _, candidate := range in {
		key := strings.Join([]string{
			NormalizeLocalityPath(candidate.Path),
			strings.TrimSpace(candidate.Symbol),
			strings.TrimSpace(candidate.FQN),
			strings.TrimSpace(candidate.Kind),
			strings.TrimSpace(candidate.Reason),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	// Callers append candidates in precedence order (declarations first, deeper
	// paths first); keep that order instead of re-sorting alphabetically.
	return out
}
