package ontology_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/query"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const (
	aoStaticKnowledgeSDL = "../app/cli/init/templates/starters/ao-kb/module/ontology"
	aoAccountModel       = "artifacts/_assets/_model/features.yaml"
	aoAlphaModel         = "products/alpha/artifacts/_assets/_model/features.yaml"
)

// aoVault is a disposable copy of a checked-in AO fixture with its compiled
// bindings installed. Every helper write updates the authored-byte ledger, so
// any byte Rhizome changes on its own fails requireAuthoredBytes.
type aoVault struct {
	t        *testing.T
	root     string
	vault    obsidian.VaultDefinition
	indexer  notemeta.Indexer
	db       string
	store    *semdb.Store
	authored map[string][]byte
	stamp    time.Time
}

func newAOVault(t *testing.T, fixture string, includes []string, sdl ...string) *aoVault {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(fixture)))
	v := &aoVault{t: t, root: root, vault: obsidian.VaultDefinition{Root: root, Includes: includes}, indexer: testkit.NoteMetadataIndexer(t), db: filepath.Join(t.TempDir(), "incremental.sqlite"), authored: map[string][]byte{}, stamp: time.Unix(1900000000, 0)}
	for _, file := range sdl {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome/ontology"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology", filepath.Base(file)), data, 0o644))
	}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		v.authored[filepath.ToSlash(rel)], err = os.ReadFile(path)
		return err
	}))
	store, err := semdb.Open(v.db)
	require.NoError(t, err)
	v.store = store
	t.Cleanup(func() { _ = v.store.Close() })
	return v
}

func (v *aoVault) write(path, content string) {
	v.t.Helper()
	full := filepath.Join(v.root, filepath.FromSlash(path))
	require.NoError(v.t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(v.t, os.WriteFile(full, []byte(content), 0o644))
	v.stamp = v.stamp.Add(time.Second)
	require.NoError(v.t, os.Chtimes(full, v.stamp, v.stamp))
	v.authored[path] = []byte(content)
}

func (v *aoVault) replace(path, old, new string) {
	v.t.Helper()
	current := string(v.authored[path])
	require.Contains(v.t, current, old, path)
	v.write(path, strings.Replace(current, old, new, 1))
}

func (v *aoVault) remove(path string) {
	v.t.Helper()
	require.NoError(v.t, os.Remove(filepath.Join(v.root, filepath.FromSlash(path))))
	delete(v.authored, path)
}

func (v *aoVault) sync(changed, deleted []string) {
	v.t.Helper()
	_, err := ontology.SyncPathsForTest(v.t.Context(), v.indexer, v.vault, &obsidian.Note{}, v.store, nil, changed, deleted)
	require.NoError(v.t, err)
}

// requireConverged compares the incremental store with a fresh full index of
// the same files: sources, records, field values, edges/occurrences, assessments
// and projection diagnostics.
func (v *aoVault) requireConverged() {
	v.t.Helper()
	full, err := semdb.Open(filepath.Join(v.t.TempDir(), "full.sqlite"))
	require.NoError(v.t, err)
	defer full.Close()
	_, err = ontology.SyncPathsForTest(v.t.Context(), v.indexer, v.vault, &obsidian.Note{}, full, nil, nil, nil)
	require.NoError(v.t, err)
	require.Equal(v.t, ontology.StructuredSnapshotForTest(v.t, full), ontology.StructuredSnapshotForTest(v.t, v.store))
	v.requireAuthoredBytes()
}

// requireAuthoredBytes proves indexing and reads wrote no authored byte and no
// file outside Rhizome's own .rhizome state.
func (v *aoVault) requireAuthoredBytes() {
	v.t.Helper()
	seen := map[string]bool{}
	require.NoError(v.t, filepath.WalkDir(v.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(v.root, path)
		require.NoError(v.t, err)
		rel = filepath.ToSlash(rel)
		want, authored := v.authored[rel]
		if !authored {
			require.True(v.t, strings.HasPrefix(rel, ".rhizome/"), "unexpected file %s", rel)
			return nil
		}
		got, err := os.ReadFile(path)
		require.NoError(v.t, err)
		require.Equal(v.t, string(want), string(got), rel)
		seen[rel] = true
		return nil
	}))
	for path := range v.authored {
		require.True(v.t, seen[path], "authored file disappeared: %s", path)
	}
}

