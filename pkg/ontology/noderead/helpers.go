package noderead

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

// RefIdentityKey preserves the node kind, catalog identity, and structural selector.
func RefIdentityKey(ref ontology.NodeRef) string {
	ref = NormalizeRef(ref)
	kind := ref.Kind
	if kind == "" && ref.Fragment == "" && ref.NodeID == "" && ref.Structural == "" {
		kind = ontology.NodeKindNote
	}
	return strings.Join([]string{ref.NotePath, ref.Fragment, strings.TrimSpace(ref.NodeID), string(kind), strings.TrimSpace(ref.Structural)}, "|")
}

func nodeRefResultKey(ref ontology.NodeRef) string {
	ref = NormalizeRef(ref)
	if ref.NodeID != "" || ref.Structural != "" {
		return RefIdentityKey(ref)
	}
	return ref.String()
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
	record.resolvedFrom = append([]ontology.NodeRef(nil), record.resolvedFrom...)
	fields := make(map[string]projectedFieldValues, len(record.projectedFields))
	for name, field := range record.projectedFields {
		field.values = cloneStrings(field.values)
		fields[name] = field
	}
	record.projectedFields = fields
	record.Capabilities = append([]noteformat.Capability(nil), record.Capabilities...)
	if record.LinkTarget != nil {
		target := *record.LinkTarget
		record.LinkTarget = &target
	}
	if record.NodeLocator != nil {
		locator := cloneNodeLocator(*record.NodeLocator)
		record.NodeLocator = &locator
	}
	return record
}

func cloneNodeLocator(locator ontology.NodeLocator) ontology.NodeLocator {
	locator.Diagnostics = append([]ontology.NodeLinkDiagnostic(nil), locator.Diagnostics...)
	locator.FixActions = append([]ontology.NodeLinkFixAction(nil), locator.FixActions...)
	if locator.LinkTarget != nil {
		target := *locator.LinkTarget
		locator.LinkTarget = &target
	}
	return locator
}

func cloneRecordFieldValues(in map[string][]readmodel.FieldValueRow) map[string][]readmodel.FieldValueRow {
	if in == nil {
		return nil
	}
	if len(in) == 0 {
		return map[string][]readmodel.FieldValueRow{}
	}
	out := make(map[string][]readmodel.FieldValueRow, len(in))
	for key, values := range in {
		out[key] = append([]readmodel.FieldValueRow(nil), values...)
		for i := range out[key] {
			row := &out[key][i]
			row.ValueBool = clonePointer(row.ValueBool)
			row.ValueInt = clonePointer(row.ValueInt)
			row.ValueReal = clonePointer(row.ValueReal)
			row.ValueDate = clonePointer(row.ValueDate)
			row.ValueDateTime = clonePointer(row.ValueDateTime)
		}
	}
	return out
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
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
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
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
