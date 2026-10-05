package viewconfig

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// SchemaValidateOptions is the schema-derived part of view validation that
// `rzm validate views`, the views catalog, and Save view share.
func SchemaValidateOptions(schema *ontology.Schema) ValidateOptions {
	opts := ValidateOptions{}
	if schema == nil {
		return opts
	}
	opts.CheckReferences = true
	opts.GroupNames = DisplayGroups(schema)
	opts.TypeNames = make(map[string]struct{}, len(schema.Types))
	opts.NodeTypeNames = make(map[string]struct{}, len(schema.Types))
	opts.InterfaceNames = make(map[string]struct{}, len(schema.Interfaces))
	opts.DateFields = map[string]map[string]struct{}{}
	typeNames := make([]string, 0, len(schema.Types))
	for name, noteType := range schema.Types {
		opts.TypeNames[name] = struct{}{}
		if IsNodeType(noteType) {
			opts.NodeTypeNames[name] = struct{}{}
		}
		typeNames = append(typeNames, name)
		if noteType != nil {
			opts.DateFields[name] = dateFieldSelectors(noteType.Fields)
		}
	}
	interfaceNames := make([]string, 0, len(schema.Interfaces))
	for name, iface := range schema.Interfaces {
		opts.InterfaceNames[name] = struct{}{}
		interfaceNames = append(interfaceNames, name)
		if iface != nil {
			opts.DateFields[name] = dateFieldSelectors(iface.Fields)
		}
	}
	opts.GeneratedIDs = GeneratedIDs(typeNames, interfaceNames)
	return opts
}

// dateFieldSelectors lists the selectors that name a single Date or DateTime
// field: its schema name and its raw frontmatter or inline key.
func dateFieldSelectors(fields []*ontology.Field) map[string]struct{} {
	out := map[string]struct{}{}
	for _, field := range fields {
		if field == nil || field.Kind != ontology.FieldKindScalar || field.List || (field.TypeName != "Date" && field.TypeName != "DateTime") {
			continue
		}
		out[field.Name] = struct{}{}
		source := strings.TrimSpace(field.Source)
		if source == "" {
			source = field.Name
		}
		switch field.SourceKind {
		case ontology.FieldSourceFrontmatter:
			out["frontmatter."+source] = struct{}{}
		case ontology.FieldSourceInline:
			out["inline."+source] = struct{}{}
		}
	}
	return out
}

// validateGroupBucket allows only month buckets, on one Date or DateTime
// field. Query recipe fields are unknown here; execution treats unparseable
// values as undated.
func validateGroupBucket(def ViewDefinition, opts ValidateOptions) []Issue {
	group := def.Defaults.Group
	if group == nil || group.Bucket == "" {
		return nil
	}
	if group.Bucket != GroupBucketMonth {
		return []Issue{issue(def, "unsupported_group_bucket", "defaults.group.bucket", fmt.Sprintf("group bucket %q is not supported; use %q", group.Bucket, GroupBucketMonth))}
	}
	fields := group.Fields
	if len(fields) == 0 && group.Field != "" {
		fields = []string{group.Field}
	}
	if len(fields) != 1 {
		return []Issue{issue(def, "unsupported_group_bucket", "defaults.group.bucket", "a month bucket groups exactly one field")}
	}
	subject := def.SourceSpec.Type
	if def.SourceSpec.Kind == SourceKindOntologyInterface {
		subject = def.SourceSpec.Interface
	}
	dates, known := opts.DateFields[subject]
	if !opts.CheckReferences || !known || (def.SourceSpec.Kind != SourceKindOntologyType && def.SourceSpec.Kind != SourceKindOntologyInterface) {
		return nil
	}
	if _, ok := dates[fields[0]]; !ok {
		return []Issue{issue(def, "group_bucket_requires_date", "defaults.group.bucket", fmt.Sprintf("month buckets need a Date or DateTime field; %q is not one", fields[0]))}
	}
	return nil
}
