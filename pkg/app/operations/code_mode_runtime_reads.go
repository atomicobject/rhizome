package operations

import (
	"context"
	"encoding/json"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	semdb "github.com/atomicobject/rhizome/pkg/store"
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
	// ProjectionReadiness checks the persisted proof for this runtime's selected providers.
	ProjectionReadiness(context.Context) (semdb.OntologyReadinessState, error)
	QueryDeps(*ontoschema.Schema, *ontologyquery.ExecutableSchema) (ontologyquery.Deps, error)
	Views(*ontoschema.Schema, *ontologyquery.ExecutableSchema) (CodeModeViewRuntime, error)
}

// CodeModeRuntimeReads serves catalog-admitted code-mode reads inside the vault
// runtime through the same workflows the host runs, over the runtime's open
// index and a schema cached by source hash.
type CodeModeRuntimeReads struct {
	Runtime CodeModeReadRuntime

	mu      sync.Mutex
	defs    codeModeDefinitions
	recipes *queryrecipe.Registry
}

type codeModeDefinitions struct {
	schema *ontoschema.Schema
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
		r.mu.Lock()
		if r.recipes == nil {
			r.recipes = queryrecipe.NewRegistry(queryrecipe.DefaultSourceRoots(r.Runtime.VaultPath()))
		}
		registry := r.recipes
		r.mu.Unlock()
		var selectedDefs codeModeDefinitions
		recipes := QueryRecipeService{
			Load: func(string) ([]queryrecipe.Recipe, []queryrecipe.Issue, error) {
				recipes, issues := registry.Catalog()
				return recipes, issues, nil
			},
			Schema: func() (*ontologyquery.ExecutableSchema, error) {
				defs, err := r.definitions()
				selectedDefs = defs
				return defs.exec, err
			},
			Execute: func(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
				return (codeModeRuntimeQuery{r}).executePrepared(ctx, selectedDefs, prepared)
			},
		}
		return CodeModeQueryRecipeService{Recipes: recipes}.Call(ctx, input)
	case "view":
		return CodeModeViewService{Factory: codeModeRuntimeViews{r}}.Call(ctx, input)
	default:
		return codeModeFailure(fmt.Errorf("code operation %q has no runtime read", name), 1)
	}
}

// definitions caches the executable form of the shared immutable schema.
func (r *CodeModeRuntimeReads) definitions() (codeModeDefinitions, error) {
	schema, err := ontoschema.LoadSchema(r.Runtime.VaultPath())
	if err != nil {
		return codeModeDefinitions{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defs.schema == schema {
		return r.defs, nil
	}
	exec, err := ontologyquery.BuildExecutableSchema(schema)
	if err != nil {
		return codeModeDefinitions{}, err
	}
	r.defs = codeModeDefinitions{schema: schema, exec: exec}
	return r.defs, nil
}

// projectionCurrent keeps the host's current-projection contract: every note
// change is applied, and the ontology state matches the current schema and the
// metadata it was built from.
func (r *CodeModeRuntimeReads) projectionCurrent(ctx context.Context, schema *ontoschema.Schema) bool {
	syncCtx, cancel := context.WithTimeout(ctx, codeModeSyncBudget)
	defer cancel()
	if err := r.Runtime.SyncSources(syncCtx); err != nil {
		return false
	}
	state, err := r.Runtime.ProjectionReadiness(ctx)
	return err == nil && state.Ready && state.SchemaState.SchemaHash == schema.Hash &&
		state.SchemaState.MaterializationVersion == ontology.OntologyMaterializationVersion
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
	return q.executePrepared(ctx, defs, prepared)
}

func (q codeModeRuntimeQuery) executePrepared(ctx context.Context, defs codeModeDefinitions, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
	// The host's providers decide semantic availability, as they did before.
	if prepared.UsesSemantic && q.r.Runtime.WaitForSemantic(ctx) != nil {
		return ontologyquery.Result{}, errCodeModeRuntimeReadPending
	}
	if !q.r.projectionCurrent(ctx, defs.schema) {
		return ontologyquery.Result{}, errCodeModeRuntimeReadPending
	}
	deps, err := q.r.Runtime.QueryDeps(defs.schema, defs.exec)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	engine, err := queryengine.New(defs.schema, defs.exec, deps)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	return engine.Run(ctx, prepared)
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
