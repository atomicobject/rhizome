package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/app/queryengine/runtimeproviders"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type ontologyQueryRuntime struct {
	live       *bootstrap.LiveRuntime
	runtime    *ontology.Runtime
	service    *ontology.Service
	noteReader obsidian.NoteReader
	schema     *ontoschema.Schema
	execSchema *ontologyquery.ExecutableSchema
	// noteFormatRuntime is a focused runtime-test seam. Production always uses
	// the immutable format runtime owned by LiveRuntime's metadata indexer.
	noteFormatRuntime *noteformat.Runtime
}

func loadOntologyExecutableSchema(vaultPath string) (*ontoschema.Schema, *ontologyquery.ExecutableSchema, error) {
	schema, err := ontoschema.LoadSchema(vaultPath)
	if err != nil {
		return nil, nil, err
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	if err != nil {
		return nil, nil, err
	}
	return schema, execSchema, nil
}

func readOntologyQueryInput(rawQuery, queryFile string) (string, error) {
	hasQuery := strings.TrimSpace(rawQuery) != ""
	hasFile := strings.TrimSpace(queryFile) != ""
	switch {
	case hasQuery == hasFile:
		return "", fmt.Errorf("exactly one of --query or --file is required")
	case hasFile:
		data, err := os.ReadFile(queryFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return rawQuery, nil
	}
}

func readOntologyVariablesInput(rawJSON, variablesFile string) (map[string]any, error) {
	hasJSON := strings.TrimSpace(rawJSON) != ""
	hasFile := strings.TrimSpace(variablesFile) != ""
	switch {
	case hasJSON && hasFile:
		return nil, fmt.Errorf("only one of --variables-json or --variables-file may be provided")
	case hasFile:
		data, err := os.ReadFile(variablesFile)
		if err != nil {
			return nil, err
		}
		return parseOntologyVariablesJSON(data)
	case hasJSON:
		return parseOntologyVariablesJSON([]byte(rawJSON))
	default:
		return nil, nil
	}
}

func parseOntologyVariablesJSON(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var variables map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&variables); err != nil {
		return nil, fmt.Errorf("variables must be a JSON object: %w", err)
	}
	if variables == nil {
		return nil, fmt.Errorf("variables must be a JSON object")
	}
	return variables, nil
}

func (commandOptions *vaultCommandOptions) prepareOntologyQuery(cmdCtx context.Context, rawQuery string, variables map[string]any) (*ontologyQueryRuntime, *ontologyquery.PreparedQuery, func(), error) {
	vaultDef, err := commandOptions.vaultDefOrDefaultContext(cmdCtx)
	if err != nil {
		return nil, nil, nil, err
	}

	schema, execSchema, err := loadOntologyExecutableSchema(vaultDef.BasePath())
	if err != nil {
		return nil, nil, nil, err
	}
	prepared, errs := ontologyquery.PrepareWithVariables(execSchema, rawQuery, variables)
	if len(errs) > 0 {
		return &ontologyQueryRuntime{schema: schema, execSchema: execSchema}, nil, nil, ontologyQueryErrors(errs)
	}

	runtime, cleanup, err := commandOptions.openOntologyQueryRuntime(cmdCtx, schema, execSchema, prepared)
	return runtime, prepared, cleanup, err
}

func (commandOptions *vaultCommandOptions) openOntologyQueryRuntime(cmdCtx context.Context, schema *ontoschema.Schema, execSchema *ontologyquery.ExecutableSchema, prepared *ontologyquery.PreparedQuery) (*ontologyQueryRuntime, func(), error) {
	live, err := commandOptions.buildOntologyQueryLiveRuntime(cmdCtx, oneshotruntime.OntologyQueryPlan(prepared.UsesSemantic, prepared.UsesSearch))
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = live.Close() }

	noteReader := &obsidian.Note{}
	runtime, err := ontology.PublishedRuntimeWithStore(cmdCtx, live.NoteMetadataIndexer(), live.VaultDef, live.IntelStore())
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		live.RequestOntologyRefresh()
		cleanup()
		return nil, nil, fmt.Errorf("ontology index is not ready; retry after indexing completes")
	}

	if runtime.Schema.Hash != schema.Hash {
		cleanup()
		return nil, nil, fmt.Errorf("ontology schema changed during query preparation")
	}
	return &ontologyQueryRuntime{
		live:       live,
		runtime:    runtime,
		service:    ontology.NewService(live.IntelStore(), runtime.Schema),
		noteReader: noteReader,
		schema:     schema,
		execSchema: execSchema,
	}, cleanup, nil
}

