package schemadoc

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSchemaDocsSurfaceFieldDisplayHints(t *testing.T) {
	root := t.TempDir()
	writeTestSchema(t, root, `type Entry @node(paths: ["notes/*.md"]) {
  abstract: String @display(role: SUMMARY, importance: KEY, hover: false)
  detail: String
}`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	docs, err := SchemaDocs(schema, "Entry", identity.CurrentUserType)
	require.NoError(t, err)
	require.Len(t, docs, 1)
	require.NotNil(t, docs[0].Fields[0].Display)
	require.Equal(t, ontoschema.FieldDisplayRoleSummary, docs[0].Fields[0].Display.Role)
	require.Equal(t, ontoschema.FieldImportanceKey, docs[0].Fields[0].Display.Importance)
	require.True(t, docs[0].Fields[0].Display.HideHover)
	require.Nil(t, docs[0].Fields[1].Display)
}
