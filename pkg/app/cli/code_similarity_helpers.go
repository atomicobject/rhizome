package actions

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func buildCodeSimilarityRow(source, match codeanchor.IntelAnchor, embeddingScore float64, sourceRefs, matchRefs, sourceAncestors, matchAncestors map[string]struct{}, typeLike bool) CodeSimilarityRow {
	sharedRefs := sharedSetItems(sourceRefs, matchRefs, 8)
	sharedAncestors := sharedSetItems(sourceAncestors, matchAncestors, 4)
	sourceTokens := anchorTokens(source)
	matchTokens := anchorTokens(match)
	tokenOverlap := jaccard(sourceTokens, matchTokens)
	pathLocality := pathSimilarity(source.Path, match.Path)

	sharedSignals := append([]string(nil), sharedRefs...)
	for _, ancestor := range sharedAncestors {
		sharedSignals = append(sharedSignals, "ancestor:"+ancestor)
	}
	sharedSignals = stableUniqueStrings(sharedSignals)

	reasons := make([]string, 0, 5)
	if embeddingScore >= 0.80 {
		reasons = append(reasons, "high embedding similarity")
	} else if embeddingScore >= 0.65 {
		reasons = append(reasons, "moderate embedding similarity")
	}
	if len(sharedRefs) > 0 {
		reasons = append(reasons, "shared code references")
	}
	if len(sharedAncestors) > 0 {
		reasons = append(reasons, "shared type ancestry")
	}
	if tokenOverlap >= 0.20 {
		reasons = append(reasons, "symbol/signature token overlap")
	}
	if pathLocality >= 0.40 {
		reasons = append(reasons, "nearby path")
	}

	score := embeddingScore*0.70 + tokenOverlap*0.12 + pathLocality*0.08
	if len(sharedRefs) > 0 {
		weight := 0.04 * float64(len(sharedRefs))
		if typeLike {
			weight = 0.06 * float64(len(sharedRefs))
		}
		score += minFloat(weight, 0.16)
	}
	if len(sharedAncestors) > 0 {
		score += minFloat(0.08*float64(len(sharedAncestors)), 0.16)
	}
	if strings.EqualFold(source.Kind, match.Kind) {
		score += 0.03
	}
	return CodeSimilarityRow{
		Source:         codeSimilarityAnchor(source),
		Match:          codeSimilarityAnchor(match),
		EmbeddingScore: roundScore(embeddingScore),
		CombinedScore:  roundScore(score),
		SharedSignals:  sharedSignals,
		Reasons:        stableUniqueStrings(reasons),
	}
}

func collectSimilaritySignals(ctx context.Context, intel CodeSimilarityIntel, anchors map[string]codeanchor.IntelAnchor) (map[string]map[string]struct{}, map[string]map[string]struct{}) {
	pathsByFQN := make(map[string]string, len(anchors))
	var pathList []string
	for _, anchor := range anchors {
		if strings.TrimSpace(anchor.FQN) == "" || strings.TrimSpace(anchor.Path) == "" {
			continue
		}
		pathsByFQN[anchor.FQN] = anchor.Path
		pathList = append(pathList, anchor.Path)
	}
	pathList = stableUniqueStrings(pathList)

	refsByFQN := make(map[string]map[string]struct{}, len(anchors))
	if len(pathList) > 0 {
		refsByPath, err := intel.SymbolRefsByPaths(ctx, pathList)
		if err == nil {
			for _, rows := range refsByPath {
				for _, row := range rows {
					owner := strings.TrimSpace(row.OwnerFQN)
					if owner == "" {
						continue
					}
					if _, ok := pathsByFQN[owner]; !ok {
						continue
					}
					addSignal(refsByFQN, owner, signalFromRef(row))
				}
			}
		}
	}
	ancestorsByFQN := make(map[string]map[string]struct{}, len(anchors))
	for _, anchor := range anchors {
		fqn := strings.TrimSpace(anchor.FQN)
		if fqn == "" {
			continue
		}
		ancestors, err := intel.Ancestors(ctx, fqn)
		if err != nil {
			continue
		}
		for _, ancestor := range ancestors {
			addSignal(ancestorsByFQN, fqn, strings.TrimSpace(ancestor))
		}
	}
	return refsByFQN, ancestorsByFQN
}

func primaryChunksByOwner(chunks []codeanchor.IntelChunk, preferred []string) map[string]codeanchor.IntelChunk {
	preferredRank := make(map[string]int, len(preferred))
	for i, g := range preferred {
		preferredRank[strings.ToLower(g)] = i
	}
	out := make(map[string]codeanchor.IntelChunk)
	ranks := make(map[string]int)
	for _, chunk := range chunks {
		if chunk.OwnerType != codeSimilarityOwnerType {
			continue
		}
		rank, ok := preferredRank[strings.ToLower(strings.TrimSpace(chunk.Granularity))]
		if !ok {
			continue
		}
		if existingRank, exists := ranks[chunk.OwnerID]; exists && existingRank <= rank {
			continue
		}
		out[chunk.OwnerID] = chunk
		ranks[chunk.OwnerID] = rank
	}
	return out
}

