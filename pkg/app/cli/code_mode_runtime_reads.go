package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
)

// CodeModeRuntimeReadPending asks the code-mode host to serve one read in its
// own process, whose freshness refresh is authoritative (SPEC-0115). The host
// consumes it; generated clients never see it.
const CodeModeRuntimeReadPending = "runtime_read_pending"

// Budgets bound how long a read waits for the runtime before the host serves
// it in-process. Applying a changed note takes the watcher about a second and
// may queue behind another batch; the host's own refresh after a change
// reprojects every note, so waiting is almost always cheaper.
const (
	codeModeIndexBudget = 2 * time.Second
	codeModeSyncBudget  = 10 * time.Second
)

var errCodeModeRuntimeReadPending = fmt.Errorf(`{"code":%q,"message":"the vault runtime cannot show its projection is current"}`, CodeModeRuntimeReadPending)

// CodeModeReadRuntime is the vault runtime as the runtime-read workflow needs it.
type CodeModeReadRuntime interface {
	VaultPath() string
	WaitForCodeIndex(context.Context) error
	WaitForSemantic(context.Context) error
	// SyncSources returns once every selected note change completed before
	// the call is applied to the persisted metadata rows.
	SyncSources(context.Context) error
	ProjectionStates(context.Context) (semdb.OntologySchemaState, semdb.NoteMetadataState, error)
	QueryDeps(*ontology.Schema, *ontologyquery.ExecutableSchema) ontologyquery.Deps
	Views(*ontology.Schema, *ontologyquery.ExecutableSchema) (CodeModeViewRuntime, error)
}

// CodeModeRuntimeReads serves catalog-admitted code-mode reads inside the vault
// runtime through the same workflows the host runs, over the runtime's open
// index and a schema cached by source hash.
type CodeModeRuntimeReads struct {
	Runtime CodeModeReadRuntime

	mu   sync.Mutex
	defs codeModeDefinitions
}

type codeModeDefinitions struct {
	schema *ontology.Schema
	exec   *ontologyquery.ExecutableSchema
}

// IsCodeModeRuntimeReadPending reports the runtime's request to serve a read
// in the host process instead.
func IsCodeModeRuntimeReadPending(diagnostic any) bool {
	fields, _ := diagnostic.(map[string]any)
	return fields["code"] == CodeModeRuntimeReadPending
}

func (r *CodeModeRuntimeReads) Call(ctx context.Context, name string, input map[string]any) CodeModeOutcome {
	readyCtx, cancel := context.WithTimeout(ctx, codeModeIndexBudget)
	err := r.Runtime.WaitForCodeIndex(readyCtx)
	cancel()
	if err != nil {
		return codeModeFailure(errCodeModeRuntimeReadPending, 1)
	}
	switch name {
	case "ontology_query":
		return CodeModeOntologyQueryService{Runtime: codeModeRuntimeQuery{r}}.Call(ctx, input)
	case "query_recipe":
		recipes := QueryRecipeService{
			Load: func(string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
				recipes, issues := queryrecipe.LoadDefaultSources(r.Runtime.VaultPath())
				return recipes, issues, nil
			},
			Schema: func() (*ontologyquery.ExecutableSchema, error) {
				defs, err := r.definitions()
				return defs.exec, err
			},
			Execute: codeModeRuntimeQuery{r}.Execute,
		}
		return CodeModeQueryRecipeService{Recipes: recipes}.Call(ctx, input)
	case "view":
		return CodeModeViewService{Factory: codeModeRuntimeViews{r}}.Call(ctx, input)
	default:
		return codeModeFailure(fmt.Errorf("code operation %q has no runtime read", name), 1)
	}
}

// definitions caches the parsed schema by source hash, as the web server does,
// so a read pays for parsing only after the ontology changes.
func (r *CodeModeRuntimeReads) definitions() (codeModeDefinitions, error) {
	hash, err := ontology.SchemaSourceHash(r.Runtime.VaultPath())
	if err != nil {
		return codeModeDefinitions{}, err
	}
	r.mu.Lock()
	cached := r.defs
	r.mu.Unlock()
	if cached.schema != nil && cached.schema.Hash == hash {
		return cached, nil
	}
	schema, err := ontology.LoadSchema(r.Runtime.VaultPath())
	if err != nil {
		return codeModeDefinitions{}, err
	}
	exec, err := ontologyquery.BuildExecutableSchema(schema)
	if err != nil {
		return codeModeDefinitions{}, err
	}
	defs := codeModeDefinitions{schema: schema, exec: exec}
	r.mu.Lock()
	r.defs = defs
	r.mu.Unlock()
	return defs, nil
}

// projectionCurrent keeps the host's current-projection contract: every note
// change is applied, and the ontology state matches the current schema and the
// metadata it was built from.
func (r *CodeModeRuntimeReads) projectionCurrent(ctx context.Context, schema *ontology.Schema) bool {
	syncCtx, cancel := context.WithTimeout(ctx, codeModeSyncBudget)
	defer cancel()
	if err := r.Runtime.SyncSources(syncCtx); err != nil {
		return false
	}
	state, notes, err := r.Runtime.ProjectionStates(ctx)
	return err == nil && state.Ready && state.SchemaHash == schema.Hash &&
		state.MaterializationVersion == ontology.OntologyMaterializationVersion &&
		notes.Ready && notes.NotesHash == state.NotesHash
}

type codeModeRuntimeQuery struct{ r *CodeModeRuntimeReads }

func (q codeModeRuntimeQuery) Execute(ctx context.Context, rawQuery string, variables map[string]any) (ontologyquery.Result, error) {
	defs, err := q.r.definitions()
	if err != nil {
		return ontologyquery.Result{}, err
	}
	prepared, errs := ontologyquery.PrepareWithVariables(defs.exec, rawQuery, variables)
	if len(errs) > 0 {
		return ontologyquery.Result{}, codeModeQueryErrors(errs)
	}
	// The host's providers decide semantic availability, as they did before.
	if prepared.UsesSemantic && q.r.Runtime.WaitForSemantic(ctx) != nil {
		return ontologyquery.Result{}, errCodeModeRuntimeReadPending
	}
	if !q.r.projectionCurrent(ctx, defs.schema) {
		return ontologyquery.Result{}, errCodeModeRuntimeReadPending
	}
	return ontologyquery.ExecutePrepared(ctx, q.r.Runtime.QueryDeps(defs.schema, defs.exec), defs.schema, defs.exec, prepared), nil
}

type codeModeRuntimeViews struct{ r *CodeModeRuntimeReads }

// Open serves views over the persisted projection, the same state the host's
// one-shot view service reads. As there, an unavailable schema leaves
// schema-free views.
func (v codeModeRuntimeViews) Open(context.Context, string) (CodeModeViewRuntime, func(), error) {
	defs, _ := v.r.definitions()
	views, err := v.r.Runtime.Views(defs.schema, defs.exec)
	return views, nil, err
}

// codeModeQueryErrors matches the host's error text for invalid queries.
func codeModeQueryErrors(errs []ontologyquery.Error) error {
	data, err := json.Marshal(map[string]any{"errors": errs})
	if err != nil {
		return fmt.Errorf("ontology query failed")
	}
	return fmt.Errorf("%s", data)
}
