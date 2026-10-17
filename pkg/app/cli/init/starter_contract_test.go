package init

import (
	"testing"

	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
)

func TestInstalledStarterEffortsShareIdentifierNamespace(t *testing.T) {
	root := t.TempDir()
	installStarterOntologySchemas(t, root, templateAgenticEngineering)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	for _, name := range []string{"EffortNote", "EffortWorkspace"} {
		t.Run(name, func(t *testing.T) {
			noteType := schema.Types[name]
			require.NotNil(t, noteType)
			field := starterFields(noteType)["id"]
			require.NotNil(t, field)
			require.True(t, field.IsPreferredIdentifier)
			require.NotNil(t, field.IdentifierFormat)
			require.Equal(t, ontoschema.IdentifierStrategyDateTime, field.IdentifierFormat.Strategy)
			require.Equal(t, "EFF", field.IdentifierFormat.Prefix)
			contract, err := field.IdentifierFormat.StrategyContract()
			require.NoError(t, err)
			_, ok := contract.Parse("EFF-2026-08-05-14-32")
			require.True(t, ok)
		})
	}
}

func TestInstalledStarterStoryAuthoringShape(t *testing.T) {
	root := t.TempDir()
	installStarterOntologySchemas(t, root, templateAgenticEngineering)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	story := schema.Types["UserStory"]
	require.NotNil(t, story)
	status := starterFields(story)["status"]
	require.NotNil(t, status)
	values := schema.EnumTypes[status.TypeName]
	require.NotNil(t, values)
	var statuses []string
	for _, value := range values.Values {
		statuses = append(statuses, value.Name)
	}
	require.Contains(t, statuses, "ready")
	require.NotNil(t, starterFields(story)["increment"])
	criteria := starterFields(story)["acceptanceCriteria"]
	require.NotNil(t, criteria)
	require.Equal(t, "Acceptance Criteria", criteria.SectionHeading)
	require.True(t, criteria.SectionRequired)
	criteriaType := schema.Types[criteria.TypeName]
	require.NotNil(t, criteriaType)
	items := starterFields(criteriaType)["criteria"]
	require.NotNil(t, items)
	require.True(t, items.List)
	require.Equal(t, ontoschema.EmbeddedSourceShape("LIST_ITEM"), items.EmbeddedSourceShape)
	require.Equal(t, 1, items.ContainsMin)
}

func starterFields(noteType *ontoschema.NoteType) map[string]*ontoschema.Field {
	fields := map[string]*ontoschema.Field{}
	for _, field := range noteType.Fields {
		fields[field.Name] = field
	}
	return fields
}
