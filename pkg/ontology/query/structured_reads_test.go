package query

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit/recordfixture"
	"github.com/atomicobject/rhizome/internal/testkit/structuralfixture"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

type countedRecordProjector struct {
	formats    noteformat.Runtime
	descriptor noteformat.Descriptor
	calls      *sync.Map
}

func (p countedRecordProjector) Descriptor() noteformat.Descriptor { return p.descriptor }
func (p countedRecordProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	v, _ := p.calls.LoadOrStore(source.Path().String(), new(atomic.Int64))
	count := v.(*atomic.Int64)
	count.Add(1)
	return p.formats.Project(source)
}

func TestMixedFormatRecordsRoundTripThroughSharedReads(t *testing.T) {
	files := recordfixture.Files()
	files[".rhizome/source-ownership.yml"] = "sources:\n  - owner: Fixture author\n    reason: Change this with the owning tool.\n    paths: [model/**, generated/**, account.yaml, product.yaml]\n"
	env := newConfiguredQueryTestEnv(t, recordfixture.SDL, files, recordfixture.Includes)
	deps := env.deps(nil)
	reader := &structuralfixture.Reader{Paths: []string{"model/services.yaml", "model/groups.yml", "generated/model.json", "account.yaml", "product.yaml", "evidence.md"}}
	deps.NoteReader = reader
	calls := &sync.Map{}
	var projectors []noteformat.Projector
	for _, id := range env.noteFormats.Registry().IDs() {
		p, _ := env.noteFormats.Provider(id)
		projectors = append(projectors, countedRecordProjector{env.noteFormats, p.Descriptor(), calls})
	}
	formats, err := noteformat.NewRuntime(env.noteFormats.Registry(), projectors...)
	require.NoError(t, err)
	deps.NoteFormats = formats
	q, errs := Prepare(env.execSchema, `{ service(sort:[{field:"id"}]) {id name empty nothing absent refs{id} checks tolerant reads{id}} grouped {id group} transition {from to} accountEntry {id title} productEntry {id title} }`)
	require.Empty(t, errs)
	result := Execute(t.Context(), deps, env.schema, q)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{
		map[string]any{"id": "A", "name": "Shared title", "empty": "", "nothing": nil, "absent": nil, "refs": []any{map[string]any{"id": "B"}, map[string]any{"id": "B"}}, "checks": []any{"B"}, "tolerant": []any{"unknown", "*"}, "reads": []any{map[string]any{"id": "B"}}},
		map[string]any{"id": "B", "name": "Shared title", "empty": nil, "nothing": nil, "absent": nil, "refs": []any{}, "checks": []any{}, "tolerant": []any{}, "reads": []any{}},
	}, result.Data["service"])
	require.Equal(t, []any{map[string]any{"id": "account", "title": "1"}}, result.Data["accountEntry"])
	require.Equal(t, []any{map[string]any{"id": "product", "title": "2"}}, result.Data["productEntry"])
	require.Equal(t, []any{map[string]any{"from": "A", "to": "B"}}, result.Data["transition"])
	require.Len(t, result.Data["grouped"], 3)
	require.Zero(t, reader.Reads.Load(), "summary fields stay index-only")
	calls.Range(func(_, v any) bool { require.Zero(t, v.(*atomic.Int64).Load()); return true })
	q, errs = Prepare(env.execSchema, `{service(filters:[{field:"id",op:eq,value:"A"}]){id content jobs{id} workspace{source sourceRevision{content} capabilities{canEdit canEditFields canNavigateChildren readOnlyReason} bodies{source markdown level blockId} fields{name present values}}}}`)
	require.Empty(t, errs)
	result = Execute(t.Context(), deps, env.schema, q)
	require.Empty(t, result.Errors)
	rows := result.Data["service"].([]any)
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	require.Contains(t, row["content"], `"a/b~":"untouched alpha evidence"`)
	require.Equal(t, []any{map[string]any{"id": "plan"}, map[string]any{"id": "ship"}}, row["jobs"])
	workspace := row["workspace"].(map[string]any)
	source := workspace["source"].(ontology.NodeSourceContent)
	require.Equal(t, "/services/0", source.Address)
	require.Equal(t, "yaml", string(source.Format))
	require.Equal(t, "source_value", source.Representation)
	require.Equal(t, recordfixture.Services, workspace["sourceRevision"].(map[string]any)["content"])
	require.Contains(t, workspace["capabilities"].(map[string]any)["readOnlyReason"], "Change this with the owning tool.")
	for _, raw := range workspace["bodies"].([]any) {
		body := raw.(map[string]any)
		require.Empty(t, body["markdown"])
		require.Nil(t, body["level"])
		require.Nil(t, body["blockId"])
	}
	require.EqualValues(t, 1, reader.Reads.Load())
	v, ok := calls.Load("model/services.yaml")
	require.True(t, ok)
	require.EqualValues(t, 1, v.(*atomic.Int64).Load(), "one real parser invocation for rich content/children/workspace")
	_, errs = Prepare(env.execSchema, `{service{level}}`)
	require.NotEmpty(t, errs)
	_, errs = Prepare(env.execSchema, `{service{workspace{source}}}`)
	require.Empty(t, errs)
	require.NotContains(t, strings.Join(reader.Paths, " "), "unselected")
}

