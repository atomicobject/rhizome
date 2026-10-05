package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBuildWorkspaceGraph_EmitsCanonicalNodesEdgesAndViews(t *testing.T) {
	resp := NodeWorkspaceResponse{
		RequestedRef: "specs/demo/spec.md",
		Node: NodeDescriptorResponse{
			Ref: ontology.NodeRef{
				NotePath: "specs/demo/spec.md",
				Kind:     ontology.NodeKindNote,
			},
			NotePath:     "specs/demo/spec.md",
			Title:        "Demo spec",
			ResolvedType: "Spec",
			Locator:      "FILE",
		},
		Content: NodeContentResponse{
			Path:         "specs/demo/spec.md",
			Title:        "Demo spec",
			ResolvedType: "Spec",
			Markdown:     "# Demo spec",
			TypeDoc: &ontology.TypeDoc{
				Name: "Spec",
				Fields: []ontology.FieldDoc{
					{Name: "summary", TypeName: "String", Kind: ontology.FieldKindScalar},
					{Name: "specStatus", TypeName: "SpecStatus", Kind: ontology.FieldKindEnum, EnumValues: []string{"proposed", "active", "superseded", "archived"}},
				},
			},
			Rendered: &RenderedFileResponse{
				Path:         "specs/demo/spec.md",
				Title:        "Demo spec",
				ResolvedType: "Spec",
				Sections: []RenderedSection{
					{
						ID:      "specs/demo/spec.md#Summary",
						Title:   "Summary",
						Level:   ontology.SectionLevelH2,
						Content: "Body",
						Children: []RenderedSection{
							{
								ID:       "specs/demo/spec.md#^validation",
								Title:    "Validation story",
								Level:    ontology.SectionLevelH4,
								Content:  "Story body",
								BlockID:  "^validation",
								Locator:  "EMBEDDED",
								TypeName: "Story",
							},
						},
					},
				},
			},
			Structural: &StructuralViewResponse{
				DefaultView: "structural",
				Root: StructuralNodeResponse{
					NodeID:   "specs/demo/spec.md",
					Locator:  "FILE",
					Title:    "Demo spec",
					TypeName: "Spec",
					NotePath: "specs/demo/spec.md",
					Children: []StructuralNodeResponse{
						{
							NodeID:    "specs/demo/spec.md#Summary",
							Fragment:  "Summary",
							Locator:   "SECTION",
							Title:     "Summary",
							NotePath:  "specs/demo/spec.md",
							Level:     ontology.SectionLevelH2,
							Content:   "Body",
							FieldName: "summary",
							FieldPath: "summary",
							Children: []StructuralNodeResponse{
								{
									NodeID:    "specs/demo/spec.md#^validation",
									Fragment:  "^validation",
									Locator:   "EMBEDDED",
									Title:     "Validation story",
									TypeName:  "Story",
									NotePath:  "specs/demo/spec.md",
									Level:     ontology.SectionLevelH4,
									Content:   "Story body",
									FieldName: "stories",
									FieldPath: "stories",
								},
							},
						},
					},
				},
			},
		},
		Fields: []ontology.NodeFieldState{
			{
				Name:    "summary",
				Present: true,
				Range:   ontology.NodeRange{Start: 1, End: 10},
				Values:  []string{"Body"},
				Status:  ontology.NodeStatus{},
			},
			{
				Name:    "specStatus",
				Present: true,
				Range:   ontology.NodeRange{Start: 21, End: 30},
				Values:  []string{"proposed"},
				Status:  ontology.NodeStatus{},
			},
		},
		Collections: []ontology.NodeCollectionState{
			{
				Name:   "stories",
				Range:  ontology.NodeRange{Start: 11, End: 20},
				Status: ontology.NodeStatus{},
				Items: []ontology.NodeCollectionItemState{
					{
						Ref: ontology.NodeRef{
							NotePath: "specs/demo/story.md",
							Kind:     ontology.NodeKindNote,
						},
					},
				},
			},
		},
		Relations: []NoteWorkspaceGroup{
			{
				Key:   "ambient",
				Label: "Ambient relations",
				Items: []NoteWorkspaceLink{
					{
						Path:  "specs/demo/plan.md",
						Title: "Plan",
						Kind:  "note",
					},
				},
			},
		},
		Status:       ontology.NodeStatus{},
		Capabilities: ontology.NodeCapabilities{CanEdit: true},
		Version:      "v1",
	}

	focusedID, nodes, edges, views := buildWorkspaceGraph(&resp, nil, nil)
	summaryID := workspaceNodeIDForRef(ontology.NodeRef{
		NotePath: "specs/demo/spec.md",
		Fragment: "Summary",
		NodeID:   "specs/demo/spec.md#Summary",
		Kind:     ontology.NodeKindSection,
	})
	embeddedID := workspaceNodeIDForRef(ontology.NodeRef{
		NotePath: "specs/demo/spec.md",
		Fragment: "^validation",
		NodeID:   "specs/demo/spec.md#^validation",
		Kind:     ontology.NodeKindEmbedded,
	})

	require.NotEmpty(t, focusedID)
	require.NotEmpty(t, nodes)
	require.NotEmpty(t, edges)
	require.NotNil(t, views)
	require.Equal(t, focusedID, views.StructuralOutline.RootID)
	require.Equal(t, []string{summaryID}, views.RenderedOutline.RootIDs)

	nodeByID := map[string]WorkspaceNodeResponse{}
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}
	require.Contains(t, nodeByID, focusedID)
	require.Contains(t, nodeByID, summaryID)
	require.Contains(t, nodeByID, embeddedID)
	require.Equal(t, "FILE", nodeByID[focusedID].Data.Locator)
	require.Equal(t, ontology.SectionLevelH4, nodeByID[embeddedID].Data.Level)

	fieldNodeID := workspaceSyntheticNodeID("field", focusedID, "summary")
	statusFieldNodeID := workspaceSyntheticNodeID("field", focusedID, "specStatus")
	collectionNodeID := workspaceSyntheticNodeID("collection", focusedID, "stories")
	locatorFieldNodeID := workspaceSyntheticNodeID("field", embeddedID, "locator")
	require.Contains(t, nodeByID, fieldNodeID)
	require.Contains(t, nodeByID, statusFieldNodeID)
	require.Contains(t, nodeByID, collectionNodeID)
	require.Contains(t, nodeByID, locatorFieldNodeID)
	require.Equal(t, []string{"validation"}, nodeByID[locatorFieldNodeID].Field.Values)

	// Field nodes carry the schema-declared type so the frontend can pick the
	// right widget without re-fetching the ontology schema. Enum fields also
	// publish their admitted values so segmented pill controls work day one.
	require.Equal(t, "String", nodeByID[fieldNodeID].Field.TypeName)
	require.Empty(t, nodeByID[fieldNodeID].Field.EnumValues)
	require.Equal(t, "SpecStatus", nodeByID[statusFieldNodeID].Field.TypeName)
	require.Equal(t, []string{"proposed", "active", "superseded", "archived"}, nodeByID[statusFieldNodeID].Field.EnumValues)

	require.Condition(t, func() bool {
		for _, edge := range edges {
			if edge.Kind == WorkspaceEdgeKindBindsField && edge.ToID == fieldNodeID {
				return true
			}
		}
		return false
	})
	require.Condition(t, func() bool {
		for _, edge := range edges {
			if edge.Kind == WorkspaceEdgeKindRelatesTo && edge.RelationKey == "ambient" {
				return true
			}
		}
		return false
	})
	containsCount := 0
	for _, edge := range edges {
		if edge.Kind == WorkspaceEdgeKindContains && edge.FromID == summaryID && edge.ToID == embeddedID {
			containsCount++
		}
	}
	require.Equal(t, 1, containsCount)
}

