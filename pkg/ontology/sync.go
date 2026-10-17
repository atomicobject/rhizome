package ontology

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"sort"
	"strings"
	"time"
)

// DeltaWriteQueue publishes assessments, graph facts, and catalog rows atomically
// through the caller's durable writer lane.
type DeltaWriteQueue interface {
	SubmitOntologyDelta(context.Context, semdb.OntologyDelta) error
}

// SyncResult summarizes incremental ontology convergence.
//
// Considered counts load candidates, Skipped counts unchanged candidates, and
// Assessed counts notes that actually rebuilt validation/type/edge state.
type SyncResult struct {
	Schema     *ontoschema.Schema
	NotesHash  string
	Dirty      bool
	Rebuilt    bool
	Issues     []ValidationIssue
	Considered int
	Skipped    int
	Assessed   int
	Relations  int
}

type syncPlan struct {
	LoadPaths    []string
	ReplacePaths []string
	DeletePaths  []string
	ForcePaths   map[string]struct{}
}

type noteSyncJob struct {
	projected   *projectedNote
	doc         *noteDoc
	assessment  *NoteAssessment
	fingerprint string
	meta        semdb.NoteMetadataRow
}

type noteSyncResult struct {
	projected   *projectedNote
	path        string
	assessment  *NoteAssessment
	row         *semdb.OntologyNoteAssessmentRow
	noteState   *semdb.OntologyNoteStateRow
	noteType    *semdb.OntologyNoteTypeRow
	edges       []semdb.OntologyEdgeRow
	issues      []ValidationIssue
	relationCnt int
}

