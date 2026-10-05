package ontology

import "strings"

type IndexedFieldCapability struct {
	FieldName  string
	ValueKind  string
	FilterOps  []string
	Sortable   bool
	Groupable  bool
	TargetType string
	EnumName   string
	Editable   bool
}

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
	Editable          bool
}

func IndexedFieldCapabilitiesForType(schema *Schema, typeName string) []IndexedFieldCapability {
	if schema == nil {
		return nil
	}
	noteType := schema.Types[strings.TrimSpace(typeName)]
	if noteType == nil {
		return nil
	}
	out := make([]IndexedFieldCapability, 0, len(noteType.Fields))
	for _, field := range noteType.Fields {
		if cap, ok := IndexedFieldCapabilityForField(schema, field); ok {
			out = append(out, cap)
		}
	}
	return out
}

func FieldQueryCapabilityForField(schema *Schema, field *Field) (FieldQueryCapability, bool) {
	indexed, ok := IndexedFieldCapabilityForField(schema, field)
	if !ok {
		return FieldQueryCapability{}, false
	}
	residual := residualFilterOpsForValueKind(indexed.ValueKind)
	return FieldQueryCapability{
		FieldName:         indexed.FieldName,
		ValueKind:         indexed.ValueKind,
		FilterOps:         mergeCapabilityOps(indexed.FilterOps, residual),
		IndexedFilterOps:  append([]string(nil), indexed.FilterOps...),
		ResidualFilterOps: residual,
		Sortable:          indexed.Sortable,
		Groupable:         indexed.Groupable,
		TargetType:        indexed.TargetType,
		EnumName:          indexed.EnumName,
		Editable:          indexed.Editable,
	}, true
}

func IndexedFieldCapabilityForField(schema *Schema, field *Field) (IndexedFieldCapability, bool) {
	if field == nil || strings.TrimSpace(field.Name) == "" {
		return IndexedFieldCapability{}, false
	}
	switch field.Kind {
	case FieldKindScalar:
		valueKind := indexedScalarValueKind(field.TypeName)
		return IndexedFieldCapability{
			FieldName: strings.TrimSpace(field.Name),
			ValueKind: valueKind,
			FilterOps: indexedScalarFilterOps(valueKind),
			Sortable:  indexedScalarSortable(valueKind) && !field.List,
			Groupable: !field.List,
			Editable:  true,
		}, true
	case FieldKindEnum:
		return IndexedFieldCapability{
			FieldName: strings.TrimSpace(field.Name),
			ValueKind: "enum",
			FilterOps: []string{"eq", "in", "exists"},
			Sortable:  !field.List,
			Groupable: true,
			EnumName:  strings.TrimSpace(field.TypeName),
			Editable:  true,
		}, true
	case FieldKindLink:
		return IndexedFieldCapability{
			FieldName: strings.TrimSpace(field.Name),
			ValueKind: "relation",
			FilterOps: []string{"eq", "in", "exists"},
			Sortable:  false,
			// Views group by link target, splitting lists like enum lists.
			Groupable:  true,
			TargetType: strings.TrimSpace(field.TypeName),
			Editable:   !field.List,
		}, true
	default:
		return IndexedFieldCapability{}, false
	}
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

func indexedScalarValueKind(typeName string) string {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "boolean", "bool":
		return "bool"
	case "int", "integer":
		return "int"
	case "float", "number":
		return "real"
	case "date":
		return "date"
	case "datetime":
		return "datetime"
	case "url":
		return "url"
	case "id":
		return "id"
	default:
		return "string"
	}
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