func (v *aoVault) records(paths ...string) map[string]readmodelRecord {
	v.t.Helper()
	nodes, err := v.store.OntologyNodesByPaths(v.t.Context(), paths)
	require.NoError(v.t, err)
	out := map[string]readmodelRecord{}
	for _, node := range nodes {
		if node.RecordJSON == "" {
			continue
		}
		var identity ontology.RecordIdentity
		require.NoError(v.t, json.Unmarshal([]byte(node.RecordJSON), &identity))
		if identity.LogicalKind == "model-file" {
			continue
		}
		// Edges name their endpoint by the record ref's NodeID, not the catalog row ID.
		var ref ontology.NodeRef
		require.NoError(v.t, json.Unmarshal([]byte(node.NodeRefJSON), &ref))
		out[ref.NodeID] = readmodelRecord{key: identity.LogicalKind + ":" + identity.LogicalKey, title: node.Title, path: node.NotePath, rowID: node.NodeID}
	}
	return out
}

type readmodelRecord struct{ key, title, path, rowID string }

// evidence lists every model-record evidence edge as "kind:key -> assertion path".
func (v *aoVault) evidence(paths ...string) []string {
	v.t.Helper()
	records := v.records(paths...)
	edges, err := v.store.OntologyEdgesForPaths(v.t.Context(), paths, true, "", 0)
	require.NoError(v.t, err)
	out := []string{}
	for _, edge := range edges {
		if edge.RelationName == "evidenced-by" {
			out = append(out, records[edge.SrcNodeID].key+" -> "+edge.DstPath)
		}
	}
	slices.Sort(out)
	return out
}

func (v *aoVault) diagnostics(path string) []string {
	v.t.Helper()
	assessments, err := v.store.OntologyAssessmentsByPaths(v.t.Context(), []string{path})
	require.NoError(v.t, err)
	codes := []string{}
	if row, ok := assessments[path]; ok {
		assessment, err := ontology.AssessmentFromJSON(row.AssessmentJSON)
		require.NoError(v.t, err)
		for _, issue := range assessment.Issues {
			codes = append(codes, issue.Code)
		}
	}
	slices.Sort(codes)
	return codes
}

func (v *aoVault) fieldValue(path, key, field string) []string {
	v.t.Helper()
	records := v.records(path)
	ids := []string{}
	for _, record := range records {
		if record.key == key {
			ids = append(ids, record.rowID)
		}
	}
	require.Len(v.t, ids, 1, key)
	rows, err := v.store.OntologyNodeFieldValuesByNodeIDs(v.t.Context(), ids, nil)
	require.NoError(v.t, err)
	values := []string{}
	for _, row := range rows {
		if strings.EqualFold(row.FieldName, field) {
			values = append(values, row.ValueText)
		}
	}
	return values
}

// richRead runs summary and rich GraphQL selections (content, workspace,
// structure, outbound evidence traversal) over the published store.
func (v *aoVault) richRead(document string) map[string]any {
	v.t.Helper()
	schema, err := ontoschema.LoadSchema(v.root)
	require.NoError(v.t, err)
	executable, err := query.BuildExecutableSchema(schema)
	require.NoError(v.t, err)
	formats, err := v.indexer.FormatRuntime()
	require.NoError(v.t, err)
	prepared, errs := query.Prepare(executable, document)
	require.Empty(v.t, errs)
	result := query.Execute(v.t.Context(), query.Deps{VaultDef: v.vault, NoteReader: &obsidian.Note{}, NoteFormats: formats, Store: v.store}, schema, prepared)
	require.Empty(v.t, result.Errors)
	return result.Data
}

func aoProductVault(t *testing.T) *aoVault {
	t.Helper()
	static, err := filepath.Glob(filepath.Join(aoStaticKnowledgeSDL, "*.graphql"))
	require.NoError(t, err)
	require.NotEmpty(t, static)
	sdl := append(static, "../../testdata/ao-kb-product/expected-model.graphql", "../../testdata/ao-kb-product/expected-knowledge.graphql")
	return newAOVault(t, "../../testdata/ao-kb-product/account", []string{"**/*.md", "artifacts/_assets/_model/*.yaml", "products/alpha/artifacts/_assets/_model/*.yaml"}, sdl...)
}

const aoProductRichRead = `{
  features: aOKBModel_feature_product(first: 20) { title aoValue content
    workspace { source capabilities { canEdit readOnlyReason } fields { name values } }
    evidence: neighborhood(direction: OUTBOUND, relation: "evidenced-by", first: 50) { truncated edges { target { notePath } } } }
  account: aOKBModel_feature_account(first: 20) { title aoValue content workspace { source fields { name values } } }
  files: aOKBModelFile(first: 20) { synced workspace { structure { ref { notePath fragment } } } }
  assertion(first: 50) { aoId path content workspace { capabilities { canEdit readOnlyReason } bodies { markdown } } }
}`