// buildOntologyQueryLiveRuntime applies the request-derived plan after query
// preparation. Current projection intentionally remains a live, writer-owned
// operation; its one-shot options only remove background work and capabilities
// that cannot affect the compiled query result.
func (commandOptions *vaultCommandOptions) buildOntologyQueryLiveRuntime(ctx context.Context, plan oneshotruntime.Plan) (*bootstrap.LiveRuntime, error) {
	runtime, err := oneshotruntime.Build(ctx, plan, func(buildCtx context.Context, requirements bootstrap.RuntimeRequirements) (oneshotruntime.Runtime, error) {
		live, err := bootstrap.NewLiveRuntime(buildCtx, commandOptions.ontologyQueryLiveOptions(requirements))
		if err != nil {
			return nil, err
		}
		return &liveRuntimePlanAdapter{LiveRuntime: live}, nil
	})
	if err != nil {
		return nil, err
	}
	if err := awaitOntologyQueryRuntime(ctx, runtime, plan); err != nil {
		runtime.Close()
		return nil, err
	}
	adapter, ok := runtime.(*liveRuntimePlanAdapter)
	if !ok || adapter.LiveRuntime == nil {
		if runtime != nil {
			runtime.Close()
		}
		return nil, fmt.Errorf("ontology one-shot runtime factory returned an incompatible runtime")
	}
	if plan.OntologyState == oneshotruntime.OntologyStateCurrentProjection {
		if err := adapter.LiveRuntime.PublishCurrentProjection(ctx); err != nil {
			runtime.Close()
			return nil, err
		}
	}
	return adapter.LiveRuntime, nil
}

// awaitOntologyQueryRuntime keeps the baseline error identity at this
// machine-facing boundary. The generic one-shot await helper adds phase
// context, which would change existing CLI JSON error text.
func awaitOntologyQueryRuntime(ctx context.Context, runtime oneshotruntime.Runtime, plan oneshotruntime.Plan) error {
	for _, readiness := range plan.Readiness {
		var err error
		switch readiness {
		case oneshotruntime.ReadinessCodeIndex:
			err = runtime.WaitForCodeIndex(ctx)
		case oneshotruntime.ReadinessSemantic:
			err = runtime.WaitForSemantic(ctx)
		default:
			return fmt.Errorf("ontology query has unsupported readiness %q", readiness)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (commandOptions *vaultCommandOptions) ontologyQueryLiveOptions(requirements bootstrap.RuntimeRequirements) bootstrap.LiveOptions {
	return bootstrap.LiveOptions{
		VaultName:           commandOptions.vaultName,
		Debug:               commandOptions.debug,
		SkipCacheWarmup:     true,
		DisableWatchHub:     true,
		DisableLeaderWork:   true,
		DisableSessionStore: true,
		// Current projection is a live, writer-owned operation. Preserve the
		// baseline metadata initialization/reset behavior for semantic queries.
		QueryProvidersOnly: false,
		Requirements:       requirements,
	}
}

func (rt *ontologyQueryRuntime) deps() (ontologyquery.Deps, error) {
	base := ontologyquery.Deps{VaultDef: rt.live.VaultDef, NoteReader: rt.noteReader, Store: rt.runtime.Store}
	if rt.noteFormatRuntime != nil {
		base.NoteFormats = *rt.noteFormatRuntime
	} else if formats, err := rt.live.NoteMetadataIndexer().FormatRuntime(); err == nil {
		base.NoteFormats = formats
	}
	_, codeProvider := rt.live.CodeEmbeddings()
	resolved := rt.live.Configuration()
	deps, err := queryengine.Dependencies(base, unifiedsearch.Options{Config: &resolved, VaultPath: rt.live.VaultDef.BasePath(), VaultDef: rt.live.VaultDef, IntelStore: rt.live.IntelStore(), NoteProvider: rt.live.NoteProvider(), CodeProvider: codeProvider})
	if err != nil {
		return ontologyquery.Deps{}, err
	}
	providers := runtimeproviders.New(rt.schema, rt.execSchema, deps, rt.live.CodeAnchorService())
	deps.OntologyRuntime, deps.CodeRuntime = providers, providers
	return deps, nil
}

func (rt *ontologyQueryRuntime) run(ctx context.Context, prepared *ontologyquery.PreparedQuery) (ontologyquery.Result, error) {
	deps, err := rt.deps()
	if err != nil {
		return ontologyquery.Result{}, err
	}
	engine, err := queryengine.New(rt.schema, rt.execSchema, deps)
	if err != nil {
		return ontologyquery.Result{}, err
	}
	return engine.Run(ctx, prepared)
}

func ontologyQueryErrors(errs []ontologyquery.Error) error {
	if len(errs) == 0 {
		return nil
	}
	data, err := json.Marshal(map[string]any{"errors": errs})
	if err != nil {
		return fmt.Errorf("ontology query failed")
	}
	return fmt.Errorf("%s", string(data))
}
