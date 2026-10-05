package web

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/stretchr/testify/require"
)

func TestBuildNodePreview(t *testing.T) {
	date := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	dateTime := time.Date(2026, 6, 12, 14, 30, 5, 0, time.FixedZone("EDT", -4*60*60))

	tests := []struct {
		name  string
		build func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue)
		check func(*testing.T, NodePreview)
	}{
		{
			name: "typed note applies display rules and scalar formatting",
			build: func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue) {
				fields := []*ontology.Field{
					{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "summary"},
					{Name: "abstract", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "abstract", Display: ontology.FieldDisplay{Role: ontology.FieldDisplayRoleSummary}},
					{Name: "legacyId", Kind: ontology.FieldKindScalar, TypeName: "ID", Source: "legacy-id", IsIdentifier: true},
					{Name: "id", Kind: ontology.FieldKindScalar, TypeName: "ID", Source: "id", IsIdentifier: true, IsPreferredIdentifier: true},
					{Name: "owner", Kind: ontology.FieldKindLink, TypeName: "Person", Source: "owner"},
					{Name: "reviewers", Kind: ontology.FieldKindLink, TypeName: "Person", Source: "reviewers", List: true},
					{Name: "status", Kind: ontology.FieldKindEnum, TypeName: "Status", Source: "status", Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceKey}},
					{Name: "aliases", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "aliases", List: true},
					{Name: "date", Kind: ontology.FieldKindScalar, TypeName: "Date", Source: "date"},
					{Name: "at", Kind: ontology.FieldKindScalar, TypeName: "DateTime", Source: "at"},
					{Name: "active", Kind: ontology.FieldKindScalar, TypeName: "Boolean", Source: "active"},
					{Name: "count", Kind: ontology.FieldKindScalar, TypeName: "Int", Source: "count"},
					{Name: "ratio", Kind: ontology.FieldKindScalar, TypeName: "Float", Source: "ratio"},
					{Name: "title", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "title"},
					{Name: "detail", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "detail", Display: ontology.FieldDisplay{Importance: ontology.FieldImportanceDetail}},
					{Name: "hidden", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "hidden", Display: ontology.FieldDisplay{HideHover: true}},
					{Name: "sections", Kind: ontology.FieldKindSection, TypeName: "Section"},
					{Name: "neighbors", Kind: ontology.FieldKindNeighbor, TypeName: "Note"},
				}
				record := noderead.NodeRecord{
					Ref: ontology.NodeRef{NotePath: "notes/example.md", Kind: ontology.NodeKindNote}, Path: "notes/example.md",
					Title: "Example", TypeName: "ExampleNote", Format: noteformat.FormatID("markdown"),
					Frontmatter: map[string]any{
						"summary": "Convention summary", "abstract": "Explicit summary", "legacy-id": "OLD-1", "id": "NEW-2",
						"owner": "[[people/Ada.md]]", "reviewers": []any{"[[people/Grace Hopper|Grace]]", "[[people/Bob]]", "people/Cy.md"}, "status": "active", "aliases": []any{"one", "two"}, "date": date,
						"at": dateTime, "active": true, "count": 12, "ratio": 1.25, "title": "Example", "detail": "hidden detail", "hidden": "hidden field",
					},
				}
				links := map[string]NodePreviewValue{
					"[[people/Ada.md]]": {Text: "Ada Lovelace", Target: "people/Ada.md"},
				}
				return record, &ontology.NoteType{Name: "ExampleNote", Label: "Example note", Fields: fields}, links
			},
			check: func(t *testing.T, preview NodePreview) {
				require.Equal(t, "Explicit summary", preview.Summary)
				require.Equal(t, "NEW-2", preview.Identifier)
				require.Equal(t, "Example note", preview.TypeLabel)
				require.Equal(t, []string{"status", "summary", "owner", "reviewers", "aliases", "date", "at", "active", "count", "ratio"}, previewFieldNames(preview.Fields))
				require.Equal(t, NodePreviewValue{Text: "Ada Lovelace", Target: "people/Ada.md"}, preview.Fields[2].Values[0])
				require.Equal(t, []string{"Grace", "Bob", "people/Cy.md"}, previewValueTexts(preview.Fields[3]), "unresolved links drop brackets, aliases, and wikilink folders")
				require.Equal(t, []string{"2026-06-12"}, previewValueTexts(preview.Fields[5]))
				require.Equal(t, []string{"2026-06-12T14:30:05-04:00"}, previewValueTexts(preview.Fields[6]))
				require.Equal(t, []string{"true"}, previewValueTexts(preview.Fields[7]))
				require.Equal(t, []string{"12"}, previewValueTexts(preview.Fields[8]))
				require.Equal(t, []string{"1.25"}, previewValueTexts(preview.Fields[9]))
			},
		},
		{
			name: "untyped note sorts frontmatter and uses summary convention",
			build: func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue) {
				return noderead.NodeRecord{
					Ref: ontology.NodeRef{NotePath: "notes/plain.md", Kind: ontology.NodeKindNote}, Path: "notes/plain.md", Title: "Plain",
					Frontmatter: map[string]any{"zeta": false, "summary": "Plain summary", "alpha": 3},
				}, nil, nil
			},
			check: func(t *testing.T, preview NodePreview) {
				require.Empty(t, preview.TypeName)
				require.Equal(t, "Plain summary", preview.Summary)
				require.Equal(t, []string{"alpha", "zeta"}, previewFieldNames(preview.Fields))
				require.Equal(t, []string{"3"}, previewValueTexts(preview.Fields[0]))
				require.Equal(t, []string{"false"}, previewValueTexts(preview.Fields[1]))
			},
		},
		{
			name: "caps summary values and fields",
			build: func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue) {
				fields := []*ontology.Field{{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "summary"}}
				frontmatter := map[string]any{"summary": strings.Repeat("界", 601)}
				values := make([]any, 15)
				for i := range values {
					values[i] = fmt.Sprintf("v%d", i)
				}
				fields = append(fields, &ontology.Field{Name: "many", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "many", List: true})
				frontmatter["many"] = values
				for i := 0; i < 30; i++ {
					name := fmt.Sprintf("field%02d", i)
					fields = append(fields, &ontology.Field{Name: name, Kind: ontology.FieldKindScalar, TypeName: "String", Source: name})
					frontmatter[name] = name
				}
				return noderead.NodeRecord{Ref: ontology.NodeRef{NotePath: "caps.md", Kind: ontology.NodeKindNote}, Path: "caps.md", Frontmatter: frontmatter}, &ontology.NoteType{Name: "Cap", Fields: fields}, nil
			},
			check: func(t *testing.T, preview NodePreview) {
				require.Equal(t, 600, len([]rune(preview.Summary)))
				require.True(t, strings.HasSuffix(preview.Summary, "…"))
				require.Len(t, preview.Fields, 24)
				require.Len(t, preview.Fields[0].Values, 12)
				require.Equal(t, 3, preview.Fields[0].Truncated)
			},
		},
		{
			name: "type label humanizes the type name when no display label is authored",
			build: func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue) {
				return noderead.NodeRecord{Title: "Story"}, &ontology.NoteType{Name: "UserStory", Label: "UserStory"}, nil
			},
			check: func(t *testing.T, preview NodePreview) {
				require.Equal(t, "User story", preview.TypeLabel)
			},
		},
		{
			name: "type label keeps an authored label that equals the type name",
			build: func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue) {
				return noderead.NodeRecord{Title: "Story"}, &ontology.NoteType{Name: "UserStory", Label: "UserStory", LabelAuthored: true}, nil
			},
			check: func(t *testing.T, preview NodePreview) {
				require.Equal(t, "UserStory", preview.TypeLabel)
			},
		},
		{
			name: "summary honors hover opt-out",
			build: func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue) {
				fields := []*ontology.Field{{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "summary", Display: ontology.FieldDisplay{HideHover: true}}}
				return noderead.NodeRecord{Title: "Private", Frontmatter: map[string]any{"summary": "Not for previews"}}, &ontology.NoteType{Name: "Doc", Fields: fields}, nil
			},
			check: func(t *testing.T, preview NodePreview) {
				require.Empty(t, preview.Summary)
				require.Empty(t, preview.Fields)
			},
		},
		{
			name: "fields read declared source aliases and case variations",
			build: func() (noderead.NodeRecord, *ontology.NoteType, map[string]NodePreviewValue) {
				fields := []*ontology.Field{
					{Name: "summary", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "summary"},
					{Name: "owner", Kind: ontology.FieldKindScalar, TypeName: "String", Source: "owner", SourceAliases: []string{"owned-by"}},
				}
				return noderead.NodeRecord{Title: "Aliased", Frontmatter: map[string]any{"Summary": "Case variant", "owned-by": "Ada"}}, &ontology.NoteType{Name: "Doc", Fields: fields}, nil
			},
			check: func(t *testing.T, preview NodePreview) {
				require.Equal(t, "Case variant", preview.Summary)
				require.Equal(t, []string{"owner"}, previewFieldNames(preview.Fields))
				require.Equal(t, []string{"Ada"}, previewValueTexts(preview.Fields[0]))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, noteType, resolvedLinks := test.build()
			preview := buildNodePreview(record, noteType, resolvedLinks, true)
			test.check(t, preview)
		})
	}
}

func previewFieldNames(fields []NodePreviewField) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, field.Name)
	}
	return out
}

func previewValueTexts(field NodePreviewField) []string {
	out := make([]string, 0, len(field.Values))
	for _, value := range field.Values {
		out = append(out, value.Text)
	}
	return out
}

func TestSectionSummaryPreviewUsesIndexedOrStagedText(t *testing.T) {
	field := &ontology.Field{Name: "abstract", Kind: ontology.FieldKindSection, TypeName: "Section", Display: ontology.FieldDisplay{Role: ontology.FieldDisplayRoleSummary}}
	noteType := &ontology.NoteType{Name: "Entry", Fields: []*ontology.Field{field}}
	record := noderead.NodeRecord{Title: "Entry", InlineProps: map[string][]string{"abstract": {"A concise summary."}}}
	preview := buildNodePreview(record, noteType, nil, true)
	require.Equal(t, "A concise summary.", preview.Summary)
	require.Empty(t, preview.Fields)
	field.Display.HideHover = true
	require.Empty(t, buildNodePreview(record, noteType, nil, true).Summary)
}