func TestBuildWorkspaceGraph_AttachesBodyToInlineChildDescendants(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "specs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type ProductSpec @node(paths: ["specs/*.md"]) {
  story: UserStory @contains(level: H2, heading: "Story")
}

type UserStory implements Section @node(locator: EMBEDDED) {
  acceptanceCriteria: NarrativeSection @contains(level: H3, heading: "Acceptance Criteria", display: INLINE)
}

type NarrativeSection implements Section {
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "specs", "demo.md"), []byte(`---
type: ProductSpec
---

# Demo

## Story

Story body.

### Acceptance Criteria

- must pass
`), 0o644))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := ontology.LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/demo.md")
	require.NoError(t, err)
	noteProjection, err := ontology.ProjectNodeFromSnapshot(snapshot, schema, ontology.NodeRef{
		NotePath: "specs/demo.md",
		Kind:     ontology.NodeKindNote,
	})
	require.NoError(t, err)
	storyRef := noteProjection.Fields["story"].SectionNodes[0]
	srv := newFixtureServer(t, fixtureVault{
		root:     root,
		vault:    &obsidian.Vault{Name: "test"},
		vaultDef: obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
	}, nil)

	workspace, err := srv.nodeWorkspace(t.Context(), storyRef, defaultNodeWorkspaceIncludes())
	require.NoError(t, err)
	focusedID := workspace.FocusedNodeID
	require.NotEmpty(t, focusedID)
	byID := map[string]WorkspaceNodeResponse{}
	for _, node := range workspace.Nodes {
		byID[node.ID] = node
	}
	focused := byID[focusedID]
	require.NotEmpty(t, focused.Body)
	var inlineChildRef *ontology.NodeRef
	for _, block := range focused.Body {
		if block.Kind == ontology.NodeBodyBlockKindChildSection && block.SectionDisplay == ontology.SectionDisplayInline {
			inlineChildRef = block.ChildRef
			break
		}
	}
	require.NotNil(t, inlineChildRef)
	childID := workspaceNodeIDForRef(*inlineChildRef)
	require.Contains(t, byID, childID)
	child := byID[childID]
	require.Equal(t, inlineChildRef.NodeID, child.Ref.NodeID)
	require.NotEmpty(t, child.Body, "INLINE child sections must keep body blocks for BodyWalker recursion")
	childMarkdown := ""
	for _, block := range child.Body {
		childMarkdown += block.Markdown
	}
	require.Contains(t, childMarkdown, "must pass")
	require.NotNil(t, child.Data)
	require.Equal(t, "Acceptance Criteria", child.Data.Title, "inline child placeholders must carry the authored heading title")
}

func TestBuildWorkspaceGraph_LocatorFieldFollowsSchemaIdentifier(t *testing.T) {
	notePath := "docs/spec.md"
	embeddedRef := ontology.NodeRef{
		NotePath: notePath,
		Fragment: "^SPEC-0007-US1",
		NodeID:   notePath + "#^SPEC-0007-US1",
		Kind:     ontology.NodeKindEmbedded,
	}
	tests := []struct {
		name            string
		identifierField string
		properties      map[string]string
		wantLocator     bool
	}{
		{
			name:            "schema identifier equal to block id suppresses locator",
			identifierField: "storyID",
			properties: map[string]string{
				"id":      "not-the-identifier",
				"storyID": "SPEC-0007-US1",
				"status":  "ready",
			},
		},
		{
			name:        "plain id field without schema identifier keeps locator",
			properties:  map[string]string{"id": "SPEC-0007-US1"},
			wantLocator: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := NodeWorkspaceResponse{
				RequestedRef: notePath,
				Node: NodeDescriptorResponse{
					Ref:      ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindNote},
					NotePath: notePath,
					Title:    "Spec",
					Locator:  "FILE",
				},
				Content: NodeContentResponse{
					Path:  notePath,
					Title: "Spec",
					Structural: &StructuralViewResponse{
						DefaultView: "structural",
						Root: StructuralNodeResponse{
							NodeID:   notePath,
							Locator:  "FILE",
							Title:    "Spec",
							NotePath: notePath,
							Children: []StructuralNodeResponse{{
								NodeID:          notePath + "#^SPEC-0007-US1",
								Fragment:        "^SPEC-0007-US1",
								Locator:         "EMBEDDED",
								Title:           "Story",
								NotePath:        notePath,
								TypeName:        "UserStory",
								FieldName:       "userStories",
								FieldPath:       "userStories",
								IdentifierField: tt.identifierField,
								Properties:      tt.properties,
							}},
						},
					},
				},
			}

			_, nodes, _, _ := buildWorkspaceGraph(&resp, nil, nil)
			nodeByID := map[string]WorkspaceNodeResponse{}
			for _, node := range nodes {
				nodeByID[node.ID] = node
			}
			embeddedID := workspaceNodeIDForRef(embeddedRef)
			locatorFieldNodeID := workspaceSyntheticNodeID("field", embeddedID, "locator")
			require.Contains(t, nodeByID, embeddedID)
			if !tt.wantLocator {
				require.NotContains(t, nodeByID, locatorFieldNodeID)
				return
			}
			require.Contains(t, nodeByID, locatorFieldNodeID)
			require.Equal(t, []string{"SPEC-0007-US1"}, nodeByID[locatorFieldNodeID].Field.Values)
		})
	}
}

// TestBuildWorkspaceGraph_CollectionItemsConvergeWithRenderedOutlineTitles
// guards against regressions where addCollectionNodes and addRenderedOutline
// produce divergent workspace IDs for the same logical node. When that
// happened (Structural fingerprint in the identity key), collection rows
// rendered as "Untitled" because the collection-path node was bare and the
// rendered-outline-path node (with title) was a separate entry. Post-fix:
// one workspace node per logical child, title populated from the outline.
func TestBuildWorkspaceGraph_CollectionItemsConvergeWithRenderedOutlineTitles(t *testing.T) {
	notePath := "docs/spec.md"
	containerID := notePath + "#UserStories-100"
	storyID := notePath + "#^story-a"
	// Item ref from a real projection carries ParentID + Structural; the
	// rendered-outline ref does not. Both must resolve to the same
	// workspace node now.
	itemRef := ontology.NodeRef{
		NotePath:   notePath,
		Fragment:   "^story-a",
		NodeID:     storyID,
		Kind:       ontology.NodeKindEmbedded,
		StartByte:  200,
		EndByte:    300,
		ParentID:   containerID,
		Structural: "fingerprint-abc",
	}
	resp := NodeWorkspaceResponse{
		RequestedRef: notePath + "#UserStories-100",
		Node: NodeDescriptorResponse{
			Ref: ontology.NodeRef{
				NotePath: notePath,
				Fragment: "UserStories-100",
				NodeID:   containerID,
				Kind:     ontology.NodeKindSection,
			},
			NotePath: notePath,
			Title:    "User Stories",
			Locator:  "SECTION",
		},
		Content: NodeContentResponse{
			Path:  containerID,
			Title: "User Stories",
			Rendered: &RenderedFileResponse{
				Path:  containerID,
				Title: "User Stories",
				Sections: []RenderedSection{
					{
						ID:    containerID,
						Title: "User Stories",
						Level: ontology.SectionLevelH2,
						Children: []RenderedSection{
							{
								ID:      storyID,
								Title:   "As a user exploring a note",
								Level:   ontology.SectionLevelH3,
								BlockID: "story-a",
							},
						},
					},
				},
			},
		},
		Collections: []ontology.NodeCollectionState{
			{
				Name:  "stories",
				Kind:  ontology.BindingKindSectionList,
				Range: ontology.NodeRange{Start: 100, End: 500},
				Items: []ontology.NodeCollectionItemState{
					{
						Ref:   itemRef,
						Range: ontology.NodeRange{Start: 200, End: 300},
					},
				},
			},
		},
		Status:       ontology.NodeStatus{},
		Capabilities: ontology.NodeCapabilities{},
		Version:      "v1",
	}

	_, nodes, _, _ := buildWorkspaceGraph(&resp, nil, nil)

	// Exactly one workspace node should exist for the UserStory — the
	// collection-item-created one and the rendered-outline-created one must
	// converge. Its title comes from the outline merge.
	matching := []WorkspaceNodeResponse{}
	for _, node := range nodes {
		if node.Kind == WorkspaceNodeKindEmbedded && node.Ref.NodeID == storyID {
			matching = append(matching, node)
		}
	}
	require.Len(t, matching, 1, "collection item and rendered outline must produce one workspace node per logical child")
	require.Equal(t, WorkspaceNodeKindEmbedded, matching[0].Kind)
	require.NotNil(t, matching[0].Data)
	require.Equal(
		t,
		"As a user exploring a note",
		matching[0].Data.Title,
		"rendered-outline title must merge into the collection-item node so collection rows aren't Untitled",
	)
}
