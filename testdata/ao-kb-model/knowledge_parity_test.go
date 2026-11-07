package aokbmodel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type knowledgeExpectation struct{ Path, Kind, ID string }
type evidenceExpectation struct {
	Kind, Key, Field, Authored, Status string
	Targets                            []string
}
type connectedFixture struct {
	parityFixture
	Oracle struct {
		Rows                   map[string]map[string]map[string]any
		Edges, UnresolvedEdges []oracleEdge
		Knowledge              []knowledgeExpectation
		Evidence               []evidenceExpectation
		Files                  map[string]string
		// AOEvidenceEdges counts AO trace.py's unique evidenced-by edges and
		// those that are AO's arbitrary pick among ambiguous candidates.
		AOEvidenceEdges *struct{ Trace, Arbitrary int }
	}
}

func TestConnectedKnowledgeProviderParity(t *testing.T) {
	for _, test := range []struct{ name, dir, vault, intermediate string }{
		{"booking", "../ao-kb", "booking", "model-intermediate.json"},
		{"selected alpha", "../ao-kb-product", "account", "intermediate.json"},
	} {
		t.Run(test.name, func(t *testing.T) { assertConnectedParity(t, test.dir, test.vault, test.intermediate) })
	}
}
func connectedSchema(t *testing.T, dir string) *schema.Schema {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, ".rhizome/ontology")
	require.NoError(t, os.MkdirAll(target, 0755))
	static := "../../pkg/app/cli/init/templates/starters/ao-kb/module/ontology"
	files, err := filepath.Glob(filepath.Join(static, "*.graphql"))
	require.NoError(t, err)
	files = append(files, filepath.Join(dir, "expected-model.graphql"), filepath.Join(dir, "expected-knowledge.graphql"))
	for _, file := range files {
		bytes, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(target, filepath.Base(file)), bytes, 0644))
	}
	compiled, err := schema.LoadSchema(root)
	require.NoError(t, err)
	return compiled
}
func assertConnectedParity(t *testing.T, dir, vaultName, input string) {
	t.Helper()
	compiled := connectedSchema(t, dir)
	raw, err := os.ReadFile(filepath.Join(dir, input))
	require.NoError(t, err)
	var fixture connectedFixture
	require.NoError(t, json.Unmarshal(raw, &fixture))
	vault, err := filepath.Abs(filepath.Join(dir, vaultName))
	require.NoError(t, err)
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	sourcePaths := map[string]bool{}
	for _, source := range fixture.Sources {
		sourcePaths[source.Path] = true
	}
	require.NoError(t, filepath.WalkDir(vault, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(file, ".md") {
			return nil
		}
		rel, err := filepath.Rel(vault, file)
		if err != nil {
			return err
		}
		sourcePaths[filepath.ToSlash(rel)] = true
		return nil
	}))
	var sources []notemeta.NoteSourceSnapshot
	var documents []*ontology.PreparedDocument
	ordered := make([]string, 0, len(sourcePaths))
	for name := range sourcePaths {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		bytes, err := os.ReadFile(filepath.Join(vault, filepath.FromSlash(name)))
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		provider, ok := runtime.ProviderForPath(paths.Normalize(name))
		require.True(t, ok, name)
		authored, err := noteformat.NewAuthoredSource(paths.NotePath(name), provider.Descriptor(), bytes, 0)
		require.NoError(t, err)
		projection, err := runtime.Project(authored)
		require.NoError(t, err)
		source := notemeta.NoteSourceSnapshot{Path: paths.NotePath(name), Format: authored.Format(), RawSource: bytes, Content: string(bytes), ContentHash: authored.ContentHash(), Projection: projection}
		source.Frontmatter = map[string]any{}
		for _, fact := range projection.Facts.RootMetadata {
			source.Frontmatter[fact.Key] = fact.Value.Export()
		}
		for _, fact := range projection.Facts.Tags {
			source.Tags = append(source.Tags, fact.Value)
		}
		source.InlineProps = map[string][]string{}
		for _, fact := range projection.Facts.InlineProperties {
			source.InlineProps[fact.Key] = append(source.InlineProps[fact.Key], fact.Value)
		}
		if projection.Facts.Title != nil {
			source.Title = projection.Facts.Title.Value
		}
		sources = append(sources, source)
		root, err := ontology.BuildRootDocumentSnapshot(source)
		require.NoError(t, err)
		doc, err := ontology.PrepareDocument(root, compiled)
		require.NoError(t, err)
		documents = append(documents, doc)
	}
	ontology.BindRecordDocuments(documents...)
	identities := map[string]string{}
	knowledgePaths := map[string]string{}
	nodesByIdentity := map[string]*ontology.NodeProjection{}
	values := map[string]map[string]map[string]any{}
	addresses := map[string]string{}
	var knowledge []knowledgeExpectation
	var files int
	for _, doc := range documents {
		nodes, err := doc.Nodes()
		require.NoError(t, err)
		for _, node := range nodes {
			if node.Record == nil {
				kinds := map[string]string{"Assertion": "assertions", "Source": "sources", "Concept": "concepts", "Artifact": "artifacts"}
				kind, ok := kinds[node.ResolvedType]
				if !ok {
					continue
				}
				id := ""
				if vals := node.Fields["aoId"].Values; len(vals) > 0 {
					id = vals[0]
				}
				knowledge = append(knowledge, knowledgeExpectation{Path: node.Ref.NotePath, Kind: kind, ID: id})
				knowledgePaths[node.Ref.NotePath] = "assertion:" + node.Ref.NotePath
				continue
			}
			if node.Record.LogicalKind == "model-file" {
				files++
				if synced, ok := fixture.Oracle.Files[node.Ref.NotePath]; ok {
					require.Equal(t, []string{synced}, node.Fields["synced"].Values, "model file sync")
				}

				continue
			}
			identity := node.Record.LogicalKind + ":" + node.Record.LogicalKey
			require.NotContains(t, nodesByIdentity, identity, "selected product identities must be unique")
			identities[node.Ref.NodeID] = identity
			nodesByIdentity[identity] = node
			var row map[string]any
			require.NoError(t, json.Unmarshal([]byte(node.SourceContent().Text), &row))
			if values[node.Record.LogicalKind] == nil {
				values[node.Record.LogicalKind] = map[string]map[string]any{}
			}
			values[node.Record.LogicalKind][node.Record.LogicalKey] = row
			addresses[identity] = node.Ref.NotePath + "#" + node.SourceContent().Address
		}
	}
	sortKnowledge := func(values []knowledgeExpectation) {
		sort.Slice(values, func(i, j int) bool { return values[i].Path < values[j].Path })
	}
	sortKnowledge(knowledge)
	sortKnowledge(fixture.Oracle.Knowledge)
	require.Equal(t, fixture.Oracle.Knowledge, knowledge, "typed knowledge membership matches AO; broader source discovery stays enabled")
	require.Equal(t, fixture.Oracle.Rows, values)
	for _, record := range fixture.Records {
		require.Equal(t, record.Path+"#"+record.Address, addresses[record.Kind+":"+record.Key])
	}
	require.Positive(t, files, "file-level synced observations are available separately")
	var evidence []evidenceExpectation
	expectedFields := map[string]map[string]bool{}
	for _, expected := range fixture.Oracle.Evidence {
		identity := expected.Kind + ":" + expected.Key
		if expectedFields[identity] == nil {
			expectedFields[identity] = map[string]bool{}
		}
		expectedFields[identity][expected.Field] = true
	}
	var evidenceEdges []oracleEdge
	for identity, fields := range expectedFields {
		node := nodesByIdentity[identity]
		require.NotNil(t, node)
		for field := range fields {
			occurrences := node.Fields["v_"+strings.ReplaceAll(field, "_", "_5f_")].ReferenceOccurrences
			for _, occurrence := range occurrences {
				result := evidenceExpectation{Kind: node.Record.LogicalKind, Key: node.Record.LogicalKey, Field: field, Authored: occurrence.Authored, Status: "unresolved", Targets: []string{}}
				if occurrence.Target != nil {
					result.Status = "resolved"
					result.Targets = []string{occurrence.Target.NotePath}
					evidenceEdges = append(evidenceEdges, oracleEdge{From: identity, To: "assertion:" + occurrence.Target.NotePath, Rel: "evidenced-by"})
				}
				if occurrence.Diagnostic == "record_reference_ambiguous" {
					// AO's precedence: a file stem, else a frontmatter id.
					result.Status = "ambiguous"
					for _, byStem := range []bool{true, false} {
						for _, candidate := range knowledge {
							stem := strings.TrimSuffix(filepath.Base(candidate.Path), ".md")
							if candidate.Kind == "assertions" && ((byStem && stem == result.Authored) || (!byStem && candidate.ID == result.Authored)) {
								result.Targets = append(result.Targets, candidate.Path)
							}
						}
						if len(result.Targets) > 0 {
							break
						}
					}
					sort.Strings(result.Targets)
				}
				evidence = append(evidence, result)
			}
		}
	}
	// Sort records/fields while retaining repeated evidence occurrences within a field.
	sortEvidence := func(values []evidenceExpectation) {
		sort.SliceStable(values, func(i, j int) bool {
			return values[i].Kind+":"+values[i].Key+":"+values[i].Field < values[j].Kind+":"+values[j].Key+":"+values[j].Field
		})
	}
	sortEvidence(evidence)
	sortEvidence(fixture.Oracle.Evidence)
	require.Equal(t, fixture.Oracle.Evidence, evidence, "ordered evidence citations resolve only against AO-selected assertions")
	indexed, err := ontology.BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: vault}, sources, compiled, "fixture")
	require.NoError(t, err)
	var structural, actualEvidence []oracleEdge
	for _, edge := range indexed.Edges {
		from, fok := identities[edge.SrcNodeID]
		to, tok := identities[edge.DstNodeID]
		if edge.DstNodeID == "" {
			to, tok = knowledgePaths[edge.DstPath]
		}
		if !fok || !tok {
			continue
		}
		if strings.HasPrefix(from, "assertion:") {
			continue
		}
		item := oracleEdge{From: from, To: to, Rel: edge.RelationName}
		if edge.RelationName == "evidenced-by" {
			actualEvidence = append(actualEvidence, item)
		} else {
			var occurrences []readmodel.RelationOccurrence
			if edge.OccurrencesJSON != "" {
				require.NoError(t, json.Unmarshal([]byte(edge.OccurrencesJSON), &occurrences))
			}
			if len(occurrences) == 0 {
				structural = append(structural, item)
			}
			for _, occurrence := range occurrences {
				copy := item
				copy.At = occurrence.At
				structural = append(structural, copy)
			}
		}
	}
	require.Equal(t, edgeSet(fixture.Oracle.Edges), edgeSet(structural))
	require.Equal(t, edgeSet(evidenceEdges), edgeSet(actualEvidence))
	if counts := fixture.Oracle.AOEvidenceEdges; counts != nil {
		require.Len(t, edgeSet(actualEvidence), counts.Trace-counts.Arbitrary, "AO's evidence edges, less its arbitrary picks among ambiguous claims")
	}
}
