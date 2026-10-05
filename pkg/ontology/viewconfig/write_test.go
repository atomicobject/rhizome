package viewconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplySavedLayoutKeepsTheFilesIndentation(t *testing.T) {
	in := `apiVersion: rhizome.view.v1
id: work
name: Work
source:
    kind: ontology_type
    type: Task
mount: {kind: type, type: Task}
defaults:
    variant: table
    sort:
        - field: title
variants:
    table:
        columns: [{field: title}]
`
	out, err := ApplySavedLayout([]byte(in), SavedLayout{Variant: "kanban", Columns: []ViewColumn{{Field: "title"}}, Sort: []SortSpec{{Field: "title"}}})
	require.NoError(t, err)
	require.Equal(t, `apiVersion: rhizome.view.v1
id: work
name: Work
source:
    kind: ontology_type
    type: Task
mount: {kind: type, type: Task}
defaults:
    variant: kanban
    sort:
        - field: title
variants:
    table:
        columns:
            - field: title
`, string(out), "only the changed keys differ")
}
