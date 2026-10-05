// Package answer turns ranked search results into agent-ready answer packets.
//
// It does not retrieve or rerank candidates. It selects task-shaped evidence
// roles, reports coverage/confidence gaps, and suggests follow-up queries.
// Docs: docs/reference/analysis/Search - Answer engine packets.md and [[answer-packet-diagnostics-contract]].
package answer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

type Item struct {
	Type    string  `json:"type"`
	Path    string  `json:"path,omitempty"`
	Title   string  `json:"title,omitempty"`
	Role    string  `json:"role,omitempty"`
	Symbol  string  `json:"symbol,omitempty"`
	FQN     string  `json:"fqn,omitempty"`
	Line    int     `json:"line,omitempty"`
	Score   float64 `json:"score,omitempty"`
	Preview string  `json:"preview,omitempty"`
	Why     string  `json:"why,omitempty"`

	Specificity       float64      `json:"specificity,omitempty"`
	DirectSpecificity float64      `json:"directSpecificity,omitempty"`
	NodeRef           *NodeRef     `json:"nodeRef,omitempty"`
	LinkTarget        *LinkTarget  `json:"linkTarget,omitempty"`
	SupportingOnly    bool         `json:"-"`
	SupportedRoles    []string     `json:"-"`
	Relevance         string       `json:"-"`
	Eligibility       string       `json:"-"`
	Relationship      string       `json:"-"`
	FacetProofs       []FacetProof `json:"-"`
	IdentityMatch     bool         `json:"-"`
}

type CoverageReport struct {
	Docs      bool     `json:"docs,omitempty"`
	Code      bool     `json:"code,omitempty"`
	Tests     bool     `json:"tests,omitempty"`
	Decisions bool     `json:"decisions,omitempty"`
	Examples  bool     `json:"examples,omitempty"`
	Missing   []string `json:"missing,omitempty"`
}

