package retrieval

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// OntologyRetriever expands typed note seeds through persisted ontology edges.
//
// Structural edges are favored over ambient ones via evidence scoring. Ambient
// fallback is intent/policy controlled so ontology traversal can improve recall
// without making every body link equivalent to schema-authored structure.
type OntologyRetriever struct {
	Store          *semdb.Store
	Scope          *noderead.Scope
	IncludeAmbient bool
	Limit          int
}

func (r *OntologyRetriever) Name() string { return "ontology" }

func (r *OntologyRetriever) CostClass() search.RetrieverCostClass { return search.RetrieverCostIOBound }

func (r *OntologyRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if r.Store == nil || len(spec.Seeds) == 0 {
		return nil, nil
	}

	limit := r.Limit
	if limit <= 0 {
		limit = 30
	}

	seedPaths := make([]string, 0, len(spec.Seeds))
	seenSeeds := make(map[string]struct{})
	for _, seed := range spec.Seeds {
		if seed.Kind != knowledge.KindNote && seed.Kind != knowledge.KindNoteChunk {
			continue
		}
		p, ok := cleanTypedNotePath(seed.ID)
		if !ok {
			continue
		}
		if _, ok := seenSeeds[p]; ok {
			continue
		}
		seenSeeds[p] = struct{}{}
		seedPaths = append(seedPaths, p)
	}

	seen := make(map[string]struct{})
	out := make([]search.Candidate, 0, limit)
	scope := r.Scope
	if scope == nil {
		// Standalone retriever tests may not pass a scope. Planner-created
		// retrievers should share one request-scoped Scope with other ontology
		// graph consumers for this search run.
		scope = noderead.NewService(obsidian.VaultDefinition{}, &obsidian.Note{}, r.Store, nil).NewScope(ctx, noderead.ScopeOptions{})
	}
	basePolicy := relationPolicyForIntent(spec.Intent, r.IncludeAmbient)
	policyByPath, err := r.loadPolicyOverrides(ctx, seedPaths, spec.Intent)
	if err != nil {
		return nil, err
	}
	for _, seedPath := range seedPaths {
		policy := basePolicy
		if override, ok := policyByPath[seedPath]; ok {
			policy = applyPolicyOverride(basePolicy, override)
		}
		out, err = r.expandSeed(ctx, scope, out, seen, seedPath, policy, spec.Text, limit)
		if err != nil {
			return nil, err
		}
		if len(out) >= limit {
			return out, nil
		}
	}
	return out, nil
}

func (r *OntologyRetriever) expandSeed(ctx context.Context, scope *noderead.Scope, out []search.Candidate, seen map[string]struct{}, seedPath string, policy relationPolicy, queryText string, limit int) ([]search.Candidate, error) {
	maxDepth := policy.maxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	policy.maxDepth = maxDepth
	expanded, err := r.expandWithPolicy(ctx, scope, seedPath, policy, limit)
	if err != nil {
		return out, err
	}
	bestByKey := make(map[string]search.Candidate, len(expanded.Edges))
	for _, edge := range expanded.Edges {
		neighbor, ok := cleanTypedNotePath(edge.Target.NotePath)
		if !ok || neighbor == seedPath {
			continue
		}
		key := "note:" + neighbor
		if _, ok := seen[key]; ok {
			continue
		}
		candidate := buildOntologyCandidate(seedPath, edge.Source.NotePath, neighbor, edge.Edge, queryText, edge.Depth)
		if previous, ok := bestByKey[key]; !ok || candidate.Evidence[0].RawScore > previous.Evidence[0].RawScore {
			bestByKey[key] = candidate
		}
	}
	candidates := make([]search.Candidate, 0, len(bestByKey))
	for _, candidate := range bestByKey {
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Evidence[0].RawScore > candidates[j].Evidence[0].RawScore
	})
	for _, candidate := range candidates {
		seen[candidate.Handle.String()] = struct{}{}
		out = append(out, candidate)
		if len(out) >= limit {
			return out, nil
		}
	}

	return out, nil
}

func (r *OntologyRetriever) expandWithPolicy(ctx context.Context, scope *noderead.Scope, seedPath string, policy relationPolicy, limit int) (noderead.ExpansionResult, error) {
	result, err := scope.Expand(ctx, ontologyExpansionPlan(seedPath, policy, limit))
	if err != nil {
		return noderead.ExpansionResult{}, err
	}
	if !policy.sparseFallback || policy.includeAmbient {
		return result, nil
	}
	structuralHits := 0
	for _, edge := range result.Edges {
		if edge.Structural {
			structuralHits++
		}
	}
	if structuralHits >= policy.minStructuralHits {
		return result, nil
	}
	// Sparse fallback is a recall escape hatch: only add ambient links when the
	// structural graph produced too little evidence for this intent/type policy.
	policy.includeAmbient = true
	return scope.Expand(ctx, ontologyExpansionPlan(seedPath, policy, relationMax(limit*2, 12)))
}

