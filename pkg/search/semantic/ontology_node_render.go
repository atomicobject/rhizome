package semantic

import (
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

const (
	ontologyFieldMaxCount       = 6
	ontologyFieldMaxValues      = 2
	ontologyFieldValueMaxChars  = 120
	ontologyFieldsSectionMaxLen = 480
	ontologyAncestorsMaxLen     = 240
)

type ontologyFieldSignal struct {
	name     string
	priority int
	line     string
}

func selectedOntologyFieldSignalLines(schema *ontology.Schema, projection *ontology.NodeProjection, node codeanchor.IntelOntologyNode) []string {
	if projection == nil {
		return nil
	}
	var signals []ontologyFieldSignal
	for name, field := range projection.Fields {
		def := ontologyFieldDef(projection, name)
		if def == nil || (def.Kind != ontology.FieldKindScalar && def.Kind != ontology.FieldKindEnum) || strings.EqualFold(name, "body") || strings.EqualFold(name, "content") {
			continue
		}
		values := append([]string(nil), field.Values...)
		for _, span := range field.InlineSpans {
			values = append(values, span.Value)
		}
		for i, value := range values {
			values[i] = truncateDeterministic(strings.Join(strings.Fields(value), " "), ontologyFieldValueMaxChars)
		}
		values = removeOntologyIdentityDuplicates(uniqueSortedNonEmpty(values), node)
		if len(values) > ontologyFieldMaxValues {
			values = values[:ontologyFieldMaxValues]
		}
		if len(values) == 0 {
			continue
		}
		label := name
		if enumValues := compactEnumValues(schema, def.TypeName); len(enumValues) > 0 {
			label += " [" + strings.Join(enumValues, "|") + "]"
		}
		signals = append(signals, ontologyFieldSignal{name: name, priority: ontologyFieldPriority(def), line: "- " + label + ": " + strings.Join(values, ", ")})
	}
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].priority != signals[j].priority {
			return signals[i].priority < signals[j].priority
		}
		return signals[i].name < signals[j].name
	})
	if len(signals) > ontologyFieldMaxCount {
		signals = signals[:ontologyFieldMaxCount]
	}
	var lines []string
	used := 0
	for _, signal := range signals {
		addition := RuneLen(signal.line)
		if len(lines) > 0 {
			addition++
		}
		if used+addition > ontologyFieldsSectionMaxLen {
			break
		}
		lines = append(lines, signal.line)
		used += addition
	}
	return lines
}

func ontologyFieldPriority(field *ontology.Field) int {
	switch {
	case field.IsPreferredIdentifier:
		return 0
	case field.IsIdentifier:
		return 1
	case field.ContextInclude:
		return 2
	case field.Kind == ontology.FieldKindEnum:
		return 3
	case field.Required:
		return 4
	default:
		return 5
	}
}

func removeOntologyIdentityDuplicates(values []string, node codeanchor.IntelOntologyNode) []string {
	duplicates := map[string]struct{}{}
	for _, value := range []string{node.Title, node.NodeRefJSON, node.SourceLocator} {
		if value = strings.TrimSpace(value); value != "" {
			duplicates[strings.ToLower(value)] = struct{}{}
		}
	}
	out := values[:0]
	for _, value := range values {
		if _, duplicate := duplicates[strings.ToLower(strings.TrimSpace(value))]; !duplicate {
			out = append(out, value)
		}
	}
	return out
}

func renderOntologyAncestors(ancestors []ontologyAncestorFrame) string {
	if len(ancestors) == 0 {
		return ""
	}
	frames := ancestors
	if len(frames) > 3 {
		frames = []ontologyAncestorFrame{frames[0], {}, frames[len(frames)-2], frames[len(frames)-1]}
	}
	parts := make([]string, 0, len(frames))
	for _, frame := range frames {
		part := strings.Join(nonEmptyStrings(frame.Type, frame.Title, frame.Field), " > ")
		if part == "" {
			part = "…"
		}
		parts = append(parts, part)
	}
	return truncateDeterministic(strings.Join(parts, " / "), ontologyAncestorsMaxLen)
}

func ontologyFieldDef(projection *ontology.NodeProjection, name string) *ontology.Field {
	if projection == nil || projection.Type == nil {
		return nil
	}
	return projection.Type.ByName[name]
}

func compactEnumValues(schema *ontology.Schema, typeName string) []string {
	values := ontology.EnumValuesSet(schema, typeName)
	if len(values) == 0 || len(values) > 8 {
		return nil
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
