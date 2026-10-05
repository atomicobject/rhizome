package mcp

import (
	"os"
	"sort"
	"strings"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

func collectAnchorIDs(groups []semanticQueryGroup) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, g := range groups {
		for _, h := range g.hits {
			id := strings.TrimSpace(h.AnchorID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func buildSemanticOptions(g semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor, vaultPath string, rank, total, budget int, intent semanticQueryIntent, docs docMatcher) []semanticContentOption {
	header := semanticHeader(rank, g.match, g.score)
	headerLen := len(header)
	fullCost := estimateFullCost(g.match, vaultPath, budget, docs)
	excerptCost := estimateExcerptCost(g, anchors)
	outlineCost := estimateOutlineCost(g, anchors)
	signatureCost := estimateSignatureCost(g, anchors)
	stubCost := headerLen + 80

	options := []semanticContentOption{
		makeOption(planFull, headerLen, fullCost, g, rank, total, intent, docs),
		makeOption(planExcerpt, headerLen, excerptCost, g, rank, total, intent, docs),
		makeOption(planOutline, headerLen, outlineCost, g, rank, total, intent, docs),
		makeOption(planSignature, headerLen, signatureCost, g, rank, total, intent, docs),
		makeOption(planStub, headerLen, stubCost, g, rank, total, intent, docs),
	}

	filtered := options[:0]
	for _, opt := range options {
		if opt.cost <= 0 {
			continue
		}
		filtered = append(filtered, opt)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].ratio != filtered[j].ratio {
			return filtered[i].ratio > filtered[j].ratio
		}
		return filtered[i].cost > filtered[j].cost
	})
	return filtered
}

func makeOption(plan contentPlan, headerLen, bodyCost int, g semanticQueryGroup, rank, total int, intent semanticQueryIntent, docs docMatcher) semanticContentOption {
	if bodyCost <= 0 {
		return semanticContentOption{plan: plan}
	}
	cost := headerLen + bodyCost
	utility := planUtility(plan, g, rank, total, intent, docs)
	ratio := 0.0
	if cost > 0 {
		ratio = utility / float64(cost)
	}
	return semanticContentOption{
		plan:    plan,
		cost:    cost,
		utility: utility,
		ratio:   ratio,
	}
}

func planUtility(plan contentPlan, g semanticQueryGroup, rank, total int, intent semanticQueryIntent, docs docMatcher) float64 {
	base := g.score
	if base <= 0 {
		base = 0.001
	}
	mult := 1.0
	switch plan {
	case planFull:
		mult = 1.5
	case planExcerpt:
		mult = 1.2
	case planOutline:
		mult = 1.0
	case planSignature:
		mult = 0.8
	case planStub:
		mult = 0.4
	}
	if intent.Fetch {
		switch plan {
		case planFull:
			mult *= 0.65
		case planExcerpt:
			mult *= 0.8
		case planOutline:
			mult *= 1.15
		case planSignature:
			mult *= 1.35
		case planStub:
			mult *= 1.2
		}
	}
	spreadFactor := 1.0 + 0.1*float64(min(max(g.spread-1, 0), 3))
	diversityFactor := 1.0 + 0.05*float64(min(max(g.diversity-1, 0), 3))
	if plan == planFull && g.spread < 2 {
		mult *= 0.85
	}
	if intent.Overview && docs.IsDoc(g.match.Path, g.match.Type) {
		mult *= 1.12
	}
	if intent.Overview && docs.IsDoc(g.match.Path, g.match.Type) {
		switch plan {
		case planFull:
			mult *= 1.35
		case planExcerpt:
			mult *= 1.25
		case planOutline:
			mult *= 1.15
		case planSignature:
			mult *= 1.1
		}
	}
	if docs.IsDoc(g.match.Path, g.match.Type) && (!intent.Fetch || intent.PreferDocs) {
		switch plan {
		case planFull:
			mult *= 1.18
		case planExcerpt:
			mult *= 1.12
		}
		if docs.IsPrimaryDoc(g.match.Path) {
			mult *= 1.2
		}
	}
	if isTestPath(g.match.Path) && !intent.Tests {
		mult *= 0.7
	}
	return base * mult * spreadFactor * diversityFactor * presentationRankWeight(rank, total)
}

func adjustOptionRatio(opt semanticContentOption, g semanticQueryGroup, summaryKey, groupKey string, ctx semanticSelectionContext, intent semanticQueryIntent, docs docMatcher) float64 {
	if opt.cost <= 0 {
		return 0
	}
	utility := opt.utility
	if docs.IsDoc(g.match.Path, g.match.Type) && (!intent.Fetch || intent.PreferDocs) {
		count := ctx.selectedGroups[groupKey]
		if count == 0 {
			utility *= 1.25
		} else {
			utility *= 0.9
		}
	}
	if summaryKey != "" && len(ctx.selectedSummaries) > 0 {
		maxSim := 0.0
		for _, other := range ctx.selectedSummaries {
			if other == summaryKey {
				maxSim = 1.0
				break
			}
			sim := textSimilarity(summaryKey, other)
			if sim > maxSim {
				maxSim = sim
			}
		}
		if maxSim > 0 {
			utility *= 1.0 - 0.45*maxSim
		}
	}
	if intent.Overview && docs.IsDoc(g.match.Path, g.match.Type) {
		utility *= 1.08
	}
	if intent.PreferDocs {
		if docs.IsDoc(g.match.Path, g.match.Type) {
			utility *= 1.12
		}
	}
	if intent.PreferCode {
		if g.match.Type == "code" {
			utility *= 1.12
		}
	}
	return utility / float64(opt.cost)
}

func candidateSummaryKey(g semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) string {
	if g.match.Type == "note" {
		return normalizeClusterText(firstNonEmpty(g.match.Heading, g.match.Title, g.match.Path))
	}
	if g.match.Type != "code" {
		return normalizeClusterText(firstNonEmpty(g.match.Title, g.match.Path))
	}
	if sig := anchorSignatureKey(g, anchors); sig != "" {
		return normalizeClusterText(sig)
	}
	return normalizeClusterText(firstNonEmpty(g.match.Symbol, g.match.FQN, g.match.Title, g.match.Path))
}

func anchorSignatureKey(g semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) string {
	for _, h := range g.hits {
		if h.AnchorID == "" {
			continue
		}
		a, ok := anchors[h.AnchorID]
		if !ok {
			continue
		}
		if sig := strings.TrimSpace(a.Signature); sig != "" {
			return sig
		}
	}
	return ""
}

func textSimilarity(a, b string) float64 {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return 0
	}
	setA := tokenSet(a, 20)
	setB := tokenSet(b, 20)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	inter := 0
	for tok := range setA {
		if _, ok := setB[tok]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenSet(text string, limit int) map[string]struct{} {
	set := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		if len(set) >= limit {
			b.Reset()
			return
		}
		set[b.String()] = struct{}{}
		b.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return set
}

func estimateFullCost(match SemanticMatchPayload, vaultPath string, budget int, docs docMatcher) int {
	full, ok := safeJoinVaultPath(vaultPath, match.Path)
	if !ok {
		return 1200
	}
	if fi, err := os.Stat(full); err == nil {
		if fi.Size() <= 0 {
			return 1200
		}
		size := int(fi.Size())
		if docs.IsDoc(match.Path, match.Type) {
			cap := docFullCostCap(budget)
			if size > cap {
				size = cap
			}
		}
		return size
	}
	return 1200
}

func docFullCostCap(budget int) int {
	if budget <= 0 {
		return 6000
	}
	cap := budget / 4
	if cap < 2000 {
		cap = 2000
	}
	if cap > 12000 {
		cap = 12000
	}
	return cap
}

func estimateExcerptCost(g semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) int {
	if g.match.Type == "note" {
		return 1000
	}
	if g.match.Type != "code" {
		return 800
	}
	total := 0
	added := 0
	for _, h := range g.hits {
		if h.AnchorID == "" {
			continue
		}
		a, ok := anchors[h.AnchorID]
		if !ok {
			continue
		}
		span := int(a.EndByte - a.StartByte)
		if span <= 0 {
			span = len(a.Signature) + len(a.DocComment)
		}
		if span <= 0 {
			span = 600
		}
		total += span
		added++
		if added >= 2 {
			break
		}
	}
	if total == 0 {
		total = 900
	}
	return total
}

func estimateSignatureCost(g semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) int {
	if g.match.Type != "code" {
		return 200
	}
	for _, h := range g.hits {
		if h.AnchorID == "" {
			continue
		}
		a, ok := anchors[h.AnchorID]
		if !ok {
			continue
		}
		size := len(a.Signature) + len(a.DocComment)
		if size > 0 {
			return size + 40
		}
	}
	return 260
}

func estimateOutlineCost(g semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) int {
	if g.match.Type != "code" {
		return 0
	}
	entries := codeOutlineEntries(g, anchors, maxCodeOutlineEntries)
	if len(entries) == 0 {
		return 0
	}
	total := 24
	for _, entry := range entries {
		total += len(entry.signature) + len(entry.doc) + 18
	}
	return total
}

func clusterKeyFromBody(body string, match SemanticMatchPayload) string {
	body = strings.TrimSpace(body)
	if len(body) < 60 {
		return ""
	}
	preview := trimPreview(body, 400)
	normalized := normalizeClusterText(preview)
	if len(normalized) < 60 {
		return ""
	}
	return match.Type + ":" + normalized
}

func normalizeClusterText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	lastSpace := false
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if lastSpace {
			continue
		}
		b.WriteRune(' ')
		lastSpace = true
	}
	return strings.TrimSpace(b.String())
}

func planCostForOption(opts []semanticContentOption, plan contentPlan) int {
	for _, opt := range opts {
		if opt.plan == plan {
			return opt.cost
		}
	}
	return 0
}