// SPEC-0133 R4/Batch 5: after AO-workflow-equivalent edits to the selected
// product fixture and its compiled bindings, every incremental publication
// equals a fresh full index, evidence resolves and unresolves with its targets,
// and indexing plus rich reads leave authored bytes unchanged.
func TestAOProductExternalEditsConvergeWithFullIndex(t *testing.T) {
	v := aoProductVault(t)
	v.sync(nil, nil)
	v.requireConverged()
	evidence := []string{
		"feature:FEAT-alpha -> assertions/alpha/claim.md",
		"feature:FEAT-alpha -> assertions/alpha/collision.md",
		"feature:FEAT-alpha -> assertions/shared.md",
		"feature:FEAT-collision -> assertions/alpha/claim.md",
		"feature:FEAT-collision -> assertions/alpha/collision.md",
		"feature:FEAT-collision -> assertions/shared.md",
		"feature:FEAT-shared -> assertions/selected.md",
		"feature:FEAT-shared -> assertions/untagged.md",
	}
	models := []string{aoAccountModel, aoAlphaModel}
	require.Equal(t, evidence, v.evidence(models...), "initial evidence matches the checked AO oracle")
	require.Equal(t, []string{"record_reference_ambiguous", "record_reference_unresolved"}, v.diagnostics(aoAlphaModel))
	data := v.richRead(aoProductRichRead)
	require.Len(t, data["features"], 1)
	require.Len(t, data["account"], 2)
	v.requireAuthoredBytes()
	sharedAssertion := string(v.authored["assertions/shared.md"])

	keyedIDs := func() map[string]string {
		ids := map[string]string{}
		for id, record := range v.records(aoAccountModel) {
			ids[record.key] = id
		}
		return ids
	}
	var beforeReorder map[string]string
	for _, step := range []struct {
		name    string
		apply   func() (changed, deleted []string)
		check   func(t *testing.T)
		without []string
		with    []string
	}{
		{name: "change a model row value", apply: func() ([]string, []string) {
			v.replace(aoAccountModel, "t: Alpha account feature", "t: Alpha account feature renamed")
			return []string{aoAccountModel}, nil
		}, check: func(t *testing.T) {
			titles := []string{}
			for _, record := range v.records(aoAccountModel) {
				titles = append(titles, record.title)
			}
			require.Contains(t, titles, "Alpha account feature renamed")
		}},
		{name: "add a row", apply: func() ([]string, []string) {
			v.write(aoAlphaModel, string(v.authored[aoAlphaModel])+"  - {id: FEAT-new, t: New alpha feature, supported_by: [a-selected]}\n")
			return []string{aoAlphaModel}, nil
		}, with: []string{"feature:FEAT-new -> assertions/selected.md"}},
		{name: "reorder keyed rows", apply: func() ([]string, []string) {
			beforeReorder = keyedIDs()
			lines := strings.SplitAfter(string(v.authored[aoAccountModel]), "\n")
			// The synced and features header lines stay; every row reverses.
			rows := lines[2 : len(lines)-1]
			slices.Reverse(rows)
			v.write(aoAccountModel, strings.Join(append(append(lines[:2:2], rows...), lines[len(lines)-1]), ""))
			return []string{aoAccountModel}, nil
		}, check: func(t *testing.T) {
			require.Equal(t, beforeReorder, keyedIDs(), "keyed records keep their identity across reorder")
		}},
		{name: "delete a row", apply: func() ([]string, []string) {
			v.replace(aoAccountModel, "  - {id: FEAT-shared, t: Shared account feature, products: [\"*\"], supported_by: [a-untagged, a-selected]}\n", "")
			return []string{aoAccountModel}, nil
		}, without: []string{"feature:FEAT-shared -> assertions/selected.md", "feature:FEAT-shared -> assertions/untagged.md"}},
		{name: "edit an assertion headline and body", apply: func() ([]string, []string) {
			v.replace("assertions/selected.md", "headline: Synthetic selected.md", "headline: Revised selected claim")
			v.replace("assertions/selected.md", "Illustrative claim.", "Illustrative claim, revised by AO curation.")
			return []string{"assertions/selected.md"}, nil
		}, check: func(t *testing.T) {
			for _, row := range v.richRead(`{ assertion(path: "assertions/selected.md", first: 1) { content } }`)["assertion"].([]any) {
				require.Contains(t, row.(map[string]any)["content"], "revised by AO curation")
			}
		}},
		{name: "delete a cited assertion", apply: func() ([]string, []string) {
			v.remove("assertions/shared.md")
			return nil, []string{"assertions/shared.md"}
		}, without: []string{"feature:FEAT-alpha -> assertions/shared.md", "feature:FEAT-collision -> assertions/shared.md"}, check: func(t *testing.T) {
			require.Contains(t, v.diagnostics(aoAccountModel), "record_reference_unresolved")
		}},
		{name: "restore the cited assertion", apply: func() ([]string, []string) {
			v.write("assertions/shared.md", sharedAssertion)
			return []string{"assertions/shared.md"}, nil
		}, with: []string{"feature:FEAT-alpha -> assertions/shared.md", "feature:FEAT-collision -> assertions/shared.md"}, check: func(t *testing.T) {
			require.Empty(t, v.diagnostics(aoAccountModel))
		}},
		{name: "author a value the bindings do not declare yet", apply: func() ([]string, []string) {
			v.replace(aoAlphaModel, "    project_detail: alpha override\n", "    project_detail: alpha override\n    priority: high\n")
			return []string{aoAlphaModel}, nil
		}, check: func(t *testing.T) {
			require.Empty(t, v.fieldValue(aoAlphaModel, "feature:FEAT-collision", "v_priority"))
		}},
		{name: "change the project override and refresh bindings", apply: func() ([]string, []string) {
			v.replace("products/alpha/.kb/config/metamodel.yaml", "      project_detail: {type: text}\n", "      project_detail: {type: text}\n      priority: {type: text}\n")
			// The compiler output for the new override, without running AO: the
			// added text field appears in both feature bindings.
			sdl := ".rhizome/ontology/expected-model.graphql"
			declared := "  v_project_5f_detail: String @field(path: \"project_detail\")\n"
			require.Equal(t, 2, strings.Count(string(v.authored[sdl]), declared))
			v.write(sdl, strings.ReplaceAll(string(v.authored[sdl]), declared, declared+"  v_priority: String @field(path: \"priority\")\n"))
			// Neither file is a note: the schema change itself replans every host.
			return nil, nil
		}, check: func(t *testing.T) {
			require.Equal(t, []string{"high"}, v.fieldValue(aoAlphaModel, "feature:FEAT-collision", "v_priority"))
		}},
	} {
		t.Run(step.name, func(t *testing.T) {
			v.t = t
			changed, deleted := step.apply()
			v.sync(changed, deleted)
			v.requireConverged()
			evidence = slices.DeleteFunc(evidence, func(edge string) bool { return slices.Contains(step.without, edge) })
			evidence = append(evidence, step.with...)
			slices.Sort(evidence)
			require.Equal(t, evidence, v.evidence(models...))
			if step.check != nil {
				step.check(t)
			}
			graphEvidence := []string{}
			for _, row := range v.richRead(aoProductRichRead)["features"].([]any) {
				feature := row.(map[string]any)
				for _, edge := range feature["evidence"].(map[string]any)["edges"].([]any) {
					graphEvidence = append(graphEvidence, "feature:"+feature["aoValue"].(string)+" -> "+edge.(map[string]any)["target"].(map[string]any)["notePath"].(string))
				}
			}
			slices.Sort(graphEvidence)
			require.Equal(t, v.evidence(aoAlphaModel), graphEvidence, "GraphQL traversal agrees with the published edges")
			v.requireAuthoredBytes()
		})
	}
	v.t = t
	before := ontology.StructuredSnapshotForTest(t, v.store)
	require.NoError(t, v.store.Close())
	var err error
	v.store, err = semdb.Open(v.db)
	require.NoError(t, err)
	require.Equal(t, before, ontology.StructuredSnapshotForTest(t, v.store), "restart retains the materialization")
	v.sync(nil, nil)
	v.requireConverged()
}

