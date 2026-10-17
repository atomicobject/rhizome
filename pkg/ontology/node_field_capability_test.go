package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestBuildNodeWorkspaceProjectsOwnerSpecificFieldCapabilitiesAndSourceRevision(t *testing.T) {
	schema := &ontoschema.Schema{
		EnumTypes: map[string]*ontoschema.EnumType{
			"Status": {
				Name: "Status",
				Values: []*ontoschema.EnumValue{
					{Name: "draft"},
					{Name: "ready", View: ontoschema.EnumValueView{Label: "Ready to ship", Order: 1, Tone: "success"}},
				},
			},
		},
	}
	ownerType := &ontoschema.NoteType{
		Name: "Story",
		Fields: []*ontoschema.Field{
			{
				Name:       "status",
				Kind:       ontoschema.FieldKindEnum,
				TypeName:   "Status",
				Required:   true,
				SourceKind: ontoschema.FieldSourceInline,
				Display:    ontoschema.FieldDisplay{Importance: ontoschema.FieldImportanceKey},
			},
			{
				Name:         "reviewers",
				Kind:         ontoschema.FieldKindLink,
				TypeName:     "Person",
				List:         true,
				SourceKind:   ontoschema.FieldSourceFrontmatter,
				IsIdentifier: false,
			},
		},
	}
	ownerType.ByName = map[string]*ontoschema.Field{
		"status":    ownerType.Fields[0],
		"reviewers": ownerType.Fields[1],
	}
	schema.Types = map[string]*ontoschema.NoteType{"Story": ownerType}

	projection := &NodeProjection{
		Ref:          NodeRef{NotePath: "stories/one.md", Fragment: "^story-1", Kind: NodeKindEmbedded, TypeName: "Story"},
		ResolvedType: "Story",
		Type:         ownerType,
		Snapshot: &DocumentSnapshot{
			NotePath:           "stories/one.md",
			Content:            "status:: ready\n^story-1\n",
			ContentFingerprint: "source-fingerprint",
		},
		Fields: map[string]FieldBinding{
			"status":    {FieldName: "status", Kind: BindingKindInlineField, SourceKind: ontoschema.FieldSourceInline, Present: true, Values: []string{"ready"}},
			"reviewers": {FieldName: "reviewers", Kind: BindingKindFrontmatterField, SourceKind: ontoschema.FieldSourceFrontmatter},
		},
	}

	workspace := BuildNodeWorkspaceFromProjectionWithSchema(schema, projection)
	require.NotNil(t, workspace)
	require.Equal(t, NodeSourceRevision{
		NotePath:           "stories/one.md",
		ContentFingerprint: "source-fingerprint",
		Content:            "status:: ready\n^story-1\n",
	}, workspace.SourceRevision)

	fields := map[string]NodeFieldState{}
	for _, field := range workspace.Fields {
		fields[field.Name] = field
	}
	require.Equal(t, NodeFieldCapability{
		OwnerRef:   projection.Ref,
		OwnerType:  "Story",
		TypeName:   "Status",
		ValueKind:  NodeFieldValueKindEnum,
		Required:   true,
		EnumValues: []string{"draft", "ready"},
		EnumOptions: []NodeFieldEnumOption{
			{Value: "draft", Label: "draft", Tone: "neutral", Stage: ontoschema.StageOpen},
			{Value: "ready", Label: "Ready to ship", Tone: "success", Stage: ontoschema.StageDone},
		},
		SourceKind:        ontoschema.FieldSourceInline,
		ValueOrigin:       NodeFieldValueOriginAuthored,
		DisplayImportance: ontoschema.FieldImportanceKey,
		WriteOperation:    NodeFieldWriteOperationSetField,
	}, fields["status"].Capability)
	require.Equal(t, NodeFieldValueKindRelation, fields["reviewers"].Capability.ValueKind)
	require.True(t, fields["reviewers"].Capability.List)
	require.Equal(t, "Person", fields["reviewers"].Capability.TargetType)
	require.Equal(t, ontoschema.FieldImportanceNormal, fields["reviewers"].Capability.DisplayImportance)
	require.Equal(t, NodeFieldWriteOperationSetLinkField, fields["reviewers"].Capability.WriteOperation)
}

func TestBuildNodeWorkspacePublishesProviderRootSourceRevision(t *testing.T) {
	projection := &NodeProjection{
		Ref: NodeRef{NotePath: "reports/one.html", Kind: NodeKindNote},
		RootSnapshot: &RootDocumentSnapshot{
			NotePath: paths.NotePath("reports/one.html"), RawSource: []byte("<html>authored</html>"), ContentHash: "html-fingerprint",
		},
	}

	workspace := BuildNodeWorkspaceFromProjectionWithSchema(&ontoschema.Schema{}, projection)

	require.Equal(t, NodeSourceRevision{
		NotePath: "reports/one.html", ContentFingerprint: "html-fingerprint", Content: "<html>authored</html>",
	}, workspace.SourceRevision)
}

func TestBuildNodeWorkspaceMakesIdentifiersReadOnly(t *testing.T) {
	field := &ontoschema.Field{
		Name:                  "id",
		Kind:                  ontoschema.FieldKindScalar,
		TypeName:              "String",
		SourceKind:            ontoschema.FieldSourceFrontmatter,
		IsIdentifier:          true,
		IsPreferredIdentifier: true,
	}
	noteType := &ontoschema.NoteType{Name: "Spec", Fields: []*ontoschema.Field{field}, ByName: map[string]*ontoschema.Field{"id": field}}
	workspace := BuildNodeWorkspaceFromProjectionWithSchema(&ontoschema.Schema{}, &NodeProjection{
		Ref:      NodeRef{NotePath: "notes/one.md", Kind: NodeKindNote, TypeName: "Spec"},
		Type:     noteType,
		Snapshot: &DocumentSnapshot{NotePath: "notes/one.md", Content: "# One\n"},
		Fields: map[string]FieldBinding{
			"id": {FieldName: "id", SourceKind: ontoschema.FieldSourceFrontmatter, Present: true, Values: []string{"SPEC-0001"}},
		},
	})

	require.Len(t, workspace.Fields, 1)
	require.Empty(t, workspace.Fields[0].Capability.WriteOperation)
	require.Equal(t, "Identifiers are graph keys and must stay unique. Change them through identifier reconciliation.", workspace.Fields[0].Capability.ReadOnlyReason)
}
