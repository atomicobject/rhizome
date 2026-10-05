package noderead

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// NormalizeRef trims author-facing locator fields and infers embedded kind
// from fragment-bearing refs when callers have not set Kind explicitly.
func NormalizeRef(ref ontology.NodeRef) ontology.NodeRef {
	ref.NotePath = strings.TrimSpace(ref.NotePath)
	ref.Fragment = strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	if ref.Kind == "" && ref.Fragment != "" {
		ref.Kind = ontology.NodeKindEmbedded
	}
	return ref
}

func normalizeNodeRef(ref ontology.NodeRef) ontology.NodeRef {
	return NormalizeRef(ref)
}

// projectionCacheKey keeps node identity richer than NodeRef.String(), which is
// a link locator and intentionally omits NodeID, kind, and structural fallback.
func projectionCacheKey(ref ontology.NodeRef) string {
	ref = normalizeNodeRef(ref)
	kind := ref.Kind
	if kind == "" && ref.Fragment == "" && ref.NodeID == "" && ref.Structural == "" {
		kind = ontology.NodeKindNote
	}
	return strings.Join([]string{
		ref.NotePath,
		ref.Fragment,
		strings.TrimSpace(ref.NodeID),
		string(kind),
		strings.TrimSpace(ref.Structural),
	}, "|")
}

// RefIdentityKey returns the cache identity for a NodeRef.
//
// IMPORTANT: NodeRef.String is a locator, not a complete identity; it can omit
// kind, node ID, or structural fallback data that distinguishes projections.
func RefIdentityKey(ref ontology.NodeRef) string {
	return projectionCacheKey(ref)
}

func nodeRefIdentityKey(ref ontology.NodeRef) string {
	return RefIdentityKey(ref)
}

func nodeRefResultKey(ref ontology.NodeRef) string {
	ref = normalizeNodeRef(ref)
	if ref.NodeID != "" || ref.Structural != "" {
		return nodeRefIdentityKey(ref)
	}
	return ref.String()
}

func canonicalNodeRefFromLocator(ref ontology.NodeRef, locator *ontology.NodeLocator) ontology.NodeRef {
	ref = normalizeNodeRef(ref)
	if locator == nil {
		return ref
	}
	if locator.Ref.NotePath != "" {
		ref = normalizeNodeRef(locator.Ref)
	}
	if locator.LinkTarget != nil {
		targetRef := normalizeNodeRef(locator.LinkTarget.Ref)
		if targetRef.NotePath != "" {
			ref = targetRef
		}
		if locator.LinkTarget.BlockID != "" && ref.Fragment == "" && locator.LinkTarget.Exists {
			ref.Fragment = "^" + strings.TrimPrefix(strings.TrimSpace(locator.LinkTarget.BlockID), "^")
		}
	}
	return ref
}

func provenanceCacheKey(provenance map[string]struct{}) string {
	if len(provenance) == 0 {
		return ""
	}
	values := make([]string, 0, len(provenance))
	for value := range provenance {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return strings.Join(values, "\x00")
}

func hasEdgeGroupsForSources(grouped map[string][]semdb.OntologyEdgeRow, sources []string) bool {
	if grouped == nil {
		return false
	}
	for _, source := range sources {
		if _, ok := grouped[source]; !ok {
			return false
		}
	}
	return true
}

func cloneTraverseResultForSources(result TraverseResult, sources []string) TraverseResult {
	out := TraverseResult{EdgesBySource: make(map[string][]semdb.OntologyEdgeRow, len(sources))}
	for _, source := range sources {
		out.EdgesBySource[source] = cloneEdgeRows(result.EdgesBySource[source])
	}
	return out
}

func limitTraverseResult(result TraverseResult, limitPerSource int) {
	if limitPerSource <= 0 {
		return
	}
	for source, rows := range result.EdgesBySource {
		result.EdgesBySource[source] = dedupeEdges(rows, limitPerSource)
	}
}

func cloneEdgeRows(rows []semdb.OntologyEdgeRow) []semdb.OntologyEdgeRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]semdb.OntologyEdgeRow, len(rows))
	copy(out, rows)
	return out
}