type ConfidenceReport struct {
	Level          string   `json:"level,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	MissingSignals []string `json:"missingSignals,omitempty"`
}

type SuggestedQuery struct {
	Mode   string   `json:"mode"`
	Query  string   `json:"query,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

type Response struct {
	Summary     string           `json:"summary,omitempty"`
	MustRead    []Item           `json:"mustRead,omitempty"`
	Supporting  []Item           `json:"supporting,omitempty"`
	Coverage    CoverageReport   `json:"coverage,omitempty"`
	Confidence  ConfidenceReport `json:"confidence,omitempty"`
	NextQueries []SuggestedQuery `json:"nextQueries,omitempty"`
}

// NodeRef is the answer-layer view of a navigable ontology node. It mirrors the
// public semantic-query JSON shape without coupling answer assembly to ontology internals.
// Answer assembly treats it as provenance only; ontology lookup/link repair must
// happen before Build is called.
type NodeRef struct {
	NotePath              string `json:"notePath"`
	Fragment              string `json:"fragment,omitempty"`
	NodeID                string `json:"nodeId,omitempty"`
	TypeName              string `json:"typeName,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	StartByte             int    `json:"startByte,omitempty"`
	EndByte               int    `json:"endByte,omitempty"`
	ParentID              string `json:"parentId,omitempty"`
	StructuralFingerprint string `json:"structuralFingerprint,omitempty"`
}

// LinkTarget is the answer-layer view of a durable ontology node link.
// It is already-resolved linkability metadata, not a request for answer.Build to
// inspect markdown or mutate block IDs.
type LinkTarget struct {
	Markdown     string `json:"markdown,omitempty"`
	Wikilink     string `json:"wikilink,omitempty"`
	DisplayLabel string `json:"displayLabel,omitempty"`
	Exists       bool   `json:"exists"`
	RequiresFix  bool   `json:"requiresFix,omitempty"`
	BlockID      string `json:"blockId,omitempty"`
}

type Input struct {
	Type    string
	Path    string
	Title   string
	Role    string
	Symbol  string
	FQN     string
	Line    int
	Score   float64
	Preview string

	Specificity       float64
	DirectSpecificity float64
	Granularity       string
	NodeRef           *NodeRef
	LinkTarget        *LinkTarget
	// SupportingOnly keeps useful context in the packet without presenting it
	// as mandatory evidence for the requested task.
	SupportingOnly bool
	// Assessment fields are supplied by the application boundary. When
	// Relevance is present, answer selection uses these source-bound facts
	// rather than reconstructing proof from path or role heuristics.
	SupportedRoles []string
	Relevance      string
	Eligibility    string
	Relationship   string
	FacetProofs    []FacetProof
	// IdentityMatch marks a source the query named outright (exact path,
	// note title, symbol, or definition anchor).
	IdentityMatch bool
}

type FacetProof struct {
	Key               string
	Mode              search.Intent
	Strong            bool
	Eligibility       string
	Relationship      string
	TargetEstablished bool
}

func Build(intent search.Intent, query string, targetStatus search.TargetStatus, warnings []search.Warning, inputs []Input) Response {
	return build(intent, query, targetStatus, warnings, inputs, nil)
}

// BuildForFacets selects mandatory evidence against the exact requested facet
// keys. Facet proof is supplied by the application assessment boundary; this
// package remains a pure selector over those facts.
func BuildForFacets(intent search.Intent, query string, targetStatus search.TargetStatus, warnings []search.Warning, inputs []Input, requiredFacetKeys []string) Response {
	return build(intent, query, targetStatus, warnings, inputs, requiredFacetKeys)
}

func build(intent search.Intent, query string, targetStatus search.TargetStatus, warnings []search.Warning, inputs []Input, requiredFacetKeys []string) Response {
	// Docs: [[search-answer-workflow#^spec-0034-us1-ac1]], [[unified-search-answer-architecture#^spec-0035-us2-ac1]], and [[answer-packet-diagnostics-contract#^spec-0050-us1-ac3]].
	// IMPORTANT: this is pure answer shaping over already-ranked evidence; upstream callers own retrieval and ontology reads.
	frame := queryframe.Extract(query)
	items := make([]Item, 0, len(inputs))
	for _, in := range inputs {
		item := classifyItem(in)
		if item.DirectSpecificity <= 0 {
			title := item.Title
			item.DirectSpecificity = queryframe.ScoreFields(frame, queryframe.Fields{
				Path:   item.Path,
				Title:  title,
				Symbol: item.Symbol,
				FQN:    item.FQN,
			}).Value
		}
		if item.Specificity <= 0 {
			item.Specificity = item.DirectSpecificity
		}
		items = append(items, item)
	}

	// Docs: [[unified-search-answer-architecture#^spec-0035-us2-ac3]].
	provenanceAware := hasAssessmentProvenance(items)
	var mustRead, supporting []Item
	if provenanceAware {
		mustRead, supporting = selectAssessedItems(intent, frame, items, requiredFacetKeys)
	} else {
		mustRead, supporting = selectItems(intent, frame, items)
	}
	coverageItems := items
	requiredCoverageRoles := desiredRoles(intent, frame)
	if provenanceAware {
		coverageItems = mustRead
		requiredCoverageRoles = requiredRolesForAssessedPacket(intent, frame)
	}
	coverage := buildCoverageForRoles(intent, frame, coverageItems, requiredCoverageRoles)
	if provenanceAware {
		coverage.Missing = appendAssessedMissing(intent, mustRead, requiredFacetKeys, coverage.Missing)
		coverage.Missing = appendRelationshipTruncation(intent, items, mustRead, requiredFacetKeys, coverage.Missing)
	}
	confidence := buildConfidence(targetStatus, warnings, mustRead, coverage)
	nextQueries := buildNextQueries(intent, query, items, coverage)

	return Response{
		Summary:     renderSummary(intent, mustRead, coverage, confidence),
		MustRead:    mustRead,
		Supporting:  supporting,
		Coverage:    coverage,
		Confidence:  confidence,
		NextQueries: nextQueries,
	}
}

// RefreshSummary re-renders the human summary after the application boundary
// refines confidence or coverage using availability and pagination facts.
func RefreshSummary(intent search.Intent, response Response) Response {
	response.Summary = renderSummary(intent, response.MustRead, response.Coverage, response.Confidence)
	return response
}

func RenderText(resp Response) string {
	lines := []string{}
	if strings.TrimSpace(resp.Summary) != "" {
		lines = append(lines, resp.Summary)
	}
	if len(resp.MustRead) > 0 {
		lines = append(lines, "Must read:")
		for _, item := range resp.MustRead {
			lines = append(lines, fmt.Sprintf("- [%s] %s", item.Role, itemLabel(item)))
		}
	}
	if len(resp.Supporting) > 0 {
		lines = append(lines, "Supporting:")
		for _, item := range resp.Supporting {
			lines = append(lines, fmt.Sprintf("- [%s] %s", item.Role, itemLabel(item)))
		}
	}
	if len(resp.Coverage.Missing) > 0 {
		lines = append(lines, "Missing: "+strings.Join(resp.Coverage.Missing, ", "))
	}
	if strings.TrimSpace(resp.Confidence.Level) != "" {
		confidence := "Confidence: " + strings.TrimSpace(resp.Confidence.Level)
		if strings.TrimSpace(resp.Confidence.Reason) != "" {
			confidence += " (" + strings.TrimSpace(resp.Confidence.Reason) + ")"
		}
		lines = append(lines, confidence)
	}
	if len(resp.NextQueries) > 0 {
		lines = append(lines, "Next queries:")
		for _, q := range resp.NextQueries {
			label := strings.TrimSpace(q.Mode)
			if strings.TrimSpace(q.Query) != "" {
				label += " " + strings.TrimSpace(q.Query)
			}
			if len(q.Paths) > 0 {
				label += " " + strings.Join(q.Paths, ", ")
			}
			if strings.TrimSpace(q.Reason) != "" {
				label += " - " + strings.TrimSpace(q.Reason)
			}
			lines = append(lines, "- "+strings.TrimSpace(label))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func classifyItem(in Input) Item {
	item := Item{
		Type:    in.Type,
		Path:    in.Path,
		Title:   in.Title,
		Role:    normalizeRole(in),
		Symbol:  in.Symbol,
		FQN:     in.FQN,
		Line:    in.Line,
		Score:   in.Score,
		Preview: in.Preview,

		Specificity:       in.Specificity,
		DirectSpecificity: in.DirectSpecificity,
		NodeRef:           in.NodeRef,
		LinkTarget:        in.LinkTarget,
		SupportingOnly:    in.SupportingOnly,
		SupportedRoles:    normalizeRoles(in),
		Relevance:         strings.TrimSpace(in.Relevance),
		Eligibility:       strings.TrimSpace(in.Eligibility),
		Relationship:      strings.TrimSpace(in.Relationship),
		FacetProofs:       append([]FacetProof(nil), in.FacetProofs...),
		IdentityMatch:     in.IdentityMatch,
	}
	if len(item.SupportedRoles) > 0 {
		item.Role = item.SupportedRoles[0]
	}
	item.Why = whyForItem(item)
	return item
}

func normalizeRoles(in Input) []string {
	values := append([]string(nil), in.SupportedRoles...)
	if len(values) == 0 && strings.TrimSpace(in.Role) != "" {
		values = append(values, in.Role)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		copy := in
		copy.Role = value
		role := normalizeRole(copy)
		if role == "" {
			continue
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	return out
}

func normalizeRole(in Input) string {
	// Role selection intentionally favors explicit adapter roles first, then path
	// and granularity heuristics. Keeping this local lets CLI/MCP adapters stay thin
	// DTO mappers while answer.Build owns the public role vocabulary.
	role := strings.TrimSpace(in.Role)
	lowerPath := strings.ToLower(in.Path)
	base := strings.ToLower(filepath.Base(in.Path))
	switch {
	case role == "entry_point":
		return "entrypoint"
	case role == "impl":
		return "implementation"
	case role == "overview", role == "documentation", role == "implementation", role == "test", role == "decision", role == "example", role == "supporting", role == "entrypoint":
		return role
	case strings.Contains(lowerPath, "/adr/"), strings.Contains(lowerPath, "/decisions/"), strings.Contains(lowerPath, "decision"):
		return "decision"
	case strings.Contains(lowerPath, "/examples/"), strings.Contains(lowerPath, "/example/"), strings.Contains(base, "example"), strings.Contains(base, "demo"):
		return "example"
	case role == "doc" && (base == "context.md" || base == "readme.md" || strings.Contains(base, "hub")):
		return "overview"
	case role == "doc":
		return "documentation"
	case role == "support":
		return "supporting"
	case in.Type == "code" || in.Type == "anchor":
		return "implementation"
	default:
		return "supporting"
	}
}

func whyForItem(item Item) string {
	// Why strings explain selection provenance in packets and diagnostics. Keep them
	// grounded in already-attached evidence; avoid phrasing that implies this layer
	// performed retrieval or ontology traversal.
	switch item.Role {
	case "overview":
		return "local overview or hub"
	case "documentation":
		return "documentation tied to the task"
	case "entrypoint":
		return "likely entrypoint"
	case "implementation":
		return "primary implementation surface"
	case "test":
		return "behavior check or example"
	case "decision":
		return "decision or durable constraint"
	case "example":
		return "worked example"
	default:
		return "supporting evidence"
	}
}

func desiredRoles(intent search.Intent, frame queryframe.Frame) []string {
	// Docs: [[search-quality-evaluation-corpus#^spec-0041-us1-ac2]] and [[answer-packet-diagnostics-contract#^spec-0050-us2-ac3]].
	// Broad explanatory packets need docs plus implementation evidence; tests and entrypoints are explicit-mode signals.
	if testsOptionalForPacket(intent, frame) {
		return []string{"overview", "implementation", "documentation"}
	}
	switch intent {
	case search.IntentOverview, search.IntentSubsystemOverview:
		return []string{"overview", "entrypoint", "implementation", "test"}
	case search.IntentDocsForCode:
		return []string{"documentation", "overview", "decision", "implementation"}
	case search.IntentCodeForDocs:
		return []string{"implementation", "entrypoint", "test"}
	case search.IntentTestsForCode:
		return []string{"test", "implementation"}
	case search.IntentRefactorImpact:
		return []string{"implementation", "entrypoint", "test", "documentation"}
	case search.IntentGoToDef, search.IntentFindUsages, search.IntentCallers, search.IntentCallees, search.IntentImplementers, search.IntentOverrides, search.IntentImports:
		return []string{"implementation", "entrypoint"}
	default:
		return []string{"overview", "implementation", "documentation", "test"}
	}
}

func testsOptionalForPacket(intent search.Intent, frame queryframe.Frame) bool {
	if frame.MentionsTests || intent == search.IntentTestsForCode {
		return false
	}
	return intent == search.IntentSearch || intent == search.IntentExplainSymbol || frame.IsExplain()
}

func selectItems(intent search.Intent, frame queryframe.Frame, items []Item) ([]Item, []Item) {
	// Docs: [[answer-packet-diagnostics-contract#^spec-0050-us2-ac1]] and [[answer-packet-diagnostics-contract#^spec-0050-us2-ac2]].
	if len(items) == 0 {
		return nil, nil
	}
	items = append([]Item(nil), items...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Specificity != items[j].Specificity {
			return items[i].Specificity > items[j].Specificity
		}
		return items[i].Score > items[j].Score
	})
	selected := map[string]struct{}{}
	mustRead := make([]Item, 0, 6)
	// First satisfy each desired evidence role at most once. This keeps mustRead
	// broad enough for agent action instead of letting one high-scoring evidence
	// lane crowd out docs/code/tests/decisions coverage.
	for _, role := range desiredRoles(intent, frame) {
		for _, item := range items {
			if item.SupportingOnly {
				continue
			}
			if item.Role != role {
				continue
			}
			if shouldSkipWeakEntrypoint(intent, frame, item) {
				continue
			}
			key := itemKey(item)
			if _, ok := selected[key]; ok {
				continue
			}
			selected[key] = struct{}{}
			mustRead = append(mustRead, item)
			break
		}
	}
	for _, item := range items {
		if len(mustRead) >= 6 {
			break
		}
		if shouldDeferWeakDecision(frame, item) {
			continue
		}
		if item.SupportingOnly {
			continue
		}
		if shouldSkipWeakEntrypoint(intent, frame, item) {
			continue
		}
		key := itemKey(item)
		if _, ok := selected[key]; ok {
			continue
		}
		selected[key] = struct{}{}
		mustRead = append(mustRead, item)
	}

	supporting := make([]Item, 0, 6)
	// Supporting preserves additional evidence after mustRead role coverage, but
	// still dedupes by source so packets stay scannable and stable.
	for _, item := range items {
		if len(supporting) >= 6 {
			break
		}
		if shouldSkipWeakEntrypoint(intent, frame, item) {
			continue
		}
		key := itemKey(item)
		if _, ok := selected[key]; ok {
			continue
		}
		selected[key] = struct{}{}
		supporting = append(supporting, item)
	}
	return mustRead, supporting
}

func hasAssessmentProvenance(items []Item) bool {
	for _, item := range items {
		if item.Relevance != "" {
			return true
		}
	}
	return false
}

func selectAssessedItems(intent search.Intent, frame queryframe.Frame, items []Item, requiredFacetKeys []string) ([]Item, []Item) {
	items = append([]Item(nil), items...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Relevance != items[j].Relevance {
			return items[i].Relevance == "strong"
		}
		if specificityDecides(items[i].DirectSpecificity, items[j].DirectSpecificity) {
			return items[i].DirectSpecificity > items[j].DirectSpecificity
		}
		if items[i].Specificity != items[j].Specificity {
			return items[i].Specificity > items[j].Specificity
		}
		return items[i].Score > items[j].Score
	})

	covered := map[string]struct{}{}
	selected := map[string]struct{}{}
	mustRead := make([]Item, 0, min(6, len(items)))
	// An exact title, path, or symbol hit is what the caller asked for; it leads before role coverage fills the packet.
	if !search.IsPrecisionIntent(intent) {
		leader := -1
		for index, item := range items {
			if !item.IdentityMatch || item.SupportingOnly || item.Relevance != "strong" {
				continue
			}
			if leader < 0 || item.Score > items[leader].Score {
				leader = index
			}
		}
		if leader >= 0 {
			mustRead = append(mustRead, items[leader])
			selected[itemKey(items[leader])] = struct{}{}
			for proof := range selectionProofs(items[leader]) {
				covered[proof] = struct{}{}
			}
		}
	}

	required := map[string]struct{}{}
	requiredOrder := make([]string, 0, len(requiredFacetKeys)+4)
	addRequired := func(proof string) {
		if _, exists := required[proof]; exists {
			return
		}
		required[proof] = struct{}{}
		requiredOrder = append(requiredOrder, proof)
	}
	for _, facet := range requiredFacetKeys {
		if facet = strings.TrimSpace(facet); facet != "" {
			addRequired("facet:" + facet)
		}
	}
	for _, role := range requiredRolesForAssessedPacket(intent, frame) {
		addRequired("role:" + role)
	}
	if relationship := requiredRelationship(intent); relationship != "" {
		addRequired("relationship:" + relationship)
	}
	if len(requiredFacetKeys) == 0 && !search.IsPrecisionIntent(intent) {
		addRequired("query")
	}

	// Choose the best evidence for each requested proof before set-cover
	// deduplication. Otherwise a broad but weakly matched source can win solely
	// because it claims multiple facets and displace the direct source for each.
	for _, proof := range requiredOrder {
		if len(mustRead) >= 6 {
			break
		}
		best := -1
		for index, item := range items {
			if item.SupportingOnly || (item.Relevance != "strong" && item.Relevance != "useful") {
				continue
			}
			if _, ok := selectionProofs(item)[proof]; !ok {
				continue
			}
			if best < 0 || assessedProofCandidateLess(proof, item, items[best]) {
				best = index
			}
		}
		if best < 0 {
			continue
		}
		covered[proof] = struct{}{}
		key := itemKey(items[best])
		if _, exists := selected[key]; exists {
			continue
		}
		selected[key] = struct{}{}
		mustRead = append(mustRead, items[best])
	}
	for _, item := range mustRead {
		for proof := range selectionProofs(item) {
			covered[proof] = struct{}{}
		}
	}
	for len(mustRead) < 6 {
		bestStrongIndex, bestStrongGain := -1, 0
		bestUsefulIndex, bestUsefulGain := -1, 0
		for index, item := range items {
			if item.SupportingOnly || (item.Relevance != "strong" && item.Relevance != "useful") {
				continue
			}
			if _, exists := selected[itemKey(item)]; exists {
				continue
			}
			gain := 0
			for proof := range selectionProofs(item) {
				if _, needed := required[proof]; !needed {
					continue
				}
				if _, done := covered[proof]; !done {
					gain++
				}
			}
			if item.Relevance == "strong" && gain > bestStrongGain {
				bestStrongIndex, bestStrongGain = index, gain
			}
			if item.Relevance == "useful" && gain > bestUsefulGain {
				bestUsefulIndex, bestUsefulGain = index, gain
			}
		}
		bestIndex := bestStrongIndex
		if bestIndex < 0 {
			bestIndex = bestUsefulIndex
		}
		if bestIndex < 0 {
			break
		}
		item := items[bestIndex]
		selected[itemKey(item)] = struct{}{}
		mustRead = append(mustRead, item)
		for proof := range selectionProofs(item) {
			covered[proof] = struct{}{}
		}
	}
	for _, proof := range exhaustiveProofRequirements(intent, requiredFacetKeys) {
		for _, item := range items {
			if len(mustRead) >= 6 {
				break
			}
			if !itemSatisfiesExhaustiveProof(item, proof) {
				continue
			}
			key := itemKey(item)
			if _, exists := selected[key]; exists {
				continue
			}
			selected[key] = struct{}{}
			mustRead = append(mustRead, item)
		}
	}

	supporting := make([]Item, 0, 6)
	for _, item := range items {
		if len(supporting) >= 6 {
			break
		}
		key := itemKey(item)
		if _, exists := selected[key]; exists {
			continue
		}
		selected[key] = struct{}{}
		supporting = append(supporting, item)
	}
	return mustRead, supporting
}

func assessedProofCandidateLess(proof string, left, right Item) bool {
	if left.Relevance != right.Relevance {
		return left.Relevance == "strong"
	}
	leftProofStrong := facetProofStrong(proof, left)
	rightProofStrong := facetProofStrong(proof, right)
	if leftProofStrong != rightProofStrong {
		return leftProofStrong
	}
	leftSpecificity := proofSpecificity(proof, left)
	rightSpecificity := proofSpecificity(proof, right)
	if strings.HasPrefix(proof, "facet:") {
		// A facet is short and targeted, so its own identity match stays decisive.
		if leftSpecificity != rightSpecificity {
			return leftSpecificity > rightSpecificity
		}
	} else if specificityDecides(leftSpecificity, rightSpecificity) {
		return leftSpecificity > rightSpecificity
	}
	if specificityDecides(left.DirectSpecificity, right.DirectSpecificity) {
		return left.DirectSpecificity > right.DirectSpecificity
	}
	if left.Specificity != right.Specificity {
		return left.Specificity > right.Specificity
	}
	return left.Score > right.Score
}

func facetProofStrong(proof string, item Item) bool {
	if !strings.HasPrefix(proof, "facet:") {
		return false
	}
	key := strings.TrimPrefix(proof, "facet:")
	for _, facet := range item.FacetProofs {
		if facet.Key == key {
			return facet.Strong
		}
	}
	return false
}

func proofSpecificity(proof string, item Item) float64 {
	if !strings.HasPrefix(proof, "facet:") {
		return item.DirectSpecificity
	}
	key := strings.TrimPrefix(proof, "facet:")
	text, _, _ := strings.Cut(key, "\x00")
	return queryframe.ScoreFields(queryframe.Extract(text), queryframe.Fields{
		Path: item.Path, Title: item.Title, Symbol: item.Symbol, FQN: item.FQN,
	}).Value
}

func assessedProofs(item Item) map[string]struct{} {
	proofs := map[string]struct{}{}
	if item.Relevance == "strong" {
		proofs["query"] = struct{}{}
	}
	for _, role := range item.SupportedRoles {
		proofs["role:"+role] = struct{}{}
	}
	if item.Eligibility == "primary" && item.Relationship != "" {
		proofs["relationship:"+item.Relationship] = struct{}{}
	}
	for _, facet := range item.FacetProofs {
		if facet.Strong && facetProofQualifies(facet) && strings.TrimSpace(facet.Key) != "" {
			proofs["facet:"+strings.TrimSpace(facet.Key)] = struct{}{}
		}
	}
	return proofs
}

func selectionProofs(item Item) map[string]struct{} {
	proofs := assessedProofs(item)
	if item.Relevance == "useful" {
		proofs["query"] = struct{}{}
		for _, facet := range item.FacetProofs {
			if facetProofQualifies(facet) && strings.TrimSpace(facet.Key) != "" {
				proofs["facet:"+strings.TrimSpace(facet.Key)] = struct{}{}
			}
		}
	}
	return proofs
}

func facetProofQualifies(facet FacetProof) bool {
	if !search.IsPrecisionIntent(facet.Mode) {
		return true
	}
	return facet.TargetEstablished && facet.Eligibility == "primary" && facet.Relationship == requiredRelationship(facet.Mode)
}

func appendRelationshipTruncation(intent search.Intent, items, selected []Item, requiredFacetKeys, missing []string) []string {
	for _, proof := range exhaustiveProofRequirements(intent, requiredFacetKeys) {
		available, retained := 0, 0
		for _, item := range items {
			if itemSatisfiesExhaustiveProof(item, proof) {
				available++
			}
		}
		for _, item := range selected {
			if itemSatisfiesExhaustiveProof(item, proof) {
				retained++
			}
		}
		if retained < available {
			missing = appendMissing(missing, proof.missing)
		}
	}
	return missing
}

type exhaustiveProofRequirement struct {
	relationship string
	facetKey     string
	missing      string
}

func exhaustiveProofRequirements(intent search.Intent, requiredFacetKeys []string) []exhaustiveProofRequirement {
	var out []exhaustiveProofRequirement
	if relationship := requiredRelationship(intent); relationship != "" && requiresAllRelationshipEvidence(intent) {
		out = append(out, exhaustiveProofRequirement{relationship: relationship, missing: "relationship:" + relationship + ":truncated"})
	}
	for _, facetKey := range requiredFacetKeys {
		parts := strings.SplitN(strings.TrimSpace(facetKey), "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		mode := search.Intent(parts[1])
		relationship := requiredRelationship(mode)
		if relationship == "" || !requiresAllRelationshipEvidence(mode) {
			continue
		}
		out = append(out, exhaustiveProofRequirement{relationship: relationship, facetKey: facetKey, missing: "facet:" + parts[0] + ":truncated"})
	}
	return out
}

func itemSatisfiesExhaustiveProof(item Item, proof exhaustiveProofRequirement) bool {
	if item.Relevance != "strong" {
		return false
	}
	if proof.facetKey == "" {
		return item.Eligibility == "primary" && item.Relationship == proof.relationship
	}
	for _, facet := range item.FacetProofs {
		if facet.Key == proof.facetKey && facet.Strong && facetProofQualifies(facet) && facet.Relationship == proof.relationship {
			return true
		}
	}
	return false
}

func requiredRolesForAssessedPacket(intent search.Intent, frame queryframe.Frame) []string {
	switch intent {
	case search.IntentOverview, search.IntentSubsystemOverview:
		return []string{"overview", "implementation"}
	case search.IntentDocsForCode:
		return []string{"documentation"}
	case search.IntentCodeForDocs, search.IntentGoToDef, search.IntentImplementers:
		return []string{"implementation"}
	case search.IntentTestsForCode:
		return []string{"test"}
	case search.IntentRefactorImpact:
		return []string{"implementation", "test"}
	default:
		if frame.MentionsTests {
			return []string{"test"}
		}
		return nil
	}
}

func requiresAllRelationshipEvidence(intent search.Intent) bool {
	switch intent {
	case search.IntentFindUsages, search.IntentCallers, search.IntentCallees, search.IntentTestsForCode, search.IntentImplementers, search.IntentOverrides, search.IntentImports:
		return true
	default:
		return false
	}
}

func requiredRelationship(intent search.Intent) string {
	switch intent {
	case search.IntentGoToDef:
		return "definition"
	case search.IntentFindUsages:
		return "usage"
	case search.IntentCallers:
		return "caller"
	case search.IntentCallees:
		return "callee"
	case search.IntentTestsForCode:
		return "tests"
	case search.IntentImplementers:
		return "implementation"
	case search.IntentOverrides:
		return "override"
	case search.IntentImports:
		return "import"
	default:
		return ""
	}
}

func appendAssessedMissing(intent search.Intent, selected []Item, requiredFacetKeys, missing []string) []string {
	proofs := map[string]struct{}{}
	for _, item := range selected {
		for proof := range assessedProofs(item) {
			proofs[proof] = struct{}{}
		}
	}
	if relationship := requiredRelationship(intent); relationship != "" {
		key := "relationship:" + relationship
		if _, ok := proofs[key]; !ok {
			missing = appendMissing(missing, key)
		}
	}
	for _, facet := range requiredFacetKeys {
		facet = strings.TrimSpace(facet)
		key := "facet:" + facet
		if _, ok := proofs[key]; !ok {
			label := strings.SplitN(facet, "\x00", 2)[0]
			missing = appendMissing(missing, "facet:"+label)
		}
	}
	if len(requiredFacetKeys) == 0 && !search.IsPrecisionIntent(intent) {
		if _, ok := proofs["query"]; !ok {
			missing = appendMissing(missing, "query_evidence")
		}
	}
	sort.Strings(missing)
	return missing
}

func itemKey(item Item) string {
	if item.NodeRef != nil && item.NodeRef.Kind != "" && item.NodeRef.Kind != "NOTE" {
		return strings.Join([]string{"node", item.NodeRef.NotePath, item.NodeRef.NodeID, item.NodeRef.Fragment, item.NodeRef.StructuralFingerprint}, "|")
	}
	if fqn := strings.TrimSpace(item.FQN); fqn != "" {
		return "fqn|" + fqn + "|" + strings.TrimSpace(item.Path)
	}
	if path := strings.TrimSpace(item.Path); path != "" {
		return "path|" + path
	}
	return item.Type + "|" + item.Title + "|" + item.Symbol
}

func shouldDeferWeakDecision(frame queryframe.Frame, item Item) bool {
	// WHY: explanatory packets should surface concrete docs/code before weakly matched decisions; decisions remain eligible as supporting evidence.
	// Docs: [[answer-packet-diagnostics-contract#^spec-0050-us1-ac2]].
	return frame.IsExplain() && item.Role == "decision" && item.DirectSpecificity < 0.75
}

func shouldSkipWeakEntrypoint(intent search.Intent, frame queryframe.Frame, item Item) bool {
	if item.Role != "entrypoint" {
		return false
	}
	threshold := 0.25
	if testsOptionalForPacket(intent, frame) {
		threshold = 0.5
	}
	return item.DirectSpecificity < threshold
}

func buildCoverageForRoles(intent search.Intent, frame queryframe.Frame, items []Item, requiredRoles []string) CoverageReport {
	// Docs: [[answer-packet-diagnostics-contract#^spec-0050-us3-ac1]].
	coverage := CoverageReport{}
	for _, item := range items {
		if shouldSkipWeakEntrypoint(intent, frame, item) {
			continue
		}
		roles := []string{item.Role}
		if item.Relevance != "" {
			if item.Relevance != "strong" {
				continue
			}
			roles = item.SupportedRoles
		}
		for _, role := range roles {
			switch role {
			case "overview", "documentation":
				coverage.Docs = true
			case "implementation", "entrypoint":
				coverage.Code = true
			case "test":
				coverage.Tests = true
			case "decision":
				coverage.Decisions = true
			case "example":
				coverage.Examples = true
			}
		}
	}
	for _, role := range requiredRoles {
		switch role {
		case "overview", "documentation":
			if !coverage.Docs {
				coverage.Missing = appendMissing(coverage.Missing, "docs")
			}
		case "implementation", "entrypoint":
			if !coverage.Code {
				coverage.Missing = appendMissing(coverage.Missing, "code")
			}
		case "test":
			if !coverage.Tests {
				coverage.Missing = appendMissing(coverage.Missing, "tests")
			}
		case "decision":
			if !coverage.Decisions {
				coverage.Missing = appendMissing(coverage.Missing, "decisions")
			}
		case "example":
			if !coverage.Examples {
				coverage.Missing = appendMissing(coverage.Missing, "examples")
			}
		}
	}
	sort.Strings(coverage.Missing)
	return coverage
}

func buildConfidence(targetStatus search.TargetStatus, warnings []search.Warning, mustRead []Item, coverage CoverageReport) ConfidenceReport {
	// Docs: [[search-quality-evaluation-corpus#^spec-0041-us2-ac2]] and
	// [[search-quality-evaluation-corpus#^spec-0041-us4-ac2]] and [[answer-packet-diagnostics-contract#^spec-0050-us3-ac2]].
	missing := append([]string(nil), coverage.Missing...)
	confidence := ConfidenceReport{MissingSignals: missing}
	strongEvidence := false
	for _, item := range mustRead {
		if item.DirectSpecificity >= directSpecificityThreshold || item.Specificity >= 0.7 {
			strongEvidence = true
			break
		}
	}
	// Confidence describes the shaped packet, not global truth. Target ambiguity,
	// empty mustRead, warnings, and missing role coverage lower confidence even when
	// individual upstream scores were high.
	switch {
	case targetStatus == search.TargetStatusAmbiguous || targetStatus == search.TargetStatusUnresolved:
		confidence.Level = "low"
		confidence.Reason = "target resolution weak"
	case len(mustRead) == 0:
		confidence.Level = "low"
		confidence.Reason = "no strong evidence selected"
	case strongEvidence && len(coverage.Missing) == 0 && len(warnings) == 0:
		confidence.Level = "high"
		confidence.Reason = "strong relevant evidence covers the required roles"
	default:
		confidence.Level = "medium"
		confidence.Reason = "useful packet, but coverage is incomplete"
	}
	if len(coverage.Missing) > 2 {
		confidence.Level = "low"
		confidence.Reason = "several required evidence slots missing"
	}
	return confidence
}

func buildNextQueries(intent search.Intent, query string, items []Item, coverage CoverageReport) []SuggestedQuery {
	// Docs: [[answer-packet-diagnostics-contract#^spec-0050-us4-ac1]] and [[answer-packet-diagnostics-contract#^spec-0050-us4-ac2]].
	basePath := ""
	for _, item := range items {
		if strings.TrimSpace(item.Path) != "" {
			basePath = item.Path
			break
		}
	}
	next := make([]SuggestedQuery, 0, 3)
	for _, missing := range coverage.Missing {
		switch missing {
		case "docs":
			next = append(next, SuggestedQuery{Mode: string(search.IntentDocsForCode), Paths: onePath(basePath), Reason: "direct docs missing from current packet"})
		case "code":
			next = append(next, SuggestedQuery{Mode: string(search.IntentCodeForDocs), Paths: onePath(basePath), Reason: "code evidence missing from current packet"})
		case "tests":
			next = append(next, SuggestedQuery{Mode: string(search.IntentTestsForCode), Paths: onePath(basePath), Reason: "tests omitted or not found"})
		case "decisions":
			next = append(next, SuggestedQuery{Mode: string(search.IntentOverview), Query: strings.TrimSpace(query + " decisions"), Reason: "look for design constraints or ADRs"})
		}
	}
	if len(next) == 0 && search.IsBroadIntent(intent) && len(items) > 0 {
		next = append(next, SuggestedQuery{Mode: string(search.IntentSubsystemOverview), Paths: onePath(basePath), Reason: "follow up with a tighter local packet"})
	}
	return next
}

func renderSummary(intent search.Intent, mustRead []Item, coverage CoverageReport, confidence ConfidenceReport) string {
	parts := []string{fmt.Sprintf("Confidence: %s.", strings.TrimSpace(confidence.Level))}
	if len(mustRead) > 0 {
		parts = append(parts, fmt.Sprintf("Top packet covers %d item(s).", len(mustRead)))
	}
	if len(coverage.Missing) == 0 {
		parts = append(parts, "Coverage looks complete for this mode.")
	} else {
		parts = append(parts, "Missing "+strings.Join(coverage.Missing, ", ")+".")
	}
	if search.IsPrecisionIntent(intent) {
		parts = append(parts, "Precision mode kept the packet narrow.")
	}
	return strings.Join(parts, " ")
}

func itemLabel(item Item) string {
	label := strings.TrimSpace(item.Path)
	if label == "" {
		label = strings.TrimSpace(item.Title)
	}
	if label == "" {
		label = strings.TrimSpace(item.Symbol)
	}
	if item.Line > 0 && label != "" {
		label = fmt.Sprintf("%s:%d", label, item.Line)
	}
	ident := strings.TrimSpace(item.Symbol)
	if ident == "" {
		ident = strings.TrimSpace(item.FQN)
	}
	labelBase := label
	if item.Line > 0 {
		if suffix := fmt.Sprintf(":%d", item.Line); strings.HasSuffix(labelBase, suffix) {
			labelBase = strings.TrimSuffix(labelBase, suffix)
		}
	}
	if ident != "" && ident != filepath.Base(item.Path) && ident != labelBase {
		if label != "" {
			label += " " + ident
		} else {
			label = ident
		}
	}
	if item.LinkTarget != nil && strings.TrimSpace(item.LinkTarget.Wikilink) != "" {
		label += " link: " + strings.TrimSpace(item.LinkTarget.Wikilink)
	}
	if item.Why == "" {
		return label
	}
	return label + " - " + item.Why
}

func appendMissing(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func onePath(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return []string{path}
}

// directSpecificityThreshold is the identity-field match strength that counts
// as a real title, path, or symbol hit rather than incidental token overlap.
const directSpecificityThreshold = 0.6

// specificityDecides reports whether two identity-field specificities should
// order sources. Below the threshold, one shared token must not outrank the
// engine score of an otherwise equal source.
func specificityDecides(left, right float64) bool {
	return left != right && max(left, right) >= directSpecificityThreshold
}
