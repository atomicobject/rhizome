package identifierreconcile

import (
	"context"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/reference"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type identifierLinkResolutionInventory struct {
	paths       map[string][]ontology.NodeRef
	identities  map[string][]ontology.NodeRef
	fragments   map[string][]ontology.NodeRef
	claims      map[string]Claim
	rewriteRefs map[string]struct{}
}

func newIdentifierLinkResolutionInventory(sources []notemeta.NoteSourceSnapshot, projections []*ontology.NodeProjection, fieldOccurrences []reference.StructuredFieldOccurrence, rewrites []reference.IdentifierRewrite) (*identifierLinkResolutionInventory, error) {
	inventory := &identifierLinkResolutionInventory{
		paths: make(map[string][]ontology.NodeRef), identities: make(map[string][]ontology.NodeRef),
		fragments: make(map[string][]ontology.NodeRef), claims: make(map[string]Claim), rewriteRefs: make(map[string]struct{}, len(rewrites)),
	}
	aliasFieldsByType := identifierAliasFieldsByType(projections, rewrites)
	for _, rewrite := range rewrites {
		inventory.rewriteRefs[repairRefKey(rewrite.OldRef)] = struct{}{}
	}
	aliasesByPath := make(map[string][]string, len(sources))
	for _, source := range sources {
		aliasesByPath[source.Path.String()] = append([]string(nil), source.Aliases...)
	}
	type identityProfile struct {
		preferredField string
		pool           PoolKey
		poolReady      bool
	}
	profiles := make(map[string]identityProfile, len(projections))
	for _, projection := range projections {
		if projection == nil || projection.Type == nil {
			continue
		}
		preferred := preferredIdentifierProjectionField(projection)
		ref := projectedResolutionRef(projection.Ref, projection.ResolvedType)
		if ref.Kind == ontology.NodeKindNote {
			inventory.indexPath(ref.NotePath, ref)
		}
		inventory.indexFragment(ref, projection.Ref)
		var pool PoolKey
		poolReady := false
		if preferred != nil && preferred.IdentifierFormat != nil {
			var err error
			pool, err = NewPoolKey(preferred.IdentifierFormat)
			if err != nil {
				return nil, err
			}
			poolReady = true
		}
		profile := identityProfile{poolReady: poolReady, pool: pool}
		if preferred != nil {
			profile.preferredField = preferred.Name
		}
		profiles[repairRefKey(ref)] = profile
		for _, field := range projection.Type.Fields {
			if field == nil {
				continue
			}
			binding, ok := projection.Fields[field.Name]
			if !ok || (!binding.Present && !binding.Derived) {
				continue
			}
			aliasField := slices.Contains(aliasFieldsByType[projection.Type.Name], field.Name)
			for _, value := range binding.Values {
				if !field.IsPreferredIdentifier && !field.IsIdentifier && !aliasField {
					continue
				}
				inventory.indexIdentity(value, ref)
				if poolReady && (field.IsPreferredIdentifier || aliasField) {
					kind := ClaimAlias
					if field.IsPreferredIdentifier {
						kind = ClaimPreferred
					}
					inventory.indexClaim(value, ref, preferred.Name, pool, kind)
				}
			}
		}
		if ref.Kind == ontology.NodeKindNote {
			for _, alias := range aliasesByPath[ref.NotePath] {
				inventory.indexIdentity(alias, ref)
				if poolReady {
					inventory.indexClaim(alias, ref, preferred.Name, pool, ClaimAlias)
				}
			}
		}
	}
	// Custom alias fields may intentionally remain outside the type SDL. The
	// field constructor is the trusted source of their exact authored values;
	// consume that sealed inventory instead of reparsing caller-provided fields.
	for _, occurrence := range fieldOccurrences {
		if occurrence.Kind != reference.StructuredFieldAliasIdentifier {
			continue
		}
		ref := semanticRepairRef(occurrence.OwnerRef)
		inventory.indexIdentity(occurrence.Value, ref)
		if profile, found := profiles[repairRefKey(ref)]; found && profile.poolReady && profile.preferredField != "" {
			inventory.indexClaim(occurrence.Value, ref, profile.preferredField, profile.pool, ClaimAlias)
		}
	}
	inventory.normalize()
	return inventory, nil
}

