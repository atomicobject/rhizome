package query

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"

	"github.com/stretchr/testify/require"
)

const referenceRegressionSDL = `type Host @node(default:true){status:String}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"test",logicalKind:"row") {
 id:ID @field(path:"id")
 refs:[Row!] @field(path:"refs") @references(to:["Row"],mode:STRICT,values:ELEMENTS,edge:{name:"uses"})
}`

func TestRichRecordReferencesPreserveGlobalAmbiguity(t *testing.T) {
	env := newConfiguredQueryTestEnv(t, referenceRegressionSDL, map[string]string{
		"a.yaml": "rows: [{id: A, refs: [B]}, {id: B}]\n",
		"b.yaml": "rows: [{id: B}]\n",
	}, []string{"*.yaml"})
	for _, selection := range []string{"id refs {id}", "id content refs {id}"} {
		q, errs := Prepare(env.execSchema, `{row(filters:[{field:"id",op:eq,value:"A"}]){`+selection+`}}`)
		require.Empty(t, errs)
		result := Execute(t.Context(), env.deps(nil), env.schema, q)
		require.Empty(t, result.Errors)
		rows := result.Data["row"].([]any)
		require.Len(t, rows, 1)
		require.Empty(t, rows[0].(map[string]any)["refs"], selection)
	}
}

func TestPresentEmptyRecordReferencesMatchExists(t *testing.T) {
	env := newConfiguredQueryTestEnv(t, referenceRegressionSDL, map[string]string{
		"a.yaml": "rows: [{id: empty, refs: []}, {id: missing}]\n",
	}, []string{"*.yaml"})
	for _, selection := range []string{"id refs {id}", "id content refs {id}"} {
		q, errs := Prepare(env.execSchema, `{row(filters:[{field:"refs",op:exists}]){`+selection+`}}`)
		require.Empty(t, errs)
		result := Execute(t.Context(), env.deps(nil), env.schema, q)
		require.Empty(t, result.Errors)
		rows := result.Data["row"].([]any)
		require.Len(t, rows, 1)
		require.Equal(t, "empty", rows[0].(map[string]any)["id"])
		require.Empty(t, rows[0].(map[string]any)["refs"])
	}
}

