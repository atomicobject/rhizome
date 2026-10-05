package search

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"unicode"

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
	if spec.PathKinds == nil {
		spec.PathKinds = pathKindsFromTargetResolver(ctx, store)
	}

	var warnings []Warning
	pathMatches := inferExactPathMatches(spec.PathKinds, spec.Text, 8)
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

func resolveFromIndexedSymbols(ctx context.Context, store TargetResolverStore, spec QuerySpec) (QuerySpec, []Warning, bool) {
	fqns, symbols := extractTargetSymbolTokens(spec.Text)
	language := targetLanguageQualifier(spec.Text)
	candidates := make([]TargetCandidate, 0, 8)

	if len(fqns) > 0 {
		found, err := store.IntelAnchorsByFQNsLimited(ctx, fqns, 4)
		if err == nil {
			anchors := filterTargetAnchors(spec.Intent, language, flattenAnchorMatches(found, fqns))
			if resolvedSpec, ok := applyResolvedAnchors(spec, anchors, 0.93, &candidates); ok {
				return resolvedSpec, nil, true
			}
			if len(anchors) > 0 {
				spec.TargetCandidates = dedupeTargetCandidates(append(spec.TargetCandidates, candidates...))
				return spec, nil, false
			}
		}
	}

	meaningfulWords := len(extractTargetWordTokens(spec.Text))
	var speculative []TargetCandidate
	for i := len(symbols) - 1; i >= 0; i-- {
		sym := symbols[i]
		anchors, err := store.IntelAnchorsBySymbol(ctx, sym, 8)
		if err != nil {
			continue
		}
		anchors = resolveReExportTargets(ctx, store, anchors)
		anchors = filterTargetAnchors(spec.Intent, language, anchors)
		anchors = filterQualifiedMemberAnchors(sym, fqns, anchors)
		if !symbolIsExplicitlyQualified(sym, fqns) {
			anchors = preferDeclarationAnchors(anchors)
		}
		anchors = filterAnchorsByQueryContext(spec.Text, sym, anchors)
		if len(anchors) == 0 {
			continue
		}
		if IsPrecisionIntent(spec.Intent) && !targetTokenNamesCode(spec.Text, sym, fqns, meaningfulWords) {
			speculative = appendSpeculativeCandidates(speculative, anchors)
			continue
		}
		if resolvedSpec, ok := applyResolvedAnchors(spec, anchors, 0.82, &candidates); ok {
			return resolvedSpec, nil, true
		}
		break
	}

	if len(candidates) == 0 {
		candidates = speculative
	}
	spec.TargetCandidates = dedupeTargetCandidates(append(spec.TargetCandidates, candidates...))
	return spec, nil, false
}

const maxSpeculativeTargetCandidates = 8

func appendSpeculativeCandidates(out []TargetCandidate, anchors []codeanchor.IntelAnchor) []TargetCandidate {
	for _, anchor := range sortAmbiguousAnchors(dedupeAnchors(anchors)) {
		if len(out) >= maxSpeculativeTargetCandidates {
			break
		}
		out = append(out, targetCandidateFromAnchor(anchor, speculativeTargetReason))
	}
	return out
}

// targetTokenNamesCode reports whether the query names tok the way a user names
// code: the request is nothing but the identifier, the token is qualified or
// carries an internal capital, the user quoted it, or a code cue word sits next
// to it. A bare lowercase prose word that merely collides with a member name
// fails this test and may only list candidates.
func targetTokenNamesCode(query, tok string, fqns []string, meaningfulWords int) bool {
	if meaningfulWords <= 1 {
		return true
	}
	if strings.ContainsAny(tok, "._:/\\") || symbolIsExplicitlyQualified(tok, fqns) {
		return true
	}
	if strings.ToLower(tok) != tok {
		return true
	}
	if explicitlyQuotedTarget(query, tok) {
		return true
	}
	return targetTokenHasCodeCue(query, tok)
}

