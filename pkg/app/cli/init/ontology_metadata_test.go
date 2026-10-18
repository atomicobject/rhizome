package init

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAgenticEngineeringSchemaExposesLifecycleMetadata(t *testing.T) {
	root := t.TempDir()
	installStarterOntologySchemas(t, root, templateAgenticEngineering)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	efforts, err := schemadoc.SchemaDocs(schema, "EffortNote", identity.CurrentUserType)
	require.NoError(t, err)
	require.Len(t, efforts, 1)
	fields := map[string]schemadoc.FieldDoc{}
	for _, field := range efforts[0].Fields {
		fields[field.Name] = field
	}
	for _, name := range []string{"createdAt", "planApprovedBy", "closureChecklist", "executionNotes"} {
		require.Contains(t, fields, name)
	}
	for _, name := range []string{"auditStatus", "backportStatus", "compoundStatus", "date", "time"} {
		require.NotContains(t, fields, name)
	}
	journal := fields["executionNotes"]
	require.NotNil(t, journal.Policy)
	require.True(t, journal.Policy.ForbidAutonomousSemanticEdits)
	require.Equal(t, "APPEND_ONLY", journal.Policy.EditScope)

	specs, err := schemadoc.SchemaDocs(schema, "ProductSpec", identity.CurrentUserType)
	require.NoError(t, err)
	require.Len(t, specs, 1)
	fields = map[string]schemadoc.FieldDoc{}
	for _, field := range specs[0].Fields {
		fields[field.Name] = field
	}
	require.Contains(t, fields, "id")
	require.True(t, fields["id"].Identifier)
	require.True(t, fields["id"].PreferredIdentifier)
	require.Contains(t, fields, "specStatus")
	require.NotNil(t, fields["specStatus"].Enum)
	var values []string
	for _, value := range fields["specStatus"].Enum.Values {
		values = append(values, value.Name)
	}
	require.Equal(t, []string{"proposed", "active", "superseded", "archived"}, values)
}