func TestWorkspaceLinksKeepIdenticalIDsInDifferentKindsDistinct(t *testing.T) {
	sdl := `type Host @node(default:true){status:String}
type Alpha @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"alphas",key:{field:"id"},title:["name"],space:"test",logicalKind:"alpha"){ id:ID @field(path:"id") name:String @field(path:"name") }
type Beta @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"betas",key:{field:"id"},title:["name"],space:"test",logicalKind:"beta"){ id:ID @field(path:"id") name:String @field(path:"name") }
type Item @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"items",key:{field:"id"},title:["id"],space:"test",logicalKind:"item"){
 id:ID @field(path:"id")
 alpha:Alpha @field(path:"alpha") @references(to:["Alpha"],mode:STRICT,values:VALUE)
 beta:Beta @field(path:"beta") @references(to:["Beta"],mode:STRICT,values:VALUE)
}`
	env := newConfiguredQueryTestEnv(t, sdl, map[string]string{
		"model.yaml": "alphas: [{id: X, name: Alpha X}]\nbetas: [{id: X, name: Beta X}]\nitems: [{id: I, alpha: X, beta: X}]\n",
	}, []string{"*.yaml"})
	q, errs := Prepare(env.execSchema, `{item{alpha{name} beta{name} workspace{fields{name links{value resolved title}}}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	item := result.Data["item"].([]any)[0].(map[string]any)
	require.Equal(t, map[string]any{"name": "Alpha X"}, item["alpha"])
	require.Equal(t, map[string]any{"name": "Beta X"}, item["beta"])
	titles := map[string]any{}
	for _, raw := range item["workspace"].(map[string]any)["fields"].([]any) {
		field := raw.(map[string]any)
		for _, link := range field["links"].([]any) {
			titles[field["name"].(string)] = link.(map[string]any)["title"]
		}
	}
	require.Equal(t, map[string]any{"alpha": "Alpha X", "beta": "Beta X"}, titles)
}

// countingReadStore counts the batched catalog, field and metadata reads
// behind record reference selections.
type countingReadStore struct {
	*codeanchorsqlite.Store
	reads atomic.Int64
}

func (s *countingReadStore) OntologyNodeFieldValuesByNodeIDs(ctx context.Context, nodeIDs, fields []string) ([]readmodel.FieldValueRow, error) {
	s.reads.Add(1)
	return s.Store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, fields)
}
func (s *countingReadStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]codeanchorsqlite.NoteMetadataRow, error) {
	s.reads.Add(1)
	return s.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
}
func (s *countingReadStore) OntologyTypesByPaths(ctx context.Context, paths []string) (map[string]codeanchorsqlite.OntologyNoteTypeRow, error) {
	s.reads.Add(1)
	return s.Store.OntologyTypesByPaths(ctx, paths)
}
func (s *countingReadStore) OntologyNodesByPaths(ctx context.Context, paths []string) ([]readmodel.NodeRow, error) {
	s.reads.Add(1)
	return s.Store.OntologyNodesByPaths(ctx, paths)
}
func (s *countingReadStore) OntologyNodesBySourceLocators(ctx context.Context, locators []string) (map[string]readmodel.NodeRow, error) {
	s.reads.Add(1)
	return s.Store.OntologyNodesBySourceLocators(ctx, locators)
}

func TestRecordReferenceReadsStayConstantAcrossRows(t *testing.T) {
	sdl := referenceRegressionSDL + `
type Evidence @node(paths:["evidence/*.md"]){name:String}
type Citation @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"citations",key:{field:"id"},title:["id"],space:"test",logicalKind:"citation") { id:ID @field(path:"id") evidence:[Evidence!] @field(path:"evidence") @references(to:["Evidence"],mode:STRICT,values:ELEMENTS) }`
	reads := func(rows int, query string) int64 {
		files := map[string]string{}
		var yaml strings.Builder
		yaml.WriteString("rows:\n")
		for i := range rows {
			fmt.Fprintf(&yaml, "  - {id: R%d, refs: [R%d, R0]}\n", i, (i+1)%rows)
		}
		yaml.WriteString("citations:\n")
		for i := range rows {
			fmt.Fprintf(&yaml, "  - {id: C%d, evidence: [evidence/e%d.md]}\n", i, i)
			files[fmt.Sprintf("evidence/e%d.md", i)] = fmt.Sprintf("---\nname: Evidence %d\n---\n# E%d\n", i, i)
		}
		files["model.yaml"] = yaml.String()
		env := newConfiguredQueryTestEnv(t, sdl, files, []string{"*.yaml", "evidence/*.md"})
		q, errs := Prepare(env.execSchema, query)
		require.Empty(t, errs)
		store := &countingReadStore{Store: env.store}
		deps := env.deps(nil)
		deps.Store = store
		result := Execute(t.Context(), deps, env.schema, q)
		require.Empty(t, result.Errors)
		for _, list := range result.Data {
			require.Len(t, list, rows)
			for i, raw := range list.([]any) {
				for _, name := range []string{"refs", "evidence"} {
					if targets, ok := raw.(map[string]any)[name]; ok {
						require.NotEmpty(t, targets, "row %d %s", i, name)
					}
				}
			}
		}
		return store.reads.Load()
	}
	for _, query := range []string{
		`{row(first:100){id}}`,
		`{row(first:100){id refs {id}}}`,
		`{row(first:100){id content refs {id}}}`,
		`{row(first:100){id refs(first: 1) {id content}}}`,
		`{row(first:100){id refs {id refs {id}}}}`,
		`{citation(first:100){id evidence {name}}}`,
	} {
		require.Equal(t, reads(3, query), reads(30, query), query)
	}
}