func preferredIdentifierProjectionField(projection *ontology.NodeProjection) *ontology.Field {
	if projection == nil || projection.Type == nil {
		return nil
	}
	for _, field := range projection.Type.Fields {
		if field != nil && field.IsPreferredIdentifier {
			return field
		}
	}
	return nil
}

func projectedResolutionRef(ref ontology.NodeRef, resolvedType string) ontology.NodeRef {
	kind := ref.Kind
	if kind == "" {
		kind = ontology.NodeKindNote
	}
	ref.Fragment = strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	ref.TypeName = strings.TrimSpace(resolvedType)
	ref.Kind = kind
	if kind == ontology.NodeKindNote {
		ref.NodeID = ""
		ref.StartByte = 0
		ref.EndByte = 0
		ref.ParentID = ""
		ref.Structural = ""
	}
	return ref
}

func (inventory *identifierLinkResolutionInventory) indexPath(notePath string, ref ontology.NodeRef) {
	stem := strings.TrimSuffix(notePath, ".md")
	for _, value := range []string{notePath, stem, path.Base(notePath), path.Base(stem)} {
		key, ok := canonicalAuthoredPathKey(value)
		if ok {
			inventory.paths[key] = appendUniqueFieldCandidate(inventory.paths[key], ref)
		}
	}
}

func (inventory *identifierLinkResolutionInventory) indexIdentity(value string, ref ontology.NodeRef) {
	key := IdentifierComparisonKey(value)
	if key != "" {
		inventory.identities[key] = appendUniqueFieldCandidate(inventory.identities[key], ref)
	}
}

func (inventory *identifierLinkResolutionInventory) indexFragment(ref, raw ontology.NodeRef) {
	values := []string{raw.Fragment}
	if nodeID := strings.TrimPrefix(strings.TrimSpace(raw.NodeID), "^"); nodeID != "" {
		values = append(values, "^"+nodeID)
	}
	for _, value := range values {
		value = canonicalFragmentKey(value)
		if value == "" {
			continue
		}
		noteKey, ok := canonicalAuthoredPathKey(ref.NotePath)
		if !ok {
			continue
		}
		key := noteKey + "\x00" + value
		inventory.fragments[key] = appendUniqueFieldCandidate(inventory.fragments[key], ref)
	}
}

func (inventory *identifierLinkResolutionInventory) indexClaim(value string, ref ontology.NodeRef, field string, pool PoolKey, kind ClaimKind) {
	node, err := NewCanonicalNodeKey(ref.NotePath, ref.Fragment, ref.TypeName, field)
	if err != nil {
		return
	}
	claim := Claim{Node: node, Pool: pool, Value: value, Kind: kind}
	normalized, err := normalizeClaim(claim)
	if err != nil {
		return
	}
	key := linkClaimKey(value, ref)
	prior, found := inventory.claims[key]
	if !found || (prior.Kind == ClaimAlias && normalized.Kind == ClaimPreferred) {
		inventory.claims[key] = normalized
	}
}

func (inventory *identifierLinkResolutionInventory) normalize() {
	for _, index := range []map[string][]ontology.NodeRef{inventory.paths, inventory.identities, inventory.fragments} {
		for key := range index {
			sort.Slice(index[key], func(i, j int) bool { return jsonKey(index[key][i]) < jsonKey(index[key][j]) })
		}
	}
}

func (inventory *identifierLinkResolutionInventory) resolutionsFor(sourcePath string, links []obsidian.StructuredLink) []reference.StructuredLinkResolution {
	out := make([]reference.StructuredLinkResolution, 0, len(links))
	for index, link := range links {
		out = append(out, reference.StructuredLinkResolution{LinkIndex: index, Candidates: inventory.resolve(sourcePath, link)})
	}
	return out
}

