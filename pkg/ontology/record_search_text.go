package ontology

import (
	"sort"
	"strings"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

// recordSearchBody is a record's own lexical evidence: its bound scalar and
// enum values, then the unbound source text its semantic chunks embed
// (NodeBodyParts). Field names, ancestors, and siblings stay out, so a record
// matches only on values it owns.
func recordSearchBody(projection *NodeProjection) string {
	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines []string
	for _, name := range names {
		if projection.Type == nil {
			break
		}
		if def := projection.Type.ByName[name]; def == nil || (def.Kind != ontoschema.FieldKindScalar && def.Kind != ontoschema.FieldKindEnum) {
			continue
		}
		lines = append(lines, projection.Fields[name].Values...)
	}
	// Record parts split only at the byte budget, so joining restores the text.
	lines = append(lines, strings.Join(NodeBodyParts(projection, 0), ""))
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
