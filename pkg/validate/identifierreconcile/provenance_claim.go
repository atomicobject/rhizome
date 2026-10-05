package identifierreconcile

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type historicalDocument struct {
	snapshot *ontology.DocumentSnapshot
	err      error
	absent   bool
}

// parseHistoricalMarkdownDocument is the historical Git compatibility parser
// for claims from the Markdown repair inventory. It is not a current-provider
// parser: current source discovery selects exact Markdown FormatID before a
// claim reaches Git provenance, and Git history is then read only as Markdown
// blob evidence for that claim.
func parseHistoricalMarkdownDocument(notePath string, blob []byte) historicalDocument {
	snapshot, err := ontology.BuildDocumentSnapshot(notePath, string(blob), time.Time{})
	if err == nil && snapshot != nil && snapshot.Frontmatter == nil && bytes.HasPrefix(blob, []byte("---")) {
		_, err = obsidian.ExtractFrontmatter(string(blob))
	}
	return historicalDocument{snapshot: snapshot, err: err}
}

func evidenceForClaim(claim Claim, events []gitHistoryEvent, documents []historicalDocument, schema *ontology.Schema) ProvenanceEvidence {
	target := IdentifierComparisonKey(claim.Value)
	values := make([]string, len(events))
	for index := range events {
		extracted := identifierValueFromDocument(documents[index], claim, schema)
		if !extracted.complete {
			return ProvenanceEvidence{Reason: extracted.reason}
		}
		if extracted.present {
			values[index] = IdentifierComparisonKey(extracted.value)
		}
	}
	if len(values) == 0 || values[0] != target {
		return ProvenanceEvidence{Reason: "current identifier claim is not committed"}
	}
	introduction := 0
	for index := 1; index < len(values); index++ {
		if values[index] != target {
			break
		}
		introduction = index
	}
	oldest := events[introduction]
	if introduction == len(events)-1 && oldest.status != 'A' && len(oldest.parents) > 0 {
		return ProvenanceEvidence{Reason: "identifier introduction history incomplete"}
	}
	return ProvenanceEvidence{
		AuthorDate: time.Unix(oldest.author, 0).UTC(),
		FullOID:    strings.ToLower(oldest.oid),
		Complete:   true,
	}
}

type claimBlobValue struct {
	value    string
	present  bool
	complete bool
	reason   string
}

func completeClaimValue(value string, present bool) claimBlobValue {
	return claimBlobValue{value: value, present: present, complete: true}
}

func incompleteClaimValue(reason string) claimBlobValue {
	return claimBlobValue{reason: reason}
}

func identifierValueFromDocument(document historicalDocument, claim Claim, schema *ontology.Schema) claimBlobValue {
	if document.absent {
		return completeClaimValue("", false)
	}
	if document.err != nil || document.snapshot == nil {
		return incompleteClaimValue("historical markdown or frontmatter is malformed")
	}
	snapshot := document.snapshot
	if strings.TrimSpace(claim.Node.Fragment) != "" {
		if schema == nil {
			return incompleteClaimValue("embedded identifier provenance requires ontology schema")
		}
		projection, err := ontology.ProjectNodeFromSnapshot(snapshot, schema, ontology.NodeRef{
			NotePath: snapshot.NotePath,
			Fragment: claim.Node.Fragment,
			TypeName: claim.Node.TypeName,
			Kind:     ontology.NodeKindEmbedded,
		})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return findHistoricalEmbeddedClaim(snapshot, schema, claim)
			}
			return incompleteClaimValue("historical embedded locator is ambiguous or unreadable")
		}
		if projection == nil || projection.ResolvedType != claim.Node.TypeName {
			return incompleteClaimValue("historical embedded locator resolved to a different type")
		}
		if claim.Kind == ClaimAlias {
			return exactAliasMembership(embeddedAliasValues(projection), claim.Value)
		}
		binding, ok := projection.Fields[claim.Node.IdentifierField]
		if !ok {
			return completeClaimValue("", false)
		}
		if binding.Derived {
			return incompleteClaimValue("historical identifier claim is structurally derived")
		}
		return oneNormalizedIdentifier(binding.Values)
	}
	if schema == nil {
		return incompleteClaimValue("note identifier provenance requires ontology schema")
	}
	noteProjection, err := ontology.ProjectNodeFromSnapshot(snapshot, schema, ontology.NodeRef{
		NotePath: snapshot.NotePath,
		TypeName: claim.Node.TypeName,
		Kind:     ontology.NodeKindNote,
	})
	if err != nil {
		return incompleteClaimValue("historical note type is unreadable")
	}
	if noteProjection == nil || noteProjection.ResolvedType != claim.Node.TypeName {
		return completeClaimValue("", false)
	}

	frontmatter := snapshot.Frontmatter
	if frontmatter == nil {
		return completeClaimValue("", false)
	}
	if claim.Kind == ClaimAlias {
		raw, matches := caseInsensitiveMapValue(frontmatter, "aliases")
		if matches == 0 {
			return completeClaimValue("", false)
		}
		if matches != 1 {
			return incompleteClaimValue("historical aliases field is ambiguous")
		}
		aliases, ok := normalizedStringList(raw)
		if !ok {
			return incompleteClaimValue("historical aliases field has an unsupported or ambiguous shape")
		}
		return exactAliasMembership(aliases, claim.Value)
	}
	fieldNames := []string{claim.Node.IdentifierField}
	if schema != nil {
		if noteType := schema.Types[claim.Node.TypeName]; noteType != nil {
			if field := noteType.ByName[claim.Node.IdentifierField]; field != nil {
				fieldNames = append([]string{field.Source}, field.SourceAliases...)
			}
		}
	}
	values := make(map[string]struct{})
	matched := false
	for key, raw := range frontmatter {
		if !matchesAnyFieldSource(key, fieldNames) {
			continue
		}
		matched = true
		var value string
		switch typed := raw.(type) {
		case string:
			value = typed
		case int, int64, float64, bool:
			value = fmt.Sprint(typed)
		default:
			return incompleteClaimValue("historical identifier field has an unsupported shape")
		}
		if value = NormalizeIdentifierValue(value); value != "" {
			values[value] = struct{}{}
		}
	}
	if !matched {
		return completeClaimValue("", false)
	}
	if len(values) != 1 {
		return incompleteClaimValue("historical identifier field is ambiguous")
	}
	for value := range values {
		return completeClaimValue(value, true)
	}
	return completeClaimValue("", false)
}