func targetTokenHasCodeCue(query, tok string) bool {
	fields := strings.Fields(query)
	for index, field := range fields {
		if !strings.EqualFold(trimTargetFieldPunctuation(field), tok) {
			continue
		}
		for _, neighbor := range []int{index - 1, index + 1} {
			if neighbor < 0 || neighbor >= len(fields) {
				continue
			}
			if _, ok := targetResolutionCodeCues[strings.ToLower(trimTargetFieldPunctuation(fields[neighbor]))]; ok {
				return true
			}
		}
	}
	return false
}

func trimTargetFieldPunctuation(field string) string {
	return strings.TrimSuffix(strings.Trim(field, "`'\"()[]{}<>,:;!?"), ".")
}

const reExportSignaturePrefix = "re-export from "

func resolveReExportTargets(ctx context.Context, store TargetResolverStore, anchors []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	if len(anchors) == 0 {
		return anchors
	}
	fqns := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		if fqn := strings.TrimSpace(strings.TrimPrefix(anchor.Signature, reExportSignaturePrefix)); fqn != "" && fqn != anchor.Signature {
			fqns = append(fqns, fqn)
		}
	}
	if len(fqns) == 0 {
		return anchors
	}
	found, err := store.IntelAnchorsByFQNsLimited(ctx, dedupeStrings(fqns), 2)
	if err != nil {
		return anchors
	}
	out := make([]codeanchor.IntelAnchor, 0, len(anchors))
	for _, anchor := range anchors {
		fqn := strings.TrimSpace(strings.TrimPrefix(anchor.Signature, reExportSignaturePrefix))
		if fqn != "" && fqn != anchor.Signature {
			if targets := dedupeAnchors(found[fqn]); len(targets) == 1 {
				out = append(out, targets[0])
				continue
			}
		}
		out = append(out, anchor)
	}
	return dedupeAnchors(out)
}

func filterAnchorsByQueryContext(query, targetSymbol string, anchors []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	if len(anchors) < 2 {
		return anchors
	}
	targetLower := strings.ToLower(strings.TrimSpace(targetSymbol))
	var qualifiers []string
	for _, token := range extractTargetWordTokens(query) {
		if token == targetLower || strings.Contains(targetLower, token) {
			continue
		}
		qualifiers = append(qualifiers, token)
	}
	if len(qualifiers) == 0 {
		return anchors
	}
	best := 0
	scores := make([]int, len(anchors))
	for index, anchor := range anchors {
		haystack := strings.ToLower(strings.Join([]string{anchor.Path, anchor.FQN, anchor.Signature}, " "))
		for _, qualifier := range qualifiers {
			if strings.Contains(haystack, qualifier) {
				scores[index]++
			}
		}
		if scores[index] > best {
			best = scores[index]
		}
	}
	if best == 0 {
		return anchors
	}
	filtered := make([]codeanchor.IntelAnchor, 0, len(anchors))
	for index, anchor := range anchors {
		if scores[index] == best {
			filtered = append(filtered, anchor)
		}
	}
	if len(filtered) == 1 {
		return filtered
	}
	return anchors
}

func filterQualifiedMemberAnchors(symbol string, qualifiedTokens []string, anchors []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	var qualifications [][]string
	for _, token := range qualifiedTokens {
		parts := targetIdentityParts(token)
		if len(parts) >= 2 && strings.EqualFold(parts[len(parts)-1], symbol) {
			qualifications = append(qualifications, parts)
		}
	}
	if len(qualifications) == 0 {
		return anchors
	}
	filtered := anchors[:0]
	for _, anchor := range anchors {
		parts := targetIdentityParts(anchor.FQN)
		for _, qualification := range qualifications {
			if targetIdentityHasSuffix(parts, qualification, anchor.Lang == codeanchor.LangPhp) {
				filtered = append(filtered, anchor)
				break
			}
		}
	}
	return filtered
}

// symbolIsExplicitlyQualified reports whether the query spelled the symbol as
// `Qualifier.Symbol`, the same test filterQualifiedMemberAnchors narrows on. A
// user who typed the qualifier wants that member, not the declaration.
func symbolIsExplicitlyQualified(symbol string, qualifiedTokens []string) bool {
	for _, token := range qualifiedTokens {
		parts := targetIdentityParts(token)
		if len(parts) >= 2 && strings.EqualFold(parts[len(parts)-1], symbol) {
			return true
		}
	}
	return false
}

