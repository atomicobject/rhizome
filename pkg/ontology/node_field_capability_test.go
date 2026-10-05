package ontology

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestBuildNodeWorkspaceProjectsOwnerSpecificFieldCapabilitiesAndSourceRevision(t *testing.T) {
	schema := &Schema{
		EnumTypes: map[string]*EnumType{
			"Status": {
				Name: "Status",
				Values: []*EnumValue{
					{Name: "draft"},
					{Name: "ready", View: EnumValueView{Label: "Ready to ship", Order: 1, Tone: "success"}},
				},
			},
		},
	}
	ownerType := &NoteType{
		Name: "Story",
		Fields: []*Field{
			{
				Name:       "status",
				Kind:       FieldKindEnum,
				TypeName:   "Status",
				Required:   true,
				SourceKind: FieldSourceInline,
				Display:    FieldDisplay{Importance: FieldImportanceKey},
			},
			{
				Name:         "reviewers",
				Kind:         FieldKindLink,
				TypeName:     "Person",
				List:         true,
				SourceKind:   FieldSourceFrontmatter,
				IsIdentifier: false,
			},
		},
	}
	ownerType.ByName = map[string]*Field{
		"status":    ownerType.Fields[0],
		"reviewers": ownerType.Fields[1],
	}
	schema.Types = map[string]*NoteType{"Story": ownerType}

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
			"status":    {FieldName: "status", Kind: BindingKindInlineField, SourceKind: FieldSourceInline, Present: true, Values: []string{"ready"}},
			"reviewers": {FieldName: "reviewers", Kind: BindingKindFrontmatterField, SourceKind: FieldSourceFrontmatter},
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
			{Value: "draft", Label: "draft", Tone: "neutral", Stage: StageOpen},
			{Value: "ready", Label: "Ready to ship", Tone: "success", Stage: StageDone},
		},
		SourceKind:        FieldSourceInline,
		ValueOrigin:       NodeFieldValueOriginAuthored,
		DisplayImportance: FieldImportanceKey,
		WriteOperation:    NodeFieldWriteOperationSetField,
	}, fields["status"].Capability)
	require.Equal(t, NodeFieldValueKindRelation, fields["reviewers"].Capability.ValueKind)
	require.True(t, fields["reviewers"].Capability.List)
	require.Equal(t, "Person", fields["reviewers"].Capability.TargetType)
	require.Equal(t, FieldImportanceNormal, fields["reviewers"].Capability.DisplayImportance)
	require.Equal(t, NodeFieldWriteOperationSetLinkField, fields["reviewers"].Capability.WriteOperation)
}

func TestBuildNodeWorkspacePublishesProviderRootSourceRevision(t *testing.T) {
	projection := &NodeProjection{
		Ref: NodeRef{NotePath: "reports/one.html", Kind: NodeKindNote},
		RootSnapshot: &RootDocumentSnapshot{
			NotePath: paths.NotePath("reports/one.html"), RawSource: []byte("<html>authored</html>"), ContentHash: "html-fingerprint",
		},
	}

	workspace := BuildNodeWorkspaceFromProjectionWithSchema(&Schema{}, projection)

	require.Equal(t, NodeSourceRevision{
		NotePath: "reports/one.html", ContentFingerprint: "html-fingerprint", Content: "<html>authored</html>",
	}, workspace.SourceRevision)
}

func TestBuildNodeWorkspaceMakesIdentifiersReadOnly(t *testing.T) {
	field := &Field{
		Name:                  "id",
		Kind:                  FieldKindScalar,
		TypeName:              "String",
		SourceKind:            FieldSourceFrontmatter,
		IsIdentifier:          true,
		IsPreferredIdentifier: true,
	}
	noteType := &NoteType{Name: "Spec", Fields: []*Field{field}, ByName: map[string]*Field{"id": field}}
	workspace := BuildNodeWorkspaceFromProjectionWithSchema(&Schema{}, &NodeProjection{
		Ref:      NodeRef{NotePath: "notes/one.md", Kind: NodeKindNote, TypeName: "Spec"},
		Type:     noteType,
		Snapshot: &DocumentSnapshot{NotePath: "notes/one.md", Content: "# One\n"},
		Fields: map[string]FieldBinding{
			"id": {FieldName: "id", SourceKind: FieldSourceFrontmatter, Present: true, Values: []string{"SPEC-0001"}},
		},
	})

	require.Len(t, workspace.Fields, 1)
	require.Empty(t, workspace.Fields[0].Capability.WriteOperation)
	require.Equal(t, "Identifiers are graph keys and must stay unique. Change them through identifier reconciliation.", workspace.Fields[0].Capability.ReadOnlyReason)
}
