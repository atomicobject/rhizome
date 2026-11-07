package ontology

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type structuredOracleEdge struct{ From, To, Rel string }

func TestStructuredAOCheckedOracleRowsDuplicatesAndEdges(t *testing.T) {
	var oracle struct {
		ToolkitRevision, PyYAML string
		Cases                   map[string]struct {
			Source        string
			Rows          map[string]map[string]map[string]any
			DuplicateRows [][]any
			Edges         []structuredOracleEdge
		}
	}
	raw, err := os.ReadFile("testdata/structured-ao/expected.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &oracle))
	require.Equal(t, "e9dce951ab4cd6aa9ccc734df96590bde25ccde9", oracle.ToolkitRevision)
	require.Equal(t, "6.0.3", oracle.PyYAML)
	schema := recordFixtureSchema(t, strings.ReplaceAll(recordRowsSDL, "*.native", "*.yaml"))
	for name, fixture := range oracle.Cases {
		t.Run(name, func(t *testing.T) {
			source := testkit.NoteSource(t, "model.yaml", fixture.Source, 1)
			root, err := ProjectNoteSourceSnapshot(source, schema)
			require.NoError(t, err)
			nodes, err := root.Prepared.Nodes()
			require.NoError(t, err)
			rows := map[string]map[string]any{}
			identities := map[string]string{}
			counts := map[string]int{}
			for _, node := range nodes {
				if node.Record == nil {
					continue
				}
				key := node.Record.LogicalKey
				var value map[string]any
				require.NoError(t, json.Unmarshal([]byte(node.SourceContent().Text), &value))
				rows[key] = value
				identities[node.Ref.NodeID] = "row:" + key
			}
			for _, record := range root.Prepared.records {
				counts[record.identity.LogicalKey]++
			}
			indexed, err := BuildIndexFromNoteSources(t.Context(), obsidian.VaultDefinition{}, []notemeta.NoteSourceSnapshot{source}, schema, "oracle")
			require.NoError(t, err)
			diagnostics := map[string]int{}
			for _, diagnostic := range root.Prepared.Diagnostics() {
				diagnostics[diagnostic.Code]++
			}
			if len(fixture.DuplicateRows) > 0 {
				for _, duplicate := range fixture.DuplicateRows {
					require.EqualValues(t, duplicate[2], counts[duplicate[1].(string)], "all AO duplicate candidates remain visible")
				}
				require.Positive(t, diagnostics["record_key_duplicate"])
				for _, edge := range indexed.Edges {
					require.NotEqual(t, "uses", edge.RelationName, "an ambiguous ID never chooses AO's last winner")
				}
				return
			}
			require.Equal(t, fixture.Rows["row"], rows)
			want := map[structuredOracleEdge]int{}
			for _, edge := range fixture.Edges {
				if _, exists := rows[strings.TrimPrefix(edge.To, "row:")]; exists {
					want[edge]++
				} else {
					require.Positive(t, diagnostics["record_reference_unresolved"])
				}
			}
			got := map[structuredOracleEdge]int{}
			for _, edge := range indexed.Edges {
				if edge.RelationName != "uses" {
					continue
				}
				var occurrences []readmodel.RelationOccurrence
				require.NoError(t, json.Unmarshal([]byte(edge.OccurrencesJSON), &occurrences))
				got[structuredOracleEdge{identities[edge.SrcNodeID], identities[edge.DstNodeID], edge.RelationName}] += len(occurrences)
			}
			require.Equal(t, want, got)
		})
	}
}