func targetIdentityParts(value string) []string {
	return strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		switch r {
		case '.', '/', '\\', ':':
			return true
		default:
			return false
		}
	})
}

func targetIdentityHasSuffix(value, suffix []string, caseInsensitive bool) bool {
	if len(suffix) == 0 || len(value) < len(suffix) {
		return false
	}
	offset := len(value) - len(suffix)
	for index := range suffix {
		matches := value[offset+index] == suffix[index]
		if caseInsensitive {
			matches = strings.EqualFold(value[offset+index], suffix[index])
		}
		if !matches {
			return false
		}
	}
	return true
}

func hasExplicitPathHint(query string, pathMatches []string) bool {
	fields := strings.Fields(strings.TrimSpace(query))
	for _, field := range fields {
		token := strings.Trim(field, "`'\"()[]{}<>,:;!?")
		token = strings.TrimSuffix(token, ".")
		if token == "" {
			continue
		}
		if strings.ContainsAny(token, `/\\`) {
			return true
		}
		extension := filepath.Ext(token)
		if extension == "" || extension != strings.ToLower(extension) {
			continue
		}
		for _, match := range pathMatches {
			if strings.EqualFold(filepath.Base(match), token) {
				return true
			}
		}
	}
	return false
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
		spec = applyResolvedPaths(spec, paths, TargetStatusInferredSymbol, confidence)
		selected := TargetCandidate{Path: NormalizeLocalityPath(anchors[0].Path), Symbol: strings.TrimSpace(anchors[0].Symbol), FQN: strings.TrimSpace(anchors[0].FQN), Kind: strings.TrimSpace(anchors[0].Kind), Reason: "resolved_symbol"}
		spec.ResolvedTarget = &selected
		spec.Seeds = appendDedupeHandles(spec.Seeds, knowledge.AnchorHandle(anchors[0].AnchorID))
		return spec, true
	}

	commonPrefix := dominantModulePath(paths)
	if commonPrefix != "" && !IsPrecisionIntent(spec.Intent) {
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

func distinctAnchorTargetCount(anchors []codeanchor.IntelAnchor) int {
	identities := map[string]struct{}{}
	for _, anchor := range anchors {
		identity := strings.TrimSpace(anchor.FQN)
		if identity == "" {
			identity = strings.Join([]string{NormalizeLocalityPath(anchor.Path), strings.TrimSpace(anchor.Symbol), strings.TrimSpace(anchor.Kind)}, "\x00")
		}
		identities[identity] = struct{}{}
	}
	return len(identities)
}

func targetCandidateFromAnchor(anchor codeanchor.IntelAnchor, reason string) TargetCandidate {
	return TargetCandidate{Path: NormalizeLocalityPath(anchor.Path), Symbol: strings.TrimSpace(anchor.Symbol), FQN: strings.TrimSpace(anchor.FQN), Kind: strings.TrimSpace(anchor.Kind), Reason: reason}
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

func inferExactPathMatches(pathKinds map[string]PathKind, text string, limit int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	tokens := extractTargetWordTokens(text)
	if len(tokens) == 0 {
		return nil
	}
	return inferPathKindMatches(pathKinds, tokens, limit)
}

func pathKindsFromTargetResolver(ctx context.Context, store TargetResolverStore) map[string]PathKind {
	if !targetResolverStoreAvailable(store) {
		return nil
	}
	out := make(map[string]PathKind)
	if notePaths, err := store.IntelNotePaths(ctx); err == nil {
		for _, path := range notePaths {
			if path = NormalizeLocalityPath(path); path != "" {
				out[path] = PathKindNote
			}
		}
	}
	if filePaths, err := store.ListFiles(ctx, 100000); err == nil {
		for _, path := range filePaths {
			if path = NormalizeLocalityPath(path); path != "" {
				if _, isNote := out[path]; !isNote {
					out[path] = PathKindCode
				}
			}
		}
	}
	return out
}

func inferPathKindMatches(pathKinds map[string]PathKind, tokens []string, limit int) []string {
	if len(pathKinds) == 0 || len(tokens) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	var paths []string
	for path, kind := range pathKinds {
		if kind != PathKindNote && kind != PathKindCode {
			continue
		}
		addIndexedPathCandidate(path, &paths, seen)
	}
	if len(paths) == 0 {
		return nil
	}

	exact := make([]string, 0, limit)
	matched := make([]string, 0, limit)
	for _, path := range paths {
		if indexedPathExactlyMatchesTokens(path, tokens) {
			exact = append(exact, path)
		} else if indexedPathMatchesTokens(path, tokens) {
			matched = append(matched, path)
		}
	}
	if len(exact) > 0 {
		matched = exact
	}
	matched = collapseDescendantPathMatches(matched)
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched
}

func indexedPathExactlyMatchesTokens(path string, tokens []string) bool {
	path = strings.ToLower(NormalizeLocalityPath(path))
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	full := strings.ToLower(filepath.Base(path))
	for _, token := range tokens {
		if token == path || token == base || token == full {
			return true
		}
	}
	return false
}

// inferExactDocumentMatch returns the one indexed note whose title/slug equals
// the query phrase. The token-based path lane compares single words to whole
// path segments, so a multi-word document name can only match here.
func inferExactDocumentMatch(pathKinds map[string]PathKind, text string) string {
	phrase := normalizeDocumentPhrase(text)
	if len(pathKinds) == 0 || !strings.Contains(phrase, " ") {
		return ""
	}
	var match string
	for path, kind := range pathKinds {
		if kind != PathKindNote || !documentPathMatchesPhrase(path, phrase) {
			continue
		}
		if normalized := NormalizeLocalityPath(path); normalized != match {
			if match != "" {
				return ""
			}
			match = normalized
		}
	}
	return match
}

func documentPathMatchesPhrase(path, phrase string) bool {
	path = NormalizeLocalityPath(path)
	if normalizeDocumentPhrase(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))) == phrase {
		return true
	}
	for _, segment := range strings.Split(path, "/") {
		if normalizeDocumentPhrase(segment) == phrase {
			return true
		}
	}
	return false
}