func ontologyExpansionPlan(seedPath string, policy relationPolicy, limit int) noderead.ExpansionPlan {
	return noderead.ExpansionPlan{
		Sources: []ontology.NodeRef{{NotePath: seedPath, Kind: ontology.NodeKindNote}},
		Steps: []noderead.ExpansionStep{{
			Direction:         noderead.TraversalDirectionBoth,
			IncludeStructural: true,
			IncludeAmbient:    policy.includeAmbient,
		}},
		Limits: noderead.TraverseLimits{
			FirstTotal: limit,
			MaxDepth:   policy.maxDepth,
			MaxEdges:   limit,
		},
		Purpose: "semantic_retrieval",
	}
}

func buildOntologyCandidate(seedPath, viaPath, neighbor string, row semdb.OntologyEdgeRow, queryText string, depth int) search.Candidate {
	kind := row.Kind()
	evType := "ontology_relation_ambient"
	score := 1.05
	if kind.IsStructural() {
		evType = "ontology_relation_structural"
		score = 1.25
	}
	score += semanticRelationBoost(queryText, row)
	score *= relationDepthWeight(depth)

	h := knowledge.NoteHandle(neighbor)
	return search.Candidate{
		Handle: h,
		Owner:  h,
		Evidence: []search.Evidence{{
			Type:     evType,
			RawScore: score,
			Source:   "ontology",
			Details: map[string]string{
				"seed":       seedPath,
				"via":        viaPath,
				"relation":   row.RelationName,
				"provenance": row.Provenance,
				"depth":      strconv.Itoa(depth),
			},
		}},
		Type:       "note",
		NoteID:     neighbor,
		Path:       neighbor,
		Title:      ontologyTitleFromPath(neighbor),
		ChunkIndex: -1,
	}
}

func ontologyTitleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

type relationPolicy struct {
	includeAmbient    bool
	sparseFallback    bool
	minStructuralHits int
	maxDepth          int
}

func relationPolicyForIntent(intent search.Intent, includeAmbient bool) relationPolicy {
	policy := relationPolicy{
		includeAmbient:    includeAmbient,
		sparseFallback:    true,
		minStructuralHits: 2,
		maxDepth:          1,
	}
	switch intent {
	case search.IntentDocsForCode:
		policy.includeAmbient = false
		policy.minStructuralHits = 1
	case search.IntentOverview, search.IntentSubsystemOverview, search.IntentRelatedToSeed:
		policy.includeAmbient = true
		policy.minStructuralHits = 2
	case search.IntentGoToDef, search.IntentFindUsages, search.IntentCallers, search.IntentCallees:
		policy.includeAmbient = false
		policy.minStructuralHits = 1
		policy.sparseFallback = false
	}
	return policy
}

func (r *OntologyRetriever) loadPolicyOverrides(ctx context.Context, seedPaths []string, intent search.Intent) (map[string]ontology.TraversalOverride, error) {
	if len(seedPaths) == 0 {
		return nil, nil
	}
	rows, err := r.Store.OntologyTypePoliciesForPaths(ctx, seedPaths)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make(map[string]ontology.TraversalOverride, len(rows))
	for path, row := range rows {
		policy, err := ontology.UnmarshalTypePolicy(row.PolicyJSON)
		if err != nil {
			continue
		}
		override, ok := policy.OverrideForIntent(string(intent))
		if !ok {
			continue
		}
		out[path] = override
	}
	return out, nil
}

func applyPolicyOverride(base relationPolicy, override ontology.TraversalOverride) relationPolicy {
	out := base
	if override.IncludeAmbient != nil {
		out.includeAmbient = *override.IncludeAmbient
	}
	if override.MinStructuralHits != nil && *override.MinStructuralHits >= 0 {
		out.minStructuralHits = *override.MinStructuralHits
	}
	if override.MaxDepth != nil && *override.MaxDepth > 0 {
		out.maxDepth = *override.MaxDepth
	}
	return out
}

func semanticRelationBoost(queryText string, row semdb.OntologyEdgeRow) float64 {
	score := relationSemanticScore(queryText, row)
	if score <= 0 {
		return 0
	}
	return relationMin(score*0.35, 0.35)
}

func relationSemanticScore(queryText string, row semdb.OntologyEdgeRow) float64 {
	queryTokens := tokens(queryText)
	if len(queryTokens) == 0 {
		return 0
	}
	targetTokens := append(tokens(row.RelationName), tokens(row.DstType)...)
	targetTokens = append(targetTokens, tokens(row.Provenance)...)
	if len(targetTokens) == 0 {
		return 0
	}
	matched := 0
	targetSet := make(map[string]struct{}, len(targetTokens))
	for _, token := range targetTokens {
		targetSet[token] = struct{}{}
	}
	for _, token := range queryTokens {
		if _, ok := targetSet[token]; ok {
			matched++
		}
	}
	if matched == 0 {
		return 0
	}
	return float64(matched) / float64(len(queryTokens))
}

func tokens(raw string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(raw)), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	})
	if len(parts) == 0 {
		return nil
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		out = append(out, part)
	}
	return out
}

func relationMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func relationMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func relationDepthWeight(depth int) float64 {
	if depth <= 1 {
		return 1
	}
	weight := 1.0 - 0.15*float64(depth-1)
	if weight < 0.55 {
		return 0.55
	}
	return weight
}