func findHistoricalEmbeddedClaim(snapshot *ontology.DocumentSnapshot, schema *ontology.Schema, claim Claim) claimBlobValue {
	refByNodeID := make(map[string]ontology.NodeRef, len(snapshot.SectionsByID)+len(snapshot.SourceSpansByID))
	for nodeID := range snapshot.SectionsByID {
		refByNodeID[nodeID] = ontology.NodeRef{NotePath: snapshot.NotePath, NodeID: nodeID, Kind: ontology.NodeKindEmbedded}
	}
	for nodeID := range snapshot.SourceSpansByID {
		refByNodeID[nodeID] = ontology.NodeRef{NotePath: snapshot.NotePath, NodeID: nodeID, Kind: ontology.NodeKindEmbedded}
	}
	nodeIDs := make([]string, 0, len(refByNodeID))
	for nodeID := range refByNodeID {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	refs := make([]ontology.NodeRef, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		refs = append(refs, refByNodeID[nodeID])
	}
	projections, err := ontology.ProjectNodesFromSnapshot(snapshot, schema, refs)
	if err != nil {
		return incompleteClaimValue("historical embedded claim search is unreadable")
	}
	matches := 0
	for _, projection := range projections {
		if projection == nil || projection.ResolvedType != claim.Node.TypeName {
			continue
		}
		var extracted claimBlobValue
		if claim.Kind == ClaimAlias {
			extracted = exactAliasMembership(embeddedAliasValues(projection), claim.Value)
		} else if binding, ok := projection.Fields[claim.Node.IdentifierField]; ok && !binding.Derived {
			extracted = oneNormalizedIdentifier(binding.Values)
		} else {
			continue
		}
		if !extracted.complete {
			return extracted
		}
		if extracted.present && IdentifierComparisonKey(extracted.value) == IdentifierComparisonKey(claim.Value) {
			matches++
		}
	}
	if matches > 1 {
		return incompleteClaimValue("historical embedded identifier claim is ambiguous")
	}
	return completeClaimValue(NormalizeIdentifierValue(claim.Value), matches == 1)
}

func oneNormalizedIdentifier(values []string) claimBlobValue {
	unique := make(map[string]struct{})
	for _, raw := range values {
		if value := NormalizeIdentifierValue(raw); value != "" {
			unique[value] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return completeClaimValue("", false)
	}
	if len(unique) != 1 {
		return incompleteClaimValue("historical identifier field is ambiguous")
	}
	for value := range unique {
		return completeClaimValue(value, true)
	}
	return completeClaimValue("", false)
}

func embeddedAliasValues(projection *ontology.NodeProjection) []string {
	values := make([]string, 0)
	for name, binding := range projection.Fields {
		if strings.EqualFold(strings.TrimSpace(name), "alias") || strings.EqualFold(strings.TrimSpace(name), "aliases") {
			values = append(values, binding.Values...)
		}
	}
	return values
}

func exactAliasMembership(values []string, target string) claimBlobValue {
	target = IdentifierComparisonKey(target)
	matches := 0
	for _, value := range values {
		if IdentifierComparisonKey(value) == target {
			matches++
		}
	}
	if matches > 1 {
		return incompleteClaimValue("historical alias claim is ambiguous")
	}
	return completeClaimValue(target, matches == 1)
}

func caseInsensitiveMapValue(values map[string]any, key string) (any, int) {
	var found any
	matches := 0
	for current, value := range values {
		if strings.EqualFold(strings.TrimSpace(current), strings.TrimSpace(key)) {
			found = value
			matches++
		}
	}
	return found, matches
}

func normalizedStringList(raw any) ([]string, bool) {
	switch value := raw.(type) {
	case string:
		return []string{NormalizeIdentifierValue(value)}, true
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, NormalizeIdentifierValue(item))
		}
		return out, true
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, NormalizeIdentifierValue(text))
		}
		return out, true
	default:
		return nil, false
	}
}

func matchesAnyFieldSource(key string, sources []string) bool {
	for _, source := range sources {
		if strings.TrimSpace(source) != "" && strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(source)) {
			return true
		}
	}
	return false
}