func (inventory *identifierLinkResolutionInventory) resolve(sourcePath string, link obsidian.StructuredLink) []ontology.NodeRef {
	if link.Path == "" {
		key, _ := canonicalAuthoredPathKey(sourcePath)
		return inventory.withFragment(inventory.paths[key], link.Fragment)
	}
	var candidates []ontology.NodeRef
	if link.Kind == obsidian.StructuredLinkMarkdown || relativeWikiPath(link) {
		candidatePath := strings.ReplaceAll(link.Path, "\\", "/")
		if strings.HasPrefix(candidatePath, "/") {
			return nil
		}
		candidatePath = path.Join(path.Dir(sourcePath), candidatePath)
		key, contained := canonicalAuthoredPathKey(candidatePath)
		if !contained {
			return nil
		}
		candidates = inventory.paths[key]
		if len(candidates) == 0 {
			if key, ok := canonicalAuthoredPathKey(path.Base(strings.ReplaceAll(link.Path, "\\", "/"))); ok {
				candidates = inventory.paths[key]
			}
		}
	} else if key, ok := canonicalAuthoredPathKey(link.Path); ok {
		candidates = inventory.paths[key]
	}
	if len(candidates) == 0 {
		candidates = inventory.identities[IdentifierComparisonKey(link.Path)]
		if len(candidates) > 0 && link.Fragment == "" {
			return cloneCandidateRefs(candidates)
		}
	}
	return inventory.withFragment(candidates, link.Fragment)
}

func relativeWikiPath(link obsidian.StructuredLink) bool {
	if link.Kind != obsidian.StructuredLinkWikilink {
		return false
	}
	value := strings.ReplaceAll(strings.TrimSpace(link.Path), "\\", "/")
	return strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../")
}

func (inventory *identifierLinkResolutionInventory) withFragment(candidates []ontology.NodeRef, fragment string) []ontology.NodeRef {
	if strings.TrimSpace(fragment) == "" {
		return cloneCandidateRefs(candidates)
	}
	if len(candidates) > 1 {
		return cloneCandidateRefs(candidates)
	}
	var out []ontology.NodeRef
	fragmentKey := canonicalFragmentKey(fragment)
	for _, candidate := range candidates {
		noteKey, ok := canonicalAuthoredPathKey(candidate.NotePath)
		if !ok {
			continue
		}
		key := noteKey + "\x00" + fragmentKey
		for _, ref := range inventory.fragments[key] {
			out = appendUniqueFieldCandidate(out, ref)
		}
	}
	if rewritten := inventory.rewrittenCandidates(out); len(rewritten) > 0 {
		return rewritten
	}
	// A fragment without its own rekey still belongs to the resolved note.
	// Keep the root identity when its governed basename/alias changes so the
	// path edit is not lost merely because the authored target has a fragment.
	if rewritten := inventory.rewrittenCandidates(candidates); len(rewritten) > 0 {
		return rewritten
	}
	return out
}

func (inventory *identifierLinkResolutionInventory) rewrittenCandidates(candidates []ontology.NodeRef) []ontology.NodeRef {
	var out []ontology.NodeRef
	for _, candidate := range candidates {
		if _, found := inventory.rewriteRefs[repairRefKey(candidate)]; found {
			out = append(out, candidate)
		}
	}
	return out
}

func cloneCandidateRefs(input []ontology.NodeRef) []ontology.NodeRef {
	return append([]ontology.NodeRef(nil), input...)
}

func canonicalAuthoredPathKey(value string) (string, bool) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "./")
	cleaned := path.Clean(value)
	if value == "" || cleaned == "." || strings.HasPrefix(cleaned, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return strings.ToLower(cleaned), true
}

func canonicalFragmentKey(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	kind := "heading:"
	if strings.HasPrefix(value, "^") {
		kind = "block:"
		value = strings.TrimPrefix(value, "^")
	}
	return kind + IdentifierComparisonKey(value)
}

