package cmd

import (
	"context"
	"errors"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// liveCodeModeReads adapts the vault runtime to the code-mode runtime-read
// workflow (SPEC-0115).
type liveCodeModeReads struct{ rt *bootstrap.LiveRuntime }

func (l liveCodeModeReads) VaultPath() string { return l.rt.VaultPath }
func (l liveCodeModeReads) WaitForCodeIndex(ctx context.Context) error {
	return l.rt.WaitForCodeIndex(ctx)
}
func (l liveCodeModeReads) WaitForSemantic(ctx context.Context) error {
	return l.rt.WaitForSemantic(ctx)
}
func (l liveCodeModeReads) SyncSources(ctx context.Context) error { return l.rt.SyncWatcher(ctx) }

func (l liveCodeModeReads) ProjectionReadiness(ctx context.Context) (semdb.OntologyReadinessState, error) {
	store := l.rt.IntelStore()
	if store == nil {
		return semdb.OntologyReadinessState{}, errors.New("index store is not open")
	}
	formats, err := l.rt.NoteMetadataIndexer().FormatRuntime()
	if err != nil {
		return semdb.OntologyReadinessState{}, err
	}
	return store.GetOntologyReadiness(ctx, ontology.OntologyProviderHash(formats))
}

// QueryDeps composes the same query dependencies the host's one-shot runtime
// builds, over the runtime's open store.
func (l liveCodeModeReads) QueryDeps(schema *ontoschema.Schema, exec *ontologyquery.ExecutableSchema) (ontologyquery.Deps, error) {
	store := l.rt.IntelStore()
	noteReader := &obsidian.Note{}
	runtime := &ontologyQueryRuntime{
		live:       l.rt,
		runtime:    &ontology.Runtime{Schema: schema, Store: store, Ready: true},
		service:    ontology.NewService(store, schema),
		noteReader: noteReader,
		schema:     schema,
		execSchema: exec,
	}
	return runtime.deps()
}

func (l liveCodeModeReads) Views(schema *ontoschema.Schema, exec *ontologyquery.ExecutableSchema) (actions.CodeModeViewRuntime, error) {
	formats, err := l.rt.NoteMetadataIndexer().FormatRuntime()
	if err != nil {
		return nil, err
	}
	return newViewService(l.rt.VaultDef, l.rt.IntelStore(), schema, exec, formats, nil, nil)
}
