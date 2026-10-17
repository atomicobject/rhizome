package planner

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPlannerOntologyLaneRequiresCurrentProviderPublication(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "index.db"))
	require.NoError(t, err)
	defer store.Close()
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	metadata := semdb.NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw", LoadedAt: 1, Ready: true}
	state := semdb.OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", MaterializationVersion: ontology.OntologyMaterializationVersion, LoadedAt: 1, Ready: true}
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(t.Context(), semdb.NoteMetadataSnapshot{State: metadata}))
	require.NoError(t, store.ApplyOntologyDelta(t.Context(), semdb.OntologyDelta{SchemaState: &state, Readiness: &semdb.OntologyReadinessPublication{SchemaState: state, MetadataState: metadata, ProviderHash: ontology.OntologyProviderHash(formats), Validate: func(semdb.OntologyReadinessSnapshot) (bool, error) { return true, nil }}}))
	p := Planner{Deps: Deps{IntelStore: store, NoteFormats: formats, VaultDef: obsidian.VaultDefinition{Path: root}, NoteReader: &obsidian.Note{}}, Options: Options{EnableGraph: true, MaxPerOwner: 3}}
	spec := search.QuerySpec{Seeds: []knowledge.Handle{knowledge.NoteHandle("seed.md")}, Intent: search.IntentRelatedToSeed, Limits: search.Limits{Total: 10}}
	plan, err := p.Plan(t.Context(), spec)
	require.NoError(t, err)
	require.True(t, planHasRetrieverNamed(plan, "ontology"))
	// Descriptor identity alone cannot reuse a proof from executable providers.
	descriptorOnly, err := noteformat.NewRuntime(formats.Registry())
	require.NoError(t, err)
	p.Deps.NoteFormats = descriptorOnly
	plan, err = p.Plan(t.Context(), spec)
	require.NoError(t, err)
	require.False(t, planHasRetrieverNamed(plan, "ontology"))
	p.Deps.NoteFormats = formats
	// A proof can be invalidated while legacy ready/hash/version fields stay put.
	_, err = store.DB().ExecContext(t.Context(), "DELETE FROM ontology_readiness")
	require.NoError(t, err)
	legacy, err := store.GetOntologySchemaState(t.Context())
	require.NoError(t, err)
	require.Equal(t, state, legacy)
	currentMetadata, err := store.GetNoteMetadataState(t.Context())
	require.NoError(t, err)
	require.Equal(t, metadata, currentMetadata)
	plan, err = p.Plan(t.Context(), spec)
	require.NoError(t, err)
	require.False(t, planHasRetrieverNamed(plan, "ontology"))
}

// publishPlannerOntology gives synthetic planner fixtures the same publication
// witness required by production readers. The fixtures own their schema rows.
func publishPlannerOntology(t *testing.T, store *semdb.Store) noteformat.Runtime {
	t.Helper()
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	state, err := store.GetOntologySchemaState(t.Context())
	require.NoError(t, err)
	metadata, err := store.GetNoteMetadataState(t.Context())
	require.NoError(t, err)
	require.NoError(t, store.ApplyOntologyDelta(t.Context(), semdb.OntologyDelta{Readiness: &semdb.OntologyReadinessPublication{
		SchemaState: state, MetadataState: metadata,
		ProviderHash: ontology.OntologyProviderHash(formats),
		Validate:     func(semdb.OntologyReadinessSnapshot) (bool, error) { return true, nil },
	}}))
	return formats
}