func linkClaimKey(value string, ref ontology.NodeRef) string {
	return IdentifierComparisonKey(value) + "\x00" + repairRefKey(ref)
}

func applyAmbiguousLinkProvenance(ctx context.Context, gitRoot string, work []identifierLinkSourceWork, inventory *identifierLinkResolutionInventory, resolve func(context.Context, string, []AmbiguousLinkProvenanceRequest) (AmbiguousLinkProvenanceResult, error)) ([]AmbiguousLinkProvenanceDecision, error) {
	if gitRoot == "" || resolve == nil {
		return nil, nil
	}
	type boundRequest struct {
		workIndex       int
		resolutionIndex int
		request         AmbiguousLinkProvenanceRequest
		key             string
	}
	var bound []boundRequest
	for workIndex := range work {
		for resolutionIndex, resolution := range work[workIndex].resolutions {
			if len(resolution.Candidates) < 2 {
				continue
			}
			if len(inventory.rewrittenCandidates(resolution.Candidates)) == 0 {
				continue
			}
			link := work[workIndex].scan.Links[resolution.LinkIndex]
			if link.Fragment != "" || link.Path != link.Target || strings.ContainsAny(link.Path, "/\\") {
				continue
			}
			claims := make([]Claim, 0, len(resolution.Candidates))
			for _, candidate := range resolution.Candidates {
				claim, ok := inventory.claims[linkClaimKey(link.Path, candidate)]
				if !ok {
					claims = nil
					break
				}
				claims = append(claims, claim)
			}
			if len(claims) != len(resolution.Candidates) {
				continue
			}
			request := AmbiguousLinkProvenanceRequest{
				NotePath: work[workIndex].source.Path.String(), SourceHash: work[workIndex].source.ContentHash,
				Content: work[workIndex].source.Content, LinkSnapshot: &work[workIndex].scan,
				LinkIndex: resolution.LinkIndex, RawTarget: link.Target, Candidates: claims,
			}
			normalized, err := normalizeAmbiguousLinkRequest(request)
			if err != nil {
				continue
			}
			bound = append(bound, boundRequest{workIndex: workIndex, resolutionIndex: resolutionIndex, request: request, key: normalized.key})
		}
	}
	if len(bound) == 0 {
		return nil, nil
	}
	requests := make([]AmbiguousLinkProvenanceRequest, len(bound))
	for index := range bound {
		requests[index] = bound[index].request
	}
	result, err := resolve(ctx, gitRoot, requests)
	if err != nil {
		return nil, err
	}
	decisions := make([]AmbiguousLinkProvenanceDecision, 0, len(bound))
	for _, item := range bound {
		decision, ok := result.Decisions[item.key]
		if !ok {
			continue
		}
		snapshot, err := decision.ValidatedSnapshot()
		if err != nil || !decisionMatchesAmbiguousRequest(snapshot, item.request) {
			continue
		}
		work[item.workIndex].resolutions[item.resolutionIndex].Candidates = []ontology.NodeRef{snapshot.Winner}
		decisions = append(decisions, *snapshot)
	}
	return decisions, nil
}

func decisionMatchesAmbiguousRequest(decision *AmbiguousLinkProvenanceDecision, request AmbiguousLinkProvenanceRequest) bool {
	if decision == nil || decision.NotePath != request.NotePath || decision.SourceHash != request.SourceHash || decision.LinkIndex != request.LinkIndex || decision.RawTarget != request.RawTarget {
		return false
	}
	normalized, err := normalizeAmbiguousLinkRequest(request)
	if err != nil || len(normalized.candidateRefs) != len(decision.CandidateRefs) {
		return false
	}
	winner := false
	for index := range normalized.candidateRefs {
		if !sameRepairRef(normalized.candidateRefs[index], decision.CandidateRefs[index]) {
			return false
		}
		winner = winner || sameRepairRef(normalized.candidateRefs[index], decision.Winner)
	}
	return winner
}
