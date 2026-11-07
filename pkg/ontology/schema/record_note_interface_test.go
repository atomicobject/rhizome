package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordSourcesDoNotInheritAuthoredNoteFields(t *testing.T) {
	schema, err := recordSchema(t, `interface Note { summary:String }
type Host @node(default:true){status:String}
type Row @node(locator:EMBEDDED) `+recordTestSource+` {id:ID @field(path:"id")}`)
	require.NoError(t, err)
	require.Contains(t, schema.Types["Host"].ByName, "summary")
	require.NotContains(t, schema.Types["Row"].ByName, "summary")
	require.False(t, TypeMatchesOrImplements(schema, "Row", "Note"))
}