// SyncPublishedPaths converges ontology from note metadata that the caller has
// already published through its durable writer barrier. An empty path set
// validates and repairs the published rows without rediscovering source metadata.
func SyncPublishedPaths(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, writeQueue DeltaWriteQueue, changedPaths, deletedPaths []string) (*SyncResult, error) {
	// Docs: [[ontology-indexed-read-model-contract#^spec-0040-full-rebuild]]
	// and [[ontology-indexed-read-model-contract#^spec-0040-incremental-convergence]]
	// define this function as the convergence boundary for indexed ontology rows.
	if store == nil {
		return &SyncResult{}, nil
	}
	if err := noteMetadata.Validate(); err != nil {
		return nil, err
	}
	formats, err := noteMetadata.FormatRuntime()
	if err != nil {
		return nil, err
	}
	state, err := store.GetOntologySchemaState(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateOntologyMaterializationVersion(state.MaterializationVersion); err != nil {
		return nil, err
	}
	if writeQueue == nil {
		return nil, fmt.Errorf("published ontology sync requires a durable write queue")
	}
	changedPaths = normalizeStringSet(changedPaths)
	deletedPaths = normalizeStringSet(deletedPaths)

	schema, schemaErr := ontoschema.LoadSchema(vaultDef.BasePath())
	if schemaErr != nil {
		if schemaErr == ontoschema.ErrNoOntologyFiles {
			if err := writeQueue.SubmitOntologyDelta(ctx, semdb.OntologyDelta{FullRebuild: true}); err != nil {
				return nil, err
			}
			return &SyncResult{}, nil
		}
		{
			issues := []ValidationIssue{{Code: "schema_invalid", Message: schemaErr.Error()}}
			noteState, err := store.GetNoteMetadataState(ctx)
			if err != nil {
				return nil, err
			}
			loadedAt := noteState.LoadedAt
			if loadedAt == 0 {
				loadedAt = time.Now().Unix()
			}
			state := semdb.OntologySchemaState{
				NotesHash:              noteState.NotesHash,
				MaterializationVersion: OntologyMaterializationVersion,
				LoadedAt:               loadedAt,
				Ready:                  false,
				ErrorJSON:              mustJSON(issues),
			}
			// Validation projection must publish invalid-schema evidence without
			// replacing the last usable ontology snapshot. The schema-state-only
			// delta stays on the coordinator's durable writer lane and deliberately
			// leaves assessments, catalog rows, chunks, embeddings, and sidecars
			// untouched.
			delta := semdb.OntologyDelta{SchemaState: &state}
			if err := writeQueue.SubmitOntologyDelta(ctx, delta); err != nil {
				return nil, err
			}
			return &SyncResult{
				NotesHash: noteState.NotesHash,
				Dirty:     true,
				Issues:    issues,
			}, nil
		}
	}
	noteState, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, err
	}
	if !noteState.Ready || noteState.LoadedAt == 0 {
		return nil, fmt.Errorf("published note metadata is not ready")
	}
	ontologyPaths, err := ProjectableMetadataPaths(ctx, noteMetadata, store)
	if err != nil {
		return nil, err
	}
	changedPaths = retainPaths(changedPaths, ontologyPaths)
	noExplicitPaths := len(changedPaths) == 0 && len(deletedPaths) == 0
	fullRebuild := !state.Ready || strings.TrimSpace(state.SchemaHash) == "" || state.SchemaHash != schema.Hash || state.MaterializationVersion != OntologyMaterializationVersion || (noExplicitPaths && state.NotesHash != noteState.NotesHash)
	if !fullRebuild && noExplicitPaths {
		proof, err := store.GetOntologyReadiness(ctx, OntologyProviderHash(formats))
		if err != nil {
			return nil, err
		}
		if proof.Ready {
			return &SyncResult{Schema: schema, NotesHash: noteState.NotesHash, Issues: decodeIssues(state.ErrorJSON)}, nil
		}
	}

	var plan *syncPlan
	if fullRebuild {
		// Schema changes can alter type resolution, embedded-node structure,
		// semantic primary chunks and graph endpoints for every typed note. Avoid
		// clever scoping here; the schema hash is the correctness barrier.
		allPaths := ontologyPaths
		plan = &syncPlan{
			LoadPaths:    allPaths,
			ReplacePaths: append([]string(nil), allPaths...),
			ForcePaths:   make(map[string]struct{}, len(allPaths)),
		}
		for _, path := range allPaths {
			plan.ForcePaths[path] = struct{}{}
		}
	} else {
		plan, err = planOntologySync(ctx, store, changedPaths, deletedPaths)
		if err != nil {
			return nil, err
		}
	}
	// A deleted or fatal source can still appear in relation-neighbor work from
	// the pre-transition graph. Only provider-current executable rows may be
	// loaded into the ontology projector; delete paths remain intact so their
	// stale read-model rows are still removed.
	plan.LoadPaths = retainPaths(plan.LoadPaths, ontologyPaths)

	allNotePaths := ontologyPaths
	var (
		docsByPath         map[string]*noteDoc
		metaByPath         map[string]semdb.NoteMetadataRow
		noteStates         map[string]semdb.OntologyNoteStateRow
		assessmentRows     map[string]semdb.OntologyNoteAssessmentRow
		decodedAssessments map[string]*NoteAssessment
		existingEdges      []semdb.OntologyEdgeRow
		incidentGraphRows  []semdb.GraphDocEdge
		policiesChanged    bool
	)
	if err := withOntologyPhase(ctx, "load_ontology_inputs", func(phaseCtx context.Context) error {
		var err error
		if fullRebuild {
			noteStates = map[string]semdb.OntologyNoteStateRow{}
		} else {
			snapshot, err := store.OntologyReadinessSnapshot(phaseCtx)
			if err != nil {
				return err
			}
			policiesChanged = !ontologyPoliciesMatch(schema, snapshot.TypePolicies)
			noteStates, assessmentRows = snapshot.NoteStates, snapshot.Assessments
			incompletePaths, decoded := incompleteOntologyPaths(allNotePaths, snapshot, schema.Hash)
			decodedAssessments = decoded
			promoteIncompleteOntologyPaths(plan, incompletePaths)
			orphanPaths := orphanedOntologyPaths(allNotePaths, snapshot)
			if len(orphanPaths) > 0 {
				cleanup, err := planOntologySync(phaseCtx, store, nil, orphanPaths)
				if err != nil {
					return err
				}
				plan.LoadPaths = normalizeStringSet(append(plan.LoadPaths, cleanup.LoadPaths...))
				plan.ReplacePaths = normalizeStringSet(append(plan.ReplacePaths, cleanup.ReplacePaths...))
				plan.DeletePaths = normalizeStringSet(append(plan.DeletePaths, cleanup.DeletePaths...))
				for path := range cleanup.ForcePaths {
					plan.ForcePaths[path] = struct{}{}
				}
			}
			plan.LoadPaths = retainPaths(plan.LoadPaths, allNotePaths)
		}
		docsByPath, metaByPath, err = loadNoteDocsFromStoreByPaths(phaseCtx, noteMetadata, vaultDef, noteMgr, store, plan.LoadPaths)
		if err != nil {
			return err
		}
		existingEdges, err = store.OntologyEdgesForPaths(phaseCtx, plan.LoadPaths, true, "", 0)
		if err != nil {
			return err
		}
		incidentGraphRows, err = store.GraphDocNoteEdgesForPaths(phaseCtx, plan.LoadPaths)
		return err
	}); err != nil {
		return nil, err
	}

	aliases, err := notemeta.LoadAliasMap(ctx, store)
	if err != nil {
		return nil, err
	}
	cache := obsidian.BuildNotePathCacheWithAliases(allNotePaths, aliases)
	graphByPath := groupIncidentGraphRows(incidentGraphRows)
	jobs, replacePaths, skipped, err := prepareOntologyJobs(plan, schema, docsByPath, metaByPath, noteStates, graphByPath, state.MaterializationVersion)
	if err != nil {
		return nil, err
	}
	recomputePaths := make([]string, 0, len(jobs))
	for _, job := range jobs {
		recomputePaths = append(recomputePaths, job.doc.Path)
	}
	indexingperf.AddCount(indexingperf.WithPhase(ctx, "compute_ontology"), indexingperf.OntologyNotesConsidered, int64(len(plan.LoadPaths)))
	indexingperf.AddCount(indexingperf.WithPhase(ctx, "compute_ontology"), indexingperf.OntologyNotesSkipped, int64(skipped))

	externalTypes, err := loadExternalOntologyTypes(ctx, store, schema, jobs, cache, replacePaths, incidentGraphRows)
	if err != nil {
		return nil, err
	}
	results, err := computeOntologyJobs(ctx, jobs, schema, cache, graphByPath, externalTypes)
	if err != nil {
		return nil, err
	}

	catalogReadModel, err := buildOntologyNodeCatalogReadModel(schema, results, ontologyLinkTargetResolver(cache, ontologyTypeResolverFromSyncResults(results, externalTypes)))
	if err != nil {
		return nil, err
	}
	catalog := catalogNodeWitnesses(catalogReadModel.Nodes)
	fieldCatalog := catalogFieldWitnesses(catalogReadModel.FieldValues)
	for _, result := range results {
		if result.assessment != nil {
			witness := catalogWitnessForPath(catalog, result.path)
			result.assessment.CatalogNodeCount = &witness.Count
			result.assessment.CatalogNodeDigest = witness.Digest
			fieldWitness := catalogWitnessForPath(fieldCatalog, result.path)
			result.assessment.CatalogFieldCount = &fieldWitness.Count
			result.assessment.CatalogFieldDigest = fieldWitness.Digest
			result.assessment.SourceContentHash = metaByPath[result.path].ContentHash
		}
	}
	setOntologyOutputWitnesses(results, catalogReadModel.LinkDependencies)
	catalogPaths := catalogReadModel.NotePaths
	delta, issues, relations := buildOntologyDelta(schema, noteState, recomputePaths, plan.DeletePaths, replacePaths, results, existingEdges, fullRebuild)
	issues, err = mergeCurrentOntologyIssues(allNotePaths, assessmentRows, decodedAssessments, replacePaths, issues, fullRebuild)
	if err != nil {
		return nil, err
	}
	sortValidationIssues(issues)
	if delta.SchemaState != nil {
		delta.SchemaState.ErrorJSON = mustJSON(issues)
	}
	for path := range replacePaths {
		catalogPaths = append(catalogPaths, path)
	}
	catalogPaths = normalizeStringSet(append(catalogPaths, plan.DeletePaths...))
	indexingperf.AddCount(indexingperf.WithPhase(ctx, "compute_ontology"), indexingperf.OntologyRelations, int64(relations))

	catalogReadModel.NotePaths = catalogPaths
	delta.ReadModels = []readmodel.NodeReadModel{catalogReadModel}
	delta.Readiness = ontologyReadinessPublication(schema, formats, *delta.SchemaState, noteState)
	if err := withOntologyPhase(ctx, "write_ontology", func(phaseCtx context.Context) error {
		indexingperf.AddCount(phaseCtx, indexingperf.OntologyDeltaRows, int64(delta.RowCount()))
		return writeQueue.SubmitOntologyDelta(phaseCtx, delta)
	}); err != nil {
		return nil, err
	}

	return &SyncResult{
		Schema:     schema,
		NotesHash:  noteState.NotesHash,
		Dirty:      fullRebuild || policiesChanged || len(results) > 0 || len(plan.DeletePaths) > 0,
		Rebuilt:    fullRebuild,
		Issues:     issues,
		Considered: len(plan.LoadPaths),
		Skipped:    skipped,
		Assessed:   len(results),
		Relations:  relations,
	}, nil
}

func withOntologyPhase(ctx context.Context, name string, fn func(context.Context) error) error {
	phaseCtx := indexingperf.WithPhase(ctx, name)
	done := indexingperf.StartSpan(phaseCtx, name)
	err := fn(phaseCtx)
	done(err)
	return err
}

func normalizeStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
