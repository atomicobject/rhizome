package identifierreconcile

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	identifierrewrite "github.com/atomicobject/rhizome/pkg/validate/identifierreconcile/rewrite"
	"slices"
	"strings"
)

// identifierAliasFieldsByType keeps declared identifier aliases and custom
// rewrite authority on their owning type. Raw aliases remain note-root metadata.
func identifierAliasFieldsByType(projections []*ontology.NodeProjection, rewrites []identifierrewrite.IdentifierRewrite) map[string][]string {
	fieldsByType := make(map[string][]string)
	for _, projection := range projections {
		if projection == nil || projection.Type == nil {
			continue
		}
		name := projection.Type.Name
		if _, seen := fieldsByType[name]; seen {
			continue
		}
		var fields []string
		if projection.Ref.Kind == ontology.NodeKindNote {
			fields = append(fields, "aliases")
		}
		if projection.Type.PreferredIdentifierField() != nil {
			for _, field := range projection.Type.Fields {
				if field != nil && (strings.EqualFold(field.Name, "alias") || strings.EqualFold(field.Name, "aliases")) {
					fields = append(fields, field.Name)
				}
			}
		}
		fieldsByType[name] = fields
	}
	for _, rewrite := range rewrites {
		fieldsByType[rewrite.OldRef.TypeName] = append(fieldsByType[rewrite.OldRef.TypeName], rewrite.AliasFieldNames()...)
	}
	for name, fields := range fieldsByType {
		slices.Sort(fields)
		fieldsByType[name] = slices.Compact(fields)
	}
	return fieldsByType
}
