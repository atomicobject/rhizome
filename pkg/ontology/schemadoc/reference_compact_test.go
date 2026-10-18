package schemadoc

import (
	"encoding/json"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestCompactPreservesAuthoredBindingsAndPolicies(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
 enum Status { active frozen @policy(requiresUserConfirmation: true, forbidAutonomousSemanticEdits: true, reason: "Frozen contract") }
 type Summary implements Section {}
 type Item implements Section @node(locator: EMBEDDED) {}
 """Expanded fallback description
 with several lines of authoring prose.
 """
 type Person @node(paths: ["people/*.md"]) { name: String! }
 type Project @node(paths: ["projects/*.md"])
 @display(singular: "Project", plural: "Projects", group: "Delivery", parent: "Portfolio")
 @guidance(meaning: "Expanded meaning should not appear in compact output")
 @companionDocs(paths:["docs/project.md"], purpose: "workflow") {
 id: String! @field @identifier(preferred:true, prefix:"PROJ")
 status: Status! @field(source:"project-status")
 owner: Person @link @workspaceMember
 summary: Summary @contains(level:H2,heading:"Summary",required:true)
 items: [Item!] @contains(shape:CHECKBOX_ITEM,marker:"#item")
 }
 `), 0644))
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	before, err := RenderReferenceJSON(schema, "Project", identity.CurrentUserType)
	require.NoError(t, err)
	result, err := CompactReference(schema, "Project", identity.CurrentUserType)
	require.NoError(t, err)
	require.Equal(t, "compact-authoring", result.Format)
	require.Equal(t, schema.Hash, result.SchemaHash)
	require.Len(t, result.Types, 1)
	payload, err := json.Marshal(result)
	require.NoError(t, err)
	for _, part := range []string{"project-status", "PROJ", "Summary", "sectionRequired", "#item", "CHECKBOX_ITEM", "Person", "requiresUserConfirmation", "forbidAutonomousSemanticEdits", "docs/project.md"} {
		require.Contains(t, string(payload), part)
	}
	require.NotContains(t, string(payload), "Expanded meaning")
	require.NotContains(t, string(payload), "Delivery")
	require.NotContains(t, string(payload), "Portfolio")
	var ownerField *FieldDoc
	for i := range result.Types[0].Fields {
		if result.Types[0].Fields[i].Name == "owner" {
			ownerField = &result.Types[0].Fields[i]
			break
		}
	}
	require.NotNil(t, ownerField)
	require.True(t, ownerField.WorkspaceMember)
	require.Contains(t, ownerField.Annotations, "workspaceMember")
	require.Len(t, result.Types[0].Enums, 1)
	for _, field := range result.Types[0].Fields {
		require.Nil(t, field.Enum)
		require.Nil(t, field.EnumValues)
	}
	after, err := RenderReferenceJSON(schema, "Project", identity.CurrentUserType)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "compaction must not mutate the compiled contract")
	person, err := CompactReference(schema, "Person", identity.CurrentUserType)
	require.NoError(t, err)
	require.Empty(t, person.Types[0].Summary)
	require.Empty(t, person.Types[0].Description)
	for _, name := range []string{"", "Missing", "_FallbackNote", "_FallbackSection"} {
		_, err := CompactReference(schema, name, identity.CurrentUserType)
		require.Error(t, err, name)
	}
}

func TestCompactAnnotationsPreservesUnknownBehavior(t *testing.T) {
	original := map[string]map[string]any{"requiresWhen": {"field": "state", "equals": "closed"}, "futureConstraint": {"min": 2}, "contains": {"heading": "Summary", "required": true, "display": "PANE"}, "guidance": {"meaning": "long prose"}}
	actual := compactAnnotations(original)
	require.Equal(t, original["requiresWhen"], actual["requiresWhen"])
	require.Equal(t, original["futureConstraint"], actual["futureConstraint"])
	require.Equal(t, true, actual["contains"]["required"])
	require.NotContains(t, actual["contains"], "display")
	require.Equal(t, "PANE", original["contains"]["display"])
	require.NotContains(t, actual, "guidance")
}