func TestRecordReferencesResolveExactMarkdownTargets(t *testing.T) {
	sdl := referenceRegressionSDL + `
type Evidence @node(paths:["evidence.md"]){name:String}
type Citation @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"citations",key:{field:"id"},title:["id"],space:"test",logicalKind:"citation") { evidence:[Evidence!] @field(path:"evidence") @references(to:["Evidence"],mode:STRICT,values:ELEMENTS,edge:{name:"evidence"}) }`
	env := newConfiguredQueryTestEnv(t, sdl, map[string]string{"model.yaml": "citations: [{id: C, evidence: [evidence.md]}]\n", "evidence.md": "---\nname: Recorded evidence\n---\n# Evidence\n"}, []string{"*.md", "*.yaml"})
	q, errs := Prepare(env.execSchema, `{citation{evidence{name}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{map[string]any{"evidence": []any{map[string]any{"name": "Recorded evidence"}}}}, result.Data["citation"])
}

func TestMarkdownEvidenceLinksKeepSameTitleRecordsDistinct(t *testing.T) {
	sdl := referenceRegressionSDL + `type Evidence @node(paths:["evidence.md"]){name:String}`
	files := map[string]string{"model.yaml": "rows: [{id: A}, {id: B}]\n", "evidence.md": "# Evidence\n"}
	initial := newConfiguredQueryTestEnv(t, sdl, files, []string{"*.md", "*.yaml"})
	rows, err := initial.store.OntologyNodesByPaths(t.Context(), []string{"model.yaml"})
	require.NoError(t, err)
	var refs []string
	for _, row := range rows {
		if row.TypeName == "Row" {
			refs = append(refs, row.SourceLocator)
		}
	}
	require.Len(t, refs, 2)
	files["evidence.md"] = "# Evidence\n\nSee [A](<" + refs[0] + ">) and [B](<" + refs[1] + ">).\n"
	env := newConfiguredQueryTestEnv(t, sdl, files, []string{"*.md", "*.yaml"})
	q, errs := Prepare(env.execSchema, `{evidence{workspace{sourceLinks{resolvedRef{ref}}}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	linked := result.Data["evidence"].([]any)[0].(map[string]any)["workspace"].(map[string]any)["sourceLinks"].([]any)
	var targets []string
	for _, raw := range linked {
		targets = append(targets, raw.(map[string]any)["resolvedRef"].(map[string]any)["ref"].(string))
	}
	require.ElementsMatch(t, refs, targets)
}