// normalizeDocumentPhrase reduces a slug, path segment, or typed request to its
// meaningful words so "search-quality-corpus" and "the search quality corpus"
// compare equal.
func normalizeDocumentPhrase(value string) string {
	separated := strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', '/', '\\', ':':
			return ' '
		default:
			return r
		}
	}, value)
	return strings.Join(extractTargetWordTokens(separated), " ")
}

func targetResolverStoreAvailable(store TargetResolverStore) bool {
	if store == nil {
		return false
	}
	value := reflect.ValueOf(store)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func collapseDescendantPathMatches(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	slices.Sort(paths)
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		isDescendant := false
		for _, parent := range out {
			if strings.HasPrefix(path, parent+"/") {
				isDescendant = true
				break
			}
		}
		if !isDescendant {
			out = append(out, path)
		}
	}
	return out
}

func addIndexedPathCandidate(path string, out *[]string, seen map[string]struct{}) {
	path = NormalizeLocalityPath(path)
	if path == "" {
		return
	}
	appendUniqueIndexedPath(path, out, seen)
	for dir := NormalizeLocalityPath(filepath.Dir(path)); dir != "" && dir != "."; dir = NormalizeLocalityPath(filepath.Dir(dir)) {
		appendUniqueIndexedPath(dir, out, seen)
	}
}

func appendUniqueIndexedPath(path string, out *[]string, seen map[string]struct{}) {
	if _, ok := seen[path]; ok {
		return
	}
	seen[path] = struct{}{}
	*out = append(*out, path)
}

func indexedPathMatchesTokens(path string, tokens []string) bool {
	path = strings.ToLower(NormalizeLocalityPath(path))
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	full := strings.ToLower(filepath.Base(path))
	for _, token := range tokens {
		if token == path || token == base || token == full || strings.Contains(path, "/"+token) || strings.HasSuffix(path, "/"+token) {
			return true
		}
	}
	return false
}

