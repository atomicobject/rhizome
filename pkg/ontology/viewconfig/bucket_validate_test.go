package viewconfig

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateGroupBucketsDensityAndMissingFilters(t *testing.T) {
	schema := &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{"Meeting": {Name: "Meeting", Fields: []*ontoschema.Field{
		{Name: "held", Kind: ontoschema.FieldKindScalar, TypeName: "Date", SourceKind: ontoschema.FieldSourceFrontmatter, Source: "held-on"},
		{Name: "topic", Kind: ontoschema.FieldKindScalar, TypeName: "String"},
	}}}}
	view := func(group *GroupSpec, density string) ViewDefinition {
		return ViewDefinition{
			APIVersion: APIVersion, ID: "meetings", Name: "Meetings",
			SourceSpec: SourceSpec{Kind: SourceKindOntologyType, Type: "Meeting"},
			Mount:      MountSpec{Kind: MountKindStandalone},
			Defaults:   DefaultsSpec{Group: group, Filters: []FilterSpec{{Field: "topic", Op: "missing"}}},
			Variants:   VariantSet{Table: &TableVariant{Columns: []ViewColumn{{Field: "title"}}, Density: density}},
		}
	}
	opts := SchemaValidateOptions(schema)

	for _, field := range []string{"held", "frontmatter.held-on"} {
		require.Empty(t, Validate([]ViewDefinition{view(&GroupSpec{Field: field, Bucket: GroupBucketMonth}, TableDensityOneLine)}, opts).Issues, field)
	}
	requireIssueField(t, Validate([]ViewDefinition{view(&GroupSpec{Field: "topic", Bucket: GroupBucketMonth}, "")}, opts).Issues, "group_bucket_requires_date", "defaults.group.bucket")
	requireIssueField(t, Validate([]ViewDefinition{view(&GroupSpec{Field: "held", Bucket: "week"}, "")}, opts).Issues, "unsupported_group_bucket", "defaults.group.bucket")
	requireIssueField(t, Validate([]ViewDefinition{view(&GroupSpec{Fields: []string{"held", "topic"}, Bucket: GroupBucketMonth}, "")}, opts).Issues, "unsupported_group_bucket", "defaults.group.bucket")
	requireIssueField(t, Validate([]ViewDefinition{view(nil, "cozy")}, opts).Issues, "unsupported_table_density", "variants.table.density")
}
