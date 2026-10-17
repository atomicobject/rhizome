package search

import (
	"context"
	"slices"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

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