func sourceAnchorIDsForRoots(metas []codeanchor.IntelAnchorMeta, roots []string) []string {
	var out []string
	for _, meta := range metas {
		if strings.TrimSpace(meta.AnchorID) == "" || !isSimilarityKind(meta.Kind) {
			continue
		}
		if pathUnderAnyRoot(meta.Path, roots) {
			out = append(out, meta.AnchorID)
		}
	}
	return stableUniqueStrings(out)
}

func filterSourceIDsWithAnchors(ids []string, anchors map[string]codeanchor.IntelAnchor) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := anchors[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

func normalizeSimilarityRoots(roots []string) []string {
	if len(roots) == 0 {
		return []string{"."}
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		root = filepath.ToSlash(filepath.Clean(string(paths.NormalizeCode(root))))
		if root == "" {
			continue
		}
		out = append(out, root)
	}
	if len(out) == 0 {
		return []string{"."}
	}
	return stableUniqueStrings(out)
}

func pathUnderAnyRoot(path string, roots []string) bool {
	path = filepath.ToSlash(filepath.Clean(string(paths.NormalizeCode(path))))
	if path == "" {
		return false
	}
	for _, root := range roots {
		root = filepath.ToSlash(filepath.Clean(root))
		root = strings.TrimSuffix(root, "/")
		if root == "." || root == "/" || root == "" {
			return true
		}
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func kindIn(kind string, allowed []string) bool {
	kind = strings.ToLower(strings.TrimSpace(kind))
	for _, item := range allowed {
		if kind == item {
			return true
		}
	}
	return false
}

func isSimilarityKind(kind string) bool {
	return kindIn(kind, functionSimilarityKinds) || kindIn(kind, typeSimilarityKinds)
}

func signalFromRef(row codeanchor.SymbolRefRow) string {
	refKind := strings.TrimSpace(string(row.RefKind))
	if refKind == "" {
		refKind = "ref"
	}
	target := strings.TrimSpace(row.DstFQN)
	if target == "" {
		target = codeanchor.Symbol{Lang: row.DstLang, Pkg: row.DstPkg, Name: row.DstName}.NormalizeFQN()
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	return refKind + ":" + target
}

func addSignal(dst map[string]map[string]struct{}, owner, signal string) {
	owner = strings.TrimSpace(owner)
	signal = strings.TrimSpace(signal)
	if owner == "" || signal == "" {
		return
	}
	if dst[owner] == nil {
		dst[owner] = make(map[string]struct{})
	}
	dst[owner][signal] = struct{}{}
}

func mergeSignalMaps(dst, src map[string]map[string]struct{}) {
	for owner, signals := range src {
		for signal := range signals {
			addSignal(dst, owner, signal)
		}
	}
}

func sharedSetItems(a, b map[string]struct{}, limit int) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	var out []string
	for item := range a {
		if _, ok := b[item]; ok {
			out = append(out, item)
		}
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func anchorTokens(anchor codeanchor.IntelAnchor) map[string]struct{} {
	text := strings.Join([]string{anchor.Symbol, anchor.FQN, anchor.Signature, anchor.DocComment}, " ")
	tokens := make(map[string]struct{})
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		token := strings.ToLower(b.String())
		b.Reset()
		if len(token) < 3 {
			return
		}
		tokens[token] = struct{}{}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersections := 0
	for item := range a {
		if _, ok := b[item]; ok {
			intersections++
		}
	}
	union := len(a) + len(b) - intersections
	if union == 0 {
		return 0
	}
	return float64(intersections) / float64(union)
}

func pathSimilarity(left, right string) float64 {
	leftParts := pathParts(left)
	rightParts := pathParts(right)
	if len(leftParts) == 0 || len(rightParts) == 0 {
		return 0
	}
	return jaccard(sliceSet(leftParts), sliceSet(rightParts))
}

func pathParts(path string) []string {
	path = filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		out = append(out, strings.ToLower(part))
	}
	return out
}

func sliceSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item != "" {
			out[item] = struct{}{}
		}
	}
	return out
}

func codeSimilarityAnchor(anchor codeanchor.IntelAnchor) CodeSimilarityAnchor {
	return CodeSimilarityAnchor{
		AnchorID:  anchor.AnchorID,
		Lang:      string(anchor.Lang),
		Kind:      anchor.Kind,
		Symbol:    anchor.Symbol,
		FQN:       anchor.FQN,
		Path:      anchor.Path,
		StartLine: anchor.StartLine,
		EndLine:   anchor.EndLine,
	}
}

func stableUniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.Join(strings.Fields(strings.TrimSpace(item)), " ")
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func roundScore(v float64) float64 {
	return float64(int(v*10000+0.5)) / 10000
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func codeSimilarityPairKey(left, right string) string {
	if left > right {
		left, right = right, left
	}
	return left + "\x00" + right
}