func extractTargetSymbolTokens(query string) (fqns []string, symbols []string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	addToken := func(tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return
		}
		lower := strings.ToLower(tok)
		if _, ok := targetResolutionStopwords[lower]; ok && tok == lower {
			return
		}
		if len(tok) < 2 && tok == lower {
			return
		}
		if strings.ContainsAny(tok, "./:\\") {
			fqns = append(fqns, tok)
			if split := strings.LastIndexAny(tok, ".:/\\"); split >= 0 && split+1 < len(tok) {
				symbols = append(symbols, tok[split+1:])
			}
			return
		}
		symbols = append(symbols, tok)
	}

	var buf []rune
	flush := func() {
		if len(buf) == 0 {
			return
		}
		addToken(string(buf))
		buf = buf[:0]
	}
	for _, r := range query {
		if isTargetSymbolRune(r) {
			buf = append(buf, r)
			continue
		}
		flush()
	}
	flush()
	fqns, symbols = dedupeStrings(fqns), dedupeStrings(symbols)
	if len(fqns)+len(symbols) > 1 {
		filtered := symbols[:0]
		for _, symbol := range symbols {
			if _, qualifier := targetResolutionQualifiers[strings.ToLower(symbol)]; qualifier && !explicitlyQuotedTarget(query, symbol) {
				continue
			}
			filtered = append(filtered, symbol)
		}
		symbols = filtered
	}
	if len(symbols) > 1 {
		ordered := make([]string, 0, len(symbols))
		for _, symbol := range symbols {
			if !explicitlyQuotedTarget(query, symbol) {
				ordered = append(ordered, symbol)
			}
		}
		for _, symbol := range symbols {
			if explicitlyQuotedTarget(query, symbol) {
				ordered = append(ordered, symbol)
			}
		}
		symbols = ordered
	}
	return fqns, symbols
}

func explicitlyQuotedTarget(query, target string) bool {
	for _, quote := range []string{"`", `'`, `"`} {
		if strings.Contains(query, quote+target+quote) {
			return true
		}
	}
	return false
}

func targetLanguageQualifier(query string) codeanchor.Lang {
	qualifiers := ExplicitLanguageQualifiers(query)
	if len(qualifiers) == 0 {
		return ""
	}
	return qualifiers[len(qualifiers)-1]
}

// ExplicitLanguageQualifiers returns programming languages that constrain a
// query. A language-named symbol remains a target when it is the only target or
// is explicitly quoted.
func ExplicitLanguageQualifiers(query string) []codeanchor.Lang {
	var qualifiers []codeanchor.Lang
	hasOtherTarget := false
	fields := strings.Fields(query)
	for index, field := range fields {
		token := strings.Trim(field, "`'\"()[]{}<>,:;!?")
		token = strings.TrimSuffix(token, ".")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if _, stopword := targetResolutionStopwords[lower]; stopword {
			continue
		}
		if lower == "go" && index+1 < len(fields) && strings.EqualFold(strings.Trim(fields[index+1], "`'\"()[]{}<>,:;!?"), "to") {
			continue
		}
		if lang, ok := targetResolutionLanguageQualifiers[lower]; ok && !explicitlyQuotedTarget(query, token) {
			if lower == "go" && !goTokenIsLanguageQualifier(fields, index, token) {
				hasOtherTarget = true
				continue
			}
			if !slices.Contains(qualifiers, lang) {
				qualifiers = append(qualifiers, lang)
			}
			continue
		}
		hasOtherTarget = true
	}
	if !hasOtherTarget {
		return nil
	}
	return qualifiers
}

func goTokenIsLanguageQualifier(fields []string, index int, token string) bool {
	if index >= 0 && index < len(fields) && strings.HasSuffix(strings.TrimSpace(fields[index]), ".") {
		return false
	}
	neighbor := func(index int) string {
		if index < 0 || index >= len(fields) {
			return ""
		}
		return strings.ToLower(strings.Trim(fields[index], "`'\"()[]{}<>,:;!?"))
	}
	if previous := neighbor(index - 1); previous == "in" || previous == "of" || previous == "using" || previous == "written" {
		return true
	}
	switch neighbor(index + 1) {
	case "code", "implementation", "module", "package", "function", "method", "service", "type", "interface":
		return true
	default:
		if token != "Go" || index+1 >= len(fields) {
			return false
		}
		next := strings.Trim(fields[index+1], "`'\"()[]{}<>,:;!?")
		return codeShapedLanguageContext(next)
	}
}