func cloneTypeListResult(result ontology.TypeListResult) ontology.TypeListResult {
	out := result
	if result.TypeDoc != nil {
		doc := *result.TypeDoc
		out.TypeDoc = &doc
	}
	if len(result.Items) > 0 {
		out.Items = append([]ontology.NodeListItem(nil), result.Items...)
	}
	return out
}

func cloneNodeRecord(record NodeRecord) NodeRecord {
	record.Frontmatter = cloneMap(record.Frontmatter)
	record.InlineProps = cloneInlineProps(record.InlineProps)
	record.FieldValues = cloneRecordFieldValues(record.FieldValues)
	record.Tags = cloneStrings(record.Tags)
	record.Capabilities = append([]noteformat.Capability(nil), record.Capabilities...)
	if record.LinkTarget != nil {
		target := *record.LinkTarget
		record.LinkTarget = &target
	}
	if record.NodeLocator != nil {
		locator := *record.NodeLocator
		if locator.LinkTarget != nil {
			target := *locator.LinkTarget
			locator.LinkTarget = &target
		}
		record.NodeLocator = &locator
	}
	return record
}

func cloneRecordFieldValues(in map[string][]codeanchor.IntelOntologyNodeFieldValue) map[string][]codeanchor.IntelOntologyNodeFieldValue {
	if in == nil {
		return nil
	}
	if len(in) == 0 {
		return map[string][]codeanchor.IntelOntologyNodeFieldValue{}
	}
	out := make(map[string][]codeanchor.IntelOntologyNodeFieldValue, len(in))
	for key, values := range in {
		out[key] = append([]codeanchor.IntelOntologyNodeFieldValue(nil), values...)
	}
	return out
}

func cloneInlineProps(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = cloneStrings(values)
	}
	return out
}

func dedupeEdges(rows []semdb.OntologyEdgeRow, limit int) []semdb.OntologyEdgeRow {
	seen := make(map[string]struct{}, len(rows))
	out := make([]semdb.OntologyEdgeRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.DstPath]; ok {
			continue
		}
		seen[row.DstPath] = struct{}{}
		out = append(out, row)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func propertyValue(row semdb.NotePropertyValueRow) any {
	switch row.ValueKind {
	case semdb.NotePropertyValueBool:
		return strings.EqualFold(strings.TrimSpace(row.ValueText), "true")
	case semdb.NotePropertyValueInt:
		if value, err := strconv.ParseInt(strings.TrimSpace(row.ValueText), 10, 64); err == nil {
			return int(value)
		}
	case semdb.NotePropertyValueFloat:
		if value, err := strconv.ParseFloat(strings.TrimSpace(row.ValueText), 64); err == nil {
			return value
		}
	}
	return row.ValueText
}

func noteTitle(notePath string, fm map[string]any) string {
	if title := stringValue(fm["title"]); title != "" {
		return title
	}
	base := filepath.Base(notePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func normalizeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// extractTags keeps only persisted frontmatter tag facts. Inline hashtag
// extraction belongs to the selected note-format projector, not noderead.
func extractTags(fm map[string]any) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, value := range flattenValueStrings(fm["tags"]) {
		value = strings.TrimPrefix(value, "#")
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func flattenValueStrings(v any) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case []string:
		return normalizeStrings(val)
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, flattenValueStrings(item)...)
		}
		return normalizeStrings(out)
	case string:
		return normalizeStrings([]string{val})
	case bool:
		return []string{strconv.FormatBool(val)}
	case int:
		return []string{strconv.Itoa(val)}
	case int64:
		return []string{strconv.FormatInt(val, 10)}
	case float64:
		return []string{strconv.FormatFloat(val, 'f', -1, 64)}
	case time.Time:
		if val.Hour() == 0 && val.Minute() == 0 && val.Second() == 0 {
			return []string{val.Format("2006-01-02")}
		}
		return []string{val.Format(time.RFC3339)}
	default:
		return []string{strings.TrimSpace(fmt.Sprint(v))}
	}
}