// The selected-product fixture has no positional model rows; the model-harness
// fixture's compiled bindings number lane positions account-first.
func TestAOPositionalRowsConvergeWithFullIndex(t *testing.T) {
	const lane = "products/alpha/artifacts/_assets/_model/lane.yaml"
	v := newAOVault(t, "../../testdata/ao-kb-model/account", []string{"artifacts/_assets/_model/lane.yaml", lane, "products/alpha/artifacts/_assets/engine-blocks/rows.json"}, "../../testdata/ao-kb-model/expected.graphql")
	v.sync(nil, nil)
	v.requireConverged()
	positions := func() []string {
		out := []string{}
		for _, record := range v.records("artifacts/_assets/_model/lane.yaml", lane) {
			if strings.HasPrefix(record.key, "position:") {
				out = append(out, record.key+"="+record.title)
			}
		}
		slices.Sort(out)
		return out
	}
	require.Equal(t, []string{"position:1=Account first", "position:2=Product second"}, positions())
	for _, step := range []struct {
		name, old, new string
		want           []string
	}{
		{"add a positional row", "- name: Product second\n", "- name: Product second\n- name: Product third\n", []string{"position:1=Account first", "position:2=Product second", "position:3=Product third"}},
		{"reorder positional rows", "- name: Product second\n- name: Product third\n", "- name: Product third\n- name: Product second\n", []string{"position:1=Account first", "position:2=Product third", "position:3=Product second"}},
		{"delete a positional row", "- name: Product third\n", "", []string{"position:1=Account first", "position:2=Product second"}},
	} {
		t.Run(step.name, func(t *testing.T) {
			v.t = t
			v.replace(lane, step.old, step.new)
			v.sync([]string{lane}, nil)
			v.requireConverged()
			require.Equal(t, step.want, positions())
		})
	}
}
