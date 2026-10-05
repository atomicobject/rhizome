package mcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
)

func compactSemanticQueryResponse(resp semanticQueryResponse) semanticQueryResponse {
	if resp.Compact != nil {
		return resp
	}

	sources := make([]SemanticCompactSource, 0, len(resp.Matches))
	refs := make(map[string]struct{}, len(resp.Matches))
	sourceIndexes := make(map[string]int, len(resp.Matches))
	for index, match := range resp.Matches {
		ref := semanticMatchKey(match)
		if ref == "" {
			ref = "result:" + compactResultRef(index)
		}
		source := SemanticCompactSource{
			Ref:         ref,
			Type:        match.Type,
			Path:        match.Path,
			Title:       match.Title,
			Symbol:      match.Symbol,
			FQN:         match.FQN,
			Kind:        match.Kind,
			Granularity: match.Granularity,
			StartLine:   match.StartLine,
			EndLine:     match.EndLine,
			Score:       match.Score,
			Evidence:    maps.Clone(match.Evidence),
			Included:    match.Included,
			LinkTarget:  match.LinkTarget,
		}
		if body := compactSourceBody(match); body != nil {
			source.Body = body
		}
		if existing, ok := sourceIndexes[ref]; ok {
			if sources[existing].Body == nil && source.Body != nil {
				sources[existing].Body = source.Body
				sources[existing].Included = source.Included
			}
			continue
		}
		refs[ref] = struct{}{}
		sourceIndexes[ref] = len(sources)
		sources = append(sources, source)
	}

	resp.Compact = &SemanticCompactResponse{
		Sources: sources,
		Roles: SemanticCompactRoles{
			MustRead:   compactRoleRefs(resp.MustRead, refs),
			Supporting: compactRoleRefs(resp.Supporting, refs),
		},
	}
	// The detailed packet repeats match source bodies in Text and answer previews.
	// Compact sources above own that content, while the top-level diagnostics and
	// pagination fields remain unchanged.
	resp.Text = ""
	resp.Matches = nil
	resp.MustRead = nil
	resp.Supporting = nil
	return resp
}

func compactSourceBody(match SemanticMatchPayload) *SemanticCompactSourceBody {
	body := ""
	switch match.ContentKind {
	case string(planFull):
		body = match.FullFileContent
	case string(planExcerpt):
		body = match.ExcerptContent
	case string(planOutline):
		body = match.OutlineContent
	case string(planSignature):
		body = match.SignatureContent
	case string(planStub):
		body = match.StubContent
	}
	if match.ContentKind == "" && !match.ContentDeduped {
		return nil
	}
	return &SemanticCompactSourceBody{
		Kind:      match.ContentKind,
		Content:   body,
		Truncated: match.ContentTruncated,
		Deduped:   match.ContentDeduped,
	}
}

func compactRoleRefs(items []answer.Item, refs map[string]struct{}) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		ref := semanticMatchKey(SemanticMatchPayload{Type: item.Type, Path: item.Path})
		if ref == "" {
			continue
		}
		if _, ok := refs[ref]; !ok {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func compactResultRef(index int) string {
	return strconv.Itoa(index + 1)
}

func marshalCompactSemanticQueryResponse(resp semanticQueryResponse, budgetChars int) ([]byte, error) {
	resp = cloneCompactSemanticQueryResponse(resp)
	encode := func() ([]byte, error) { return json.Marshal(resp) }
	if encoded, err := encode(); err != nil || len(encoded) <= budgetChars {
		return encoded, err
	}

	// Compact roles and sources already carry the actionable answer. Remove
	// duplicated presentation and aggregate diagnostics before shortening the
	// selected source body that the caller explicitly budgeted for.
	resp.TypeCounts = nil
	resp.PackMeta = nil
	resp.NextQueries = nil
	resp.Summary = ""
	if encoded, err := encode(); err != nil || len(encoded) <= budgetChars {
		return encoded, err
	}

	for _, limit := range []int{320, 80, 0} {
		for i := range resp.Compact.Sources {
			body := resp.Compact.Sources[i].Body
			if body == nil || body.Content == "" {
				continue
			}
			if limit <= 0 {
				body.Content = ""
				body.Truncated = true
				continue
			}
			if len(body.Content) > limit {
				body.Content = contextpack.TrimToBudget(body.Content, limit)
				body.Truncated = true
			}
		}
		if encoded, err := encode(); err != nil || len(encoded) <= budgetChars {
			return encoded, err
		}
	}

	resp.TargetCandidates = nil
	resp.Coverage = answer.CoverageReport{}
	resp.Confidence = answer.ConfidenceReport{}
	if encoded, err := encode(); err != nil || len(encoded) <= budgetChars {
		return encoded, err
	}

	originalSourceCount := len(resp.Compact.Sources)
	initialOmittedSources := resp.Compact.OmittedSources
	for len(resp.Compact.Sources) > 0 {
		resp.Compact.Sources = resp.Compact.Sources[:len(resp.Compact.Sources)-1]
		resp.Compact.OmittedSources = initialOmittedSources + originalSourceCount - len(resp.Compact.Sources)
		resp.Compact.Roles = compactRolesForSources(resp.Compact.Roles, resp.Compact.Sources)
		if encoded, err := encode(); err != nil || len(encoded) <= budgetChars {
			return encoded, err
		}
	}

	return nil, fmt.Errorf("compact response exceeds budget %d while preserving pagination and diagnostics", budgetChars)
}

func cloneCompactSemanticQueryResponse(resp semanticQueryResponse) semanticQueryResponse {
	if resp.Compact == nil {
		return resp
	}
	clone := *resp.Compact
	clone.Sources = make([]SemanticCompactSource, len(resp.Compact.Sources))
	copy(clone.Sources, resp.Compact.Sources)
	clone.Roles.MustRead = append([]string(nil), resp.Compact.Roles.MustRead...)
	clone.Roles.Supporting = append([]string(nil), resp.Compact.Roles.Supporting...)
	for i := range clone.Sources {
		if clone.Sources[i].Body == nil {
			continue
		}
		body := *clone.Sources[i].Body
		clone.Sources[i].Body = &body
	}
	resp.Compact = &clone
	return resp
}

func compactRolesForSources(roles SemanticCompactRoles, sources []SemanticCompactSource) SemanticCompactRoles {
	available := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		available[source.Ref] = struct{}{}
	}
	filter := func(refs []string) []string {
		out := make([]string, 0, len(refs))
		for _, ref := range refs {
			if _, ok := available[ref]; ok {
				out = append(out, ref)
			}
		}
		return out
	}
	return SemanticCompactRoles{MustRead: filter(roles.MustRead), Supporting: filter(roles.Supporting)}
}
