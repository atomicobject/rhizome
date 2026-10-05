package cmd

import (
	"context"
	"errors"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
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

func (l liveCodeModeReads) ProjectionStates(ctx context.Context) (semdb.OntologySchemaState, semdb.NoteMetadataState, error) {
	store := l.rt.IntelStore()
	if store == nil {
		return semdb.OntologySchemaState{}, semdb.NoteMetadataState{}, errors.New("index store is not open")
	}
	state, err := store.GetOntologySchemaState(ctx)
	if err != nil {
		return state, semdb.NoteMetadataState{}, err
	}
	notes, err := store.GetNoteMetadataState(ctx)
	return state, notes, err
}

// QueryDeps composes the same query dependencies the host's one-shot runtime
// builds, over the runtime's open store.
func (l liveCodeModeReads) QueryDeps(schema *ontology.Schema, exec *ontologyquery.ExecutableSchema) ontologyquery.Deps {
	store := l.rt.IntelStore()
	noteReader := &obsidian.Note{}
	runtime := &ontologyQueryRuntime{
		live:       l.rt,
		runtime:    &ontology.Runtime{Schema: schema, Store: store, Ready: true},
		service:    ontology.NewService(l.rt.VaultDef, noteReader, store, schema),
		noteReader: noteReader,
		schema:     schema,
		execSchema: exec,
	}
	return runtime.deps()
}

func (l liveCodeModeReads) Views(schema *ontology.Schema, exec *ontologyquery.ExecutableSchema) (actions.CodeModeViewRuntime, error) {
	formats, err := l.rt.NoteMetadataIndexer().FormatRuntime()
	if err != nil {
		return nil, err
	}
	return newViewService(l.rt.VaultDef, l.rt.IntelStore(), schema, exec, formats, nil, nil), nil
}
