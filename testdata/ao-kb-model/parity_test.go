package aokbmodel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type oracleEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"`
	At   string `json:"at,omitempty"`
}

type parityFixture struct {
	Sources []struct{ Path string }
	Records []struct{ Kind, Key, Path, Address string }
	Oracle  struct {
		Rows            map[string]map[string]map[string]any
		Edges           []oracleEdge
		UnresolvedEdges []oracleEdge
	}
}

func TestProviderParity(t *testing.T) {
	if os.Getenv("AOKB_RUN_PROVIDER_PARITY") != "1" {
		t.Skip("requires Lane Y2 YAML/JSON registration; set AOKB_RUN_PROVIDER_PARITY=1 after integration")
	}
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	assertProviderParity(t, runtime)
}

// Lane Y2 may call this with its explicitly configured runtime if registration is opt-in.
func assertProviderParity(t *testing.T, runtime noteformat.Runtime) {
	t.Helper()
	compiled := fixtureSchema(t)
	raw, err := os.ReadFile("intermediate.json")
	require.NoError(t, err)
	var fixture parityFixture
	require.NoError(t, json.Unmarshal(raw, &fixture))
	vault, err := filepath.Abs("account")
	require.NoError(t, err)
	seen := map[string]bool{}
	var documents []*ontology.PreparedDocument
	var sources []notemeta.NoteSourceSnapshot
	for _, source := range fixture.Sources {
		if seen[source.Path] {
			continue
		}
		seen[source.Path] = true
		raw, err := os.ReadFile(filepath.Join(vault, filepath.FromSlash(source.Path)))
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		provider, ok := runtime.ProviderForPath(paths.Normalize(source.Path))
		require.True(t, ok, "Lane Y2 must register the provider for %s", source.Path)
		authored, err := noteformat.NewAuthoredSource(paths.NotePath(source.Path), provider.Descriptor(), raw, 0)
		require.NoError(t, err)
		projection, err := runtime.Project(authored)
		require.NoError(t, err)
		snapshot := notemeta.NoteSourceSnapshot{Path: paths.NotePath(source.Path), Format: authored.Format(), RawSource: raw, Projection: projection}
		sources = append(sources, snapshot)
		root, err := ontology.BuildRootDocumentSnapshot(snapshot)
		require.NoError(t, err)
		document, err := ontology.PrepareDocument(root, compiled)
		require.NoError(t, err)
		documents = append(documents, document)
	}
	ontology.BindRecordDocuments(documents...)
	identities := map[string]string{}
	values := map[string]map[string]map[string]any{}
	addresses := map[string]string{}
	var unresolved int
	for _, document := range documents {
		nodes, err := document.Nodes()
		require.NoError(t, err)
		for _, node := range nodes {
			if node.Record == nil {
				continue
			}
			identity := node.Record.LogicalKind + ":" + node.Record.LogicalKey
			require.NotContains(t, identities, node.Ref.NodeID)
			identities[node.Ref.NodeID] = identity
			content := node.SourceContent()
			addresses[identity] = node.Ref.NotePath + "#" + content.Address
			var native any
			require.NoError(t, json.Unmarshal([]byte(content.Text), &native))
			row, ok := native.(map[string]any)
			if !ok {
				row = map[string]any{"id": native}
			}
			if group, ok := node.Fields["aoGroup"]; ok && len(group.Values) > 0 {
				row["group"] = group.Values[0]
			}
			if values[node.Record.LogicalKind] == nil {
				values[node.Record.LogicalKind] = map[string]map[string]any{}
			}
			require.NotContains(t, values[node.Record.LogicalKind], node.Record.LogicalKey, "logical keys must be unique")
			values[node.Record.LogicalKind][node.Record.LogicalKey] = row
			if identity == "service:S1" {
				var authored []string
				for _, occurrence := range node.Fields["v_uses"].ReferenceOccurrences {
					authored = append(authored, occurrence.Authored)
				}
				require.Equal(t, []string{"SHARED", "SHARED", "MISSING"}, authored)
			}
		}
		for _, diagnostic := range document.Diagnostics() {
			if diagnostic.Code == "record_reference_unresolved" {
				unresolved++
			}
		}
	}
	require.Equal(t, fixture.Oracle.Rows, values)
	for _, record := range fixture.Records {
		require.Equal(t, record.Path+"#"+record.Address, addresses[record.Kind+":"+record.Key])
	}
	if len(fixture.Oracle.UnresolvedEdges) > 0 {
		require.Positive(t, unresolved, "unresolved AO edges must retain Rhizome diagnostics")
	}
	indexed, err := ontology.BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: vault}, sources, compiled, "fixture")
	require.NoError(t, err)
	var edges []oracleEdge
	for _, edge := range indexed.Edges {
		from, fok := identities[edge.SrcNodeID]
		to, tok := identities[edge.DstNodeID]
		if !fok || !tok {
			continue
		}
		var occurrences []readmodel.RelationOccurrence
		if edge.OccurrencesJSON != "" {
			require.NoError(t, json.Unmarshal([]byte(edge.OccurrencesJSON), &occurrences))
		}
		if len(occurrences) == 0 {
			edges = append(edges, oracleEdge{From: from, To: to, Rel: edge.RelationName})
		}
		for _, occurrence := range occurrences {
			edges = append(edges, oracleEdge{From: from, To: to, Rel: edge.RelationName, At: occurrence.At})
		}
	}
	require.Equal(t, edgeSet(fixture.Oracle.Edges), edgeSet(edges))
}

func edgeSet(edges []oracleEdge) []string {
	set := map[string]bool{}
	for _, edge := range edges {
		encoded, _ := json.Marshal(edge)
		set[string(encoded)] = true
	}
	result := make([]string, 0, len(set))
	for encoded := range set {
		result = append(result, encoded)
	}
	sort.Strings(result)
	return result
}