func codeShapedLanguageContext(value string) bool {
	if strings.ContainsAny(value, ".:_") {
		return true
	}
	for index, r := range []rune(value) {
		if index > 0 && (unicode.IsUpper(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

func filterTargetAnchors(intent Intent, language codeanchor.Lang, anchors []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	if language != "" {
		filtered := anchors[:0]
		for _, anchor := range anchors {
			if anchor.Lang == language {
				filtered = append(filtered, anchor)
			}
		}
		anchors = filtered
	}
	if intent != IntentCallers && intent != IntentCallees && intent != IntentTestsForCode {
		return anchors
	}
	callable := make([]codeanchor.IntelAnchor, 0, len(anchors))
	for _, anchor := range anchors {
		switch strings.ToLower(strings.TrimSpace(anchor.Kind)) {
		case "func", "function", "method", "constructor":
			callable = append(callable, anchor)
		}
	}
	if intent == IntentTestsForCode && len(callable) == 0 {
		return anchors
	}
	return callable
}

func extractTargetWordTokens(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, field := range fields {
		field = strings.Trim(field, "`'\"()[]{}<>,:;!?")
		field = strings.TrimSuffix(field, ".")
		if field == "" {
			continue
		}
		if _, ok := targetResolutionStopwords[field]; ok {
			continue
		}
		if _, ok := targetResolutionQualifiers[field]; ok {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func isTargetSymbolRune(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r):
		return true
	case r == '_', r == '.', r == '/', r == ':', r == '\\':
		return true
	default:
		return false
	}
}

func flattenAnchorMatches(found map[string][]codeanchor.IntelAnchor, orderedKeys []string) []codeanchor.IntelAnchor {
	var out []codeanchor.IntelAnchor
	for _, key := range orderedKeys {
		out = append(out, found[key]...)
	}
	return dedupeAnchors(out)
}

func dedupeAnchors(in []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]codeanchor.IntelAnchor, 0, len(in))
	for _, anchor := range in {
		key := strings.TrimSpace(anchor.AnchorID)
		if key == "" {
			key = strings.TrimSpace(anchor.Path) + "|" + strings.TrimSpace(anchor.FQN) + "|" + strings.TrimSpace(anchor.Symbol)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, anchor)
	}
	return out
}

func uniqueAnchorPaths(anchors []codeanchor.IntelAnchor) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, anchor := range anchors {
		path := NormalizeLocalityPath(anchor.Path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	slices.Sort(out)
	return out
}

func dominantModulePath(paths []string) string {
	if len(paths) < 2 {
		return ""
	}
	prefix := NormalizeLocalityPath(filepath.Dir(paths[0]))
	for _, path := range paths[1:] {
		prefix = longestCommonPathPrefix(prefix, NormalizeLocalityPath(filepath.Dir(path)))
		if prefix == "" {
			return ""
		}
	}
	if prefix == "" {
		return ""
	}
	segments := strings.Split(prefix, "/")
	if len(segments) < 2 && (segments[0] == "pkg" || segments[0] == "cmd" || segments[0] == "docs" || segments[0] == "web") {
		return ""
	}
	return prefix
}

func longestCommonPathPrefix(a, b string) string {
	a = NormalizeLocalityPath(a)
	b = NormalizeLocalityPath(b)
	if a == "" || b == "" {
		return ""
	}
	partsA := strings.Split(a, "/")
	partsB := strings.Split(b, "/")
	n := min(len(partsA), len(partsB))
	var shared []string
	for i := 0; i < n; i++ {
		if partsA[i] != partsB[i] {
			break
		}
		shared = append(shared, partsA[i])
	}
	return strings.Join(shared, "/")
}

func targetCandidatesFromPaths(paths []string, reason string) []TargetCandidate {
	out := make([]TargetCandidate, 0, len(paths))
	for _, path := range paths {
		out = append(out, TargetCandidate{Path: NormalizeLocalityPath(path), Reason: reason})
	}
	return out
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

var targetResolutionStopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "to": {}, "for": {}, "of": {}, "this": {},
	"that": {}, "these": {}, "those": {}, "find": {}, "show": {}, "list": {}, "locate": {}, "where": {},
	"who": {}, "what": {}, "how": {}, "explain": {}, "definition": {}, "definitions": {}, "usage": {},
	"usages": {}, "callers": {}, "callee": {}, "callees": {}, "calls": {}, "tests": {}, "testing": {},
	"docs": {}, "doc": {}, "overview": {}, "subsystem": {}, "in": {},
}

// targetResolutionCodeCues mark an adjacent token as code even when the token
// itself is spelled like an ordinary word ("the func retry").
var targetResolutionCodeCues = map[string]struct{}{
	"function": {}, "func": {}, "method": {}, "type": {}, "struct": {}, "class": {},
	"interface": {}, "symbol": {}, "def": {}, "fn": {},
}

var targetResolutionQualifiers = map[string]struct{}{
	"python": {}, "go": {}, "golang": {}, "typescript": {}, "javascript": {}, "csharp": {}, "php": {},
}

var targetResolutionLanguageQualifiers = map[string]codeanchor.Lang{
	"python": codeanchor.LangPy, "go": codeanchor.LangGo, "golang": codeanchor.LangGo,
	"typescript": codeanchor.LangTS, "javascript": codeanchor.LangTS,
	"csharp": codeanchor.LangCs, "c#": codeanchor.LangCs, "php": codeanchor.LangPhp,
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// Anchor kind buckets for declaration precedence. Values match the strings
// emitted by intelKindFromSymbol in pkg/anchors/intel_extract_code.go
// ("function", "method", "interface", "class", "field", "type", "module"),
// plus the closest equivalents other indexers may emit.
var (
	declarationAnchorKinds = map[string]struct{}{
		"func": {}, "function": {}, "method": {}, "constructor": {},
		"type": {}, "class": {}, "interface": {}, "struct": {}, "enum": {}, "trait": {},
	}
	moduleAnchorKinds = map[string]struct{}{
		"module": {}, "file": {}, "package": {}, "namespace": {},
	}
	memberAnchorKinds = map[string]struct{}{
		"field": {}, "property": {}, "variable": {}, "const": {}, "constant": {}, "parameter": {},
	}
)

// preferDeclarationAnchors keeps only declaration-kind anchors when any exist.
// A bare symbol query means the declaration, not a same-named struct field.
func preferDeclarationAnchors(anchors []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	declarations := make([]codeanchor.IntelAnchor, 0, len(anchors))
	for _, anchor := range anchors {
		if _, ok := declarationAnchorKinds[normalizedAnchorKind(anchor)]; ok {
			declarations = append(declarations, anchor)
		}
	}
	if len(declarations) == 0 {
		return anchors
	}
	return declarations
}

func normalizedAnchorKind(anchor codeanchor.IntelAnchor) string {
	return strings.ToLower(strings.TrimSpace(anchor.Kind))
}

func anchorKindRank(anchor codeanchor.IntelAnchor) int {
	kind := normalizedAnchorKind(anchor)
	switch {
	case mapHasKey(declarationAnchorKinds, kind):
		return 0
	case mapHasKey(moduleAnchorKinds, kind):
		return 1
	case mapHasKey(memberAnchorKinds, kind):
		return 2
	default:
		return 3
	}
}

func mapHasKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// sortAmbiguousAnchors orders ambiguous precision candidates deterministically:
// declarations first, then deeper paths (a wrapper usually sits shallower than
// the definition it wraps), then FQN.
func sortAmbiguousAnchors(anchors []codeanchor.IntelAnchor) []codeanchor.IntelAnchor {
	out := append([]codeanchor.IntelAnchor(nil), anchors...)
	slices.SortStableFunc(out, func(a, b codeanchor.IntelAnchor) int {
		if diff := anchorKindRank(a) - anchorKindRank(b); diff != 0 {
			return diff
		}
		if diff := anchorPathDepth(b) - anchorPathDepth(a); diff != 0 {
			return diff
		}
		return strings.Compare(strings.TrimSpace(a.FQN), strings.TrimSpace(b.FQN))
	})
	return out
}

func anchorPathDepth(anchor codeanchor.IntelAnchor) int {
	path := NormalizeLocalityPath(anchor.Path)
	if path == "" {
		return 0
	}
	return strings.Count(path, "/") + 1
}
