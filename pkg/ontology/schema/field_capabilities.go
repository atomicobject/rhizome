package schema

import "strings"

type FieldQueryCapability struct {
	FieldName         string
	ValueKind         string
	FilterOps         []string
	IndexedFilterOps  []string
	ResidualFilterOps []string
	Sortable          bool
	Groupable         bool
	TargetType        string
	EnumName          string
}

func FieldQueryCapabilityForField(schema *Schema, field *Field) (FieldQueryCapability, bool) {
	if field == nil || strings.TrimSpace(field.Name) == "" {
		return FieldQueryCapability{}, false
	}
	var capability FieldQueryCapability
	switch field.Kind {
	case FieldKindScalar:
		valueKind := string(ScalarKindForType(field.TypeName))
		capability = FieldQueryCapability{
			FieldName:        strings.TrimSpace(field.Name),
			ValueKind:        valueKind,
			IndexedFilterOps: indexedScalarFilterOps(valueKind),
			Sortable:         indexedScalarSortable(valueKind) && !field.List,
			Groupable:        !field.List,
		}
	case FieldKindEnum:
		capability = FieldQueryCapability{
			FieldName:        strings.TrimSpace(field.Name),
			ValueKind:        "enum",
			IndexedFilterOps: []string{"eq", "in", "exists"},
			Sortable:         !field.List,
			Groupable:        true,
			EnumName:         strings.TrimSpace(field.TypeName),
		}
	case FieldKindLink:
		capability = FieldQueryCapability{
			FieldName:        strings.TrimSpace(field.Name),
			ValueKind:        "relation",
			IndexedFilterOps: []string{"eq", "in", "exists"},
			Sortable:         false,
			// Views group by link target, splitting lists like enum lists.
			Groupable:  true,
			TargetType: strings.TrimSpace(field.TypeName),
		}
	default:
		return FieldQueryCapability{}, false
	}
	capability.ResidualFilterOps = residualFilterOpsForValueKind(capability.ValueKind)
	capability.FilterOps = mergeCapabilityOps(capability.IndexedFilterOps, capability.ResidualFilterOps)
	return capability, true
}

func residualFilterOpsForValueKind(valueKind string) []string {
	switch strings.ToLower(strings.TrimSpace(valueKind)) {
	case "string", "url", "id":
		return []string{"neq", "contains"}
	default:
		return nil
	}
}

func mergeCapabilityOps(primary []string, extra []string) []string {
	out := make([]string, 0, len(primary)+len(extra))
	seen := map[string]struct{}{}
	for _, op := range append(append([]string(nil), primary...), extra...) {
		op = strings.TrimSpace(op)
		if op == "" {
			continue
		}
		if _, ok := seen[op]; ok {
			continue
		}
		seen[op] = struct{}{}
		out = append(out, op)
	}
	return out
}

func indexedScalarFilterOps(valueKind string) []string {
	switch valueKind {
	case "bool":
		return []string{"eq", "exists"}
	case "int", "real", "date", "datetime":
		return []string{"eq", "in", "exists", "gt", "gte", "lt", "lte"}
	default:
		return []string{"eq", "in", "exists"}
	}
}

func indexedScalarSortable(valueKind string) bool {
	switch valueKind {
	case "bool":
		return false
	default:
		return true
	}
}
