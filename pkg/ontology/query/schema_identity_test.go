package query

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutableSchemaRetainsCompiledSourceIdentity(t *testing.T) {
	schema := &ontoschema.Schema{Hash: "original", Types: map[string]*ontoschema.NoteType{}, Interfaces: map[string]*ontoschema.InterfaceType{}}
	exec, err := BuildExecutableSchema(schema)
	require.NoError(t, err)
	require.True(t, exec.MatchesOntologySchema(schema))
	other := *schema
	require.False(t, exec.MatchesOntologySchema(&other), "equal public hashes do not prove the same compiled source")
	schema.Hash = "changed"
	require.False(t, exec.MatchesOntologySchema(schema))
	require.False(t, exec.MatchesOntologySchema(nil))
}
