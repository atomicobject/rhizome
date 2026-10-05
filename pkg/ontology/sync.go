package ontology

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// deltaWriteQueue is the single durable-writer lane for ontology rebuilds.
// Both the assessment/edge delta and the ontology_nodes catalog read model
// must flow through the queue so producers can run in parallel without
// stepping on each other; see SPEC-0040 "Replacement Boundary".
type deltaWriteQueue interface {
	SubmitOntologyDelta(context.Context, semdb.OntologyDelta) error
	SubmitOntologyNodeReadModel(context.Context, codeanchor.IntelOntologyNodeReadModel) error
}

// SyncResult summarizes incremental ontology convergence.
//
// Considered counts load candidates, Skipped counts unchanged candidates, and
// Assessed counts notes that actually rebuilt validation/type/edge state.
type SyncResult struct {
	Schema     *Schema
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
	doc         *noteDoc
	assessment  *NoteAssessment
	fingerprint string
	meta        semdb.NoteMetadataRow
}

type noteSyncResult struct {
	path        string
	assessment  *NoteAssessment
	row         *semdb.OntologyNoteAssessmentRow
	noteState   *semdb.OntologyNoteStateRow
	noteType    *semdb.OntologyNoteTypeRow
	edges       []semdb.OntologyEdgeRow
	issues      []ValidationIssue
	relationCnt int
}

// SyncPaths incrementally converges ontology read-model rows for changed or
// deleted notes.
//
// The plan expands direct changes through existing doc and ontology edges so
// relation-dependent neighbors are refreshed without falling back to a full
// snapshot rebuild. Schema-hash changes still force a full rebuild.
func SyncPaths(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, writeQueue deltaWriteQueue, changedPaths, deletedPaths []string) (*SyncResult, error) {
	return syncPaths(ctx, noteMetadata, vaultDef, noteMgr, store, writeQueue, changedPaths, deletedPaths, false)
}

// SyncPublishedPaths converges ontology from note metadata that the caller has
// already published through its durable writer barrier. Unlike SyncPaths, an
// empty path set does not rediscover or republish note metadata.
func SyncPublishedPaths(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, writeQueue deltaWriteQueue, changedPaths, deletedPaths []string) (*SyncResult, error) {
	return syncPaths(ctx, noteMetadata, vaultDef, noteMgr, store, writeQueue, changedPaths, deletedPaths, true)
}

func syncPaths(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, writeQueue deltaWriteQueue, changedPaths, deletedPaths []string, sourcePublished bool) (*SyncResult, error) {
	// Docs: [[ontology-indexed-read-model-contract#^spec-0040-full-rebuild]]
	// and [[ontology-indexed-read-model-contract#^spec-0040-incremental-convergence]]
	// define this function as the convergence boundary for indexed ontology rows.
	if store == nil {
		return &SyncResult{}, nil
	}
	if err := noteMetadata.Validate(); err != nil {
		return nil, err
	}
	state, err := store.GetOntologySchemaState(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateOntologyMaterializationVersion(state.MaterializationVersion); err != nil {
		return nil, err
	}
	changedPaths = normalizeStringSet(changedPaths)
	deletedPaths = normalizeStringSet(deletedPaths)
	inputNoExplicitPaths := len(changedPaths) == 0 && len(deletedPaths) == 0

	schema, schemaErr := LoadSchema(vaultDef.BasePath())
	if schemaErr != nil {
		if schemaErr == ErrNoOntologyFiles {
			// A missing schema still means "replace derived ontology with empty".
			// Keep that destructive reset on the same durable writer lane used by
			// ordinary validation projection; direct replacement is only the legacy
			// fallback for callers without a queue.
			if writeQueue != nil {
				if err := writeQueue.SubmitOntologyDelta(ctx, semdb.OntologyDelta{FullRebuild: true}); err != nil {
					return nil, err
				}
			} else if err := store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{}); err != nil {
				return nil, err
			}
			return &SyncResult{}, nil
		}
		if sourcePublished {
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
			if writeQueue == nil {
				return nil, fmt.Errorf("published ontology sync requires a durable write queue")
			}
			if err := writeQueue.SubmitOntologyDelta(ctx, semdb.OntologyDelta{SchemaState: &state}); err != nil {
				return nil, err
			}
			return &SyncResult{
				NotesHash: noteState.NotesHash,
				Dirty:     true,
				Issues:    issues,
			}, nil
		}
		if _, err := EnsureIndexed(ctx, noteMetadata, vaultDef, noteMgr, store); err != nil {
			return nil, err
		}
		return &SyncResult{}, schemaErr
	}
	noteState, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, err
	}
	if !noteState.Ready || noteState.LoadedAt == 0 {
		if sourcePublished {
			return nil, fmt.Errorf("published note metadata is not ready")
		}
		if _, err := noteMetadata.EnsureIndexed(ctx, vaultDef, noteMgr, store); err != nil {
			return nil, err
		}
		noteState, err = store.GetNoteMetadataState(ctx)
		if err != nil {
			return nil, err
		}
	}
	if inputNoExplicitPaths && !sourcePublished {
		if _, err := noteMetadata.EnsureIndexed(ctx, vaultDef, noteMgr, store); err != nil {
			return nil, err
		}
		noteState, err = store.GetNoteMetadataState(ctx)
		if err != nil {
			return nil, err
		}
	}
	ontologyPaths, err := ProjectableMetadataPaths(ctx, noteMetadata, store)
	if err != nil {
		return nil, err
	}
	changedPaths = retainPaths(changedPaths, ontologyPaths)
	noExplicitPaths := len(changedPaths) == 0 && len(deletedPaths) == 0
	fullRebuild := !state.Ready || strings.TrimSpace(state.SchemaHash) == "" || state.SchemaHash != schema.Hash || state.MaterializationVersion != OntologyMaterializationVersion || (noExplicitPaths && state.NotesHash != noteState.NotesHash)
	if !fullRebuild && noExplicitPaths {
		materialized, err := ontologyStateMaterialized(ctx, noteMetadata, store, schema, state.ErrorJSON)
		if err != nil {
			return nil, err
		}
		fullRebuild = !materialized
	}
	if fullRebuild {
		if !sourcePublished {
			if _, err := noteMetadata.EnsureIndexed(ctx, vaultDef, noteMgr, store); err != nil {
				return nil, err
			}
			noteState, err = store.GetNoteMetadataState(ctx)
			if err != nil {
				return nil, err
			}
		}
	}
	if !fullRebuild && noExplicitPaths {
		return &SyncResult{
			Schema:    schema,
			NotesHash: noteState.NotesHash,
			Issues:    decodeIssues(state.ErrorJSON),
		}, nil
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
	if !fullRebuild && len(plan.LoadPaths) == 0 && len(plan.DeletePaths) == 0 {
		return &SyncResult{
			Schema:    schema,
			NotesHash: noteState.NotesHash,
			Issues:    decodeIssues(state.ErrorJSON),
		}, nil
	}

	allNotePaths := ontologyPaths
	var (
		docsByPath         map[string]*noteDoc
		metaByPath         map[string]semdb.NoteMetadataRow
		noteStates         map[string]semdb.OntologyNoteStateRow
		assessmentRows     map[string]semdb.OntologyNoteAssessmentRow
		decodedAssessments map[string]*NoteAssessment
		existingEdges      []semdb.OntologyEdgeRow
		incidentGraphRows  []semdb.GraphDocEdge
	)
	if err := withOntologyPhase(ctx, "load_ontology_inputs", func(phaseCtx context.Context) error {
		var err error
		noteStates, err = store.OntologyNoteStatesByPaths(phaseCtx, allNotePaths)
		if err != nil {
			return err
		}
		if !fullRebuild {
			assessmentRows, err = store.OntologyAssessmentsByPaths(phaseCtx, allNotePaths)
			if err != nil {
				return err
			}
			catalogNodes, err := store.OntologyNodesByPaths(phaseCtx, allNotePaths)
			if err != nil {
				return err
			}
			var incompletePaths []string
			incompletePaths, decodedAssessments = incompleteOntologyPaths(allNotePaths, assessmentRows, noteStates, catalogNodes, schema.Hash)
			promoteIncompleteOntologyPaths(plan, incompletePaths)
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

	cache := obsidian.BuildNotePathCacheWithAliases(allNotePaths, notemeta.LoadAliasMap(ctx, store))
	graphByPath := groupIncidentGraphRows(incidentGraphRows)
	jobs, replacePaths, skipped := prepareOntologyJobs(plan, schema, docsByPath, metaByPath, noteStates, graphByPath, state.MaterializationVersion)
	recomputePaths := make([]string, 0, len(jobs))
	for _, job := range jobs {
		recomputePaths = append(recomputePaths, job.doc.Path)
	}
	indexingperf.AddCount(indexingperf.WithPhase(ctx, "compute_ontology"), "ontology.notes_considered", int64(len(plan.LoadPaths)))
	indexingperf.AddCount(indexingperf.WithPhase(ctx, "compute_ontology"), "ontology.notes_skipped", int64(skipped))

	externalTypes, err := loadExternalOntologyTypes(ctx, store, schema, jobs, cache, docsByPath, replacePaths, incidentGraphRows)
	if err != nil {
		return nil, err
	}
	results, err := computeOntologyJobs(ctx, jobs, schema, cache, graphByPath, externalTypes)
	if err != nil {
		return nil, err
	}

	catalogReadModel, err := buildOntologyNodeCatalogReadModel(schema, docsByPath, results, ontologyLinkTargetResolver(cache, ontologyTypeResolverFromSyncResults(results, externalTypes)))
	if err != nil {
		return nil, err
	}
	catalog := catalogNodeWitnesses(catalogReadModel.Nodes)
	for _, result := range results {
		if result.assessment != nil {
			witness := catalogWitnessForPath(catalog, result.path)
			result.assessment.CatalogNodeCount = &witness.Count
			result.assessment.CatalogNodeDigest = witness.Digest
			result.assessment.SourceContentHash = metaByPath[result.path].ContentHash
		}
	}
	catalogPaths := catalogReadModel.NotePaths
	results = appendFrozenStoryScopeEdges(schema, docsByPath, results, catalogReadModel.Nodes)
	delta, issues, relations := buildOntologyDelta(schema, noteState, recomputePaths, plan.DeletePaths, replacePaths, results, existingEdges, fullRebuild)
	issues, err = mergeCurrentOntologyIssues(allNotePaths, assessmentRows, decodedAssessments, replacePaths, issues, fullRebuild)
	if err != nil {
		return nil, err
	}
	if delta.SchemaState != nil {
		delta.SchemaState.ErrorJSON = mustJSON(issues)
	}
	for path := range replacePaths {
		catalogPaths = append(catalogPaths, path)
	}
	catalogPaths = normalizeStringSet(append(catalogPaths, plan.DeletePaths...))
	indexingperf.AddCount(indexingperf.WithPhase(ctx, "compute_ontology"), "ontology.relations", int64(relations))
	if delta.RowCount() == 0 && !fullRebuild {
		if len(catalogPaths) > 0 {
			// Docs: [[ontology-indexed-read-model-contract#^spec-0040-catalog-refresh]]
			// requires catalog-only changes to refresh ontology_nodes even when
			// assessment/type/edge delta rows are unchanged. Route through the
			// queue (when wired) so writes share the single-writer lane with
			// the assessment/edge delta; see SPEC-0040 "Replacement Boundary".
			// `field_rows_planned` is incremented inside SubmitOntologyNodeReadModel,
			// the single durable point, so producers must not increment it here.
			if err := withOntologyPhase(ctx, "write_ontology_catalog", func(phaseCtx context.Context) error {
				catalogReadModel.NotePaths = catalogPaths
				if writeQueue != nil {
					return writeQueue.SubmitOntologyNodeReadModel(phaseCtx, catalogReadModel)
				}
				return store.ReplaceOntologyNodeReadModel(phaseCtx, catalogReadModel)
			}); err != nil {
				return nil, err
			}
		}
		return &SyncResult{
			Schema:     schema,
			NotesHash:  noteState.NotesHash,
			Considered: len(plan.LoadPaths),
			Skipped:    skipped,
			Assessed:   len(results),
			Relations:  relations,
			Issues:     issues,
		}, nil
	}

	catalogReadModel.NotePaths = catalogPaths
	delta.ReadModels = []codeanchor.IntelOntologyNodeReadModel{catalogReadModel}
	if err := withOntologyPhase(ctx, "write_ontology", func(phaseCtx context.Context) error {
		indexingperf.AddCount(phaseCtx, "ontology.delta_rows", int64(delta.RowCount()))
		if writeQueue != nil {
			return writeQueue.SubmitOntologyDelta(phaseCtx, delta)
		}
		return store.ApplyOntologyDelta(phaseCtx, delta)
	}); err != nil {
		return nil, err
	}

	return &SyncResult{
		Schema:     schema,
		NotesHash:  noteState.NotesHash,
		Dirty:      true,
		Rebuilt:    fullRebuild,
		Issues:     issues,
		Considered: len(plan.LoadPaths),
		Skipped:    skipped,
		Assessed:   len(results),
		Relations:  relations,
	}, nil
}

func retainPaths(paths, allowed []string) []string {
	if len(paths) == 0 || len(allowed) == 0 {
		return nil
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, path := range allowed {
		allowedSet[path] = struct{}{}
	}
	retained := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := allowedSet[path]; ok {
			retained = append(retained, path)
		}
	}
	return retained
}

func mergeCurrentOntologyIssues(paths []string, rows map[string]semdb.OntologyNoteAssessmentRow, decoded map[string]*NoteAssessment, replacing map[string]struct{}, rebuilt []ValidationIssue, fullRebuild bool) ([]ValidationIssue, error) {
	issues := append([]ValidationIssue(nil), rebuilt...)
	if !fullRebuild {
		for _, notePath := range paths {
			if _, ok := replacing[notePath]; ok {
				continue
			}
			assessment := decoded[notePath]
			if assessment == nil {
				row, ok := rows[notePath]
				if !ok {
					return nil, fmt.Errorf("ontology assessment missing for current note %q", notePath)
				}
				var err error
				assessment, err = AssessmentFromJSON(row.AssessmentJSON)
				if err != nil {
					return nil, fmt.Errorf("decode ontology assessment for %q: %w", notePath, err)
				}
			}
			if assessment != nil {
				issues = append(issues, assessment.Issues...)
			}
		}
	}
	sortValidationIssues(issues)
	return issues, nil
}

func incompleteOntologyPaths(paths []string, assessments map[string]semdb.OntologyNoteAssessmentRow, states map[string]semdb.OntologyNoteStateRow, nodes []codeanchor.IntelOntologyNode, schemaHash string) ([]string, map[string]*NoteAssessment) {
	incomplete := make([]string, 0)
	decoded := make(map[string]*NoteAssessment, len(paths))
	catalog := catalogNodeWitnesses(nodes)
	staleCatalog := make(map[string]bool)
	for _, node := range nodes {
		if node.SchemaHash != schemaHash || node.UpdatedAt < states[node.NotePath].UpdatedAt {
			staleCatalog[node.NotePath] = true
		}
	}
	for _, notePath := range paths {
		assessment, assessmentOK := assessments[notePath]
		if assessmentOK && assessment.SchemaHash == schemaHash && strings.TrimSpace(assessment.AssessmentJSON) != "" {
			parsed, err := AssessmentFromJSON(assessment.AssessmentJSON)
			assessmentOK = err == nil && parsed != nil && parsed.NotePath == notePath && parsed.CatalogNodeCount != nil && *parsed.CatalogNodeCount >= 0 && parsed.CatalogNodeDigest != ""
			if assessmentOK {
				decoded[notePath] = parsed
			}
		} else {
			assessmentOK = false
		}
		state, stateOK := states[notePath]
		stateOK = stateOK && state.SchemaHash == schemaHash && strings.TrimSpace(state.InputFingerprint) != ""
		witness := catalogWitnessForPath(catalog, notePath)
		catalogOK := assessmentOK && witness.Count == *decoded[notePath].CatalogNodeCount && witness.Digest == decoded[notePath].CatalogNodeDigest && !staleCatalog[notePath] && assessment.UpdatedAt >= state.UpdatedAt
		if !assessmentOK || !stateOK || !catalogOK {
			incomplete = append(incomplete, notePath)
		}
	}
	return incomplete, decoded
}

func promoteIncompleteOntologyPaths(plan *syncPlan, paths []string) {
	if plan == nil || len(paths) == 0 {
		return
	}
	if plan.ForcePaths == nil {
		plan.ForcePaths = make(map[string]struct{}, len(paths))
	}
	for _, notePath := range paths {
		plan.LoadPaths = append(plan.LoadPaths, notePath)
		plan.ReplacePaths = append(plan.ReplacePaths, notePath)
		plan.ForcePaths[notePath] = struct{}{}
	}
	plan.LoadPaths = normalizeStringSet(plan.LoadPaths)
	plan.ReplacePaths = normalizeStringSet(plan.ReplacePaths)
}

func buildOntologyNodeCatalogReadModel(schema *Schema, docsByPath map[string]*noteDoc, results []noteSyncResult, linkResolver OntologyLinkTargetResolver) (codeanchor.IntelOntologyNodeReadModel, error) {
	if schema == nil || len(results) == 0 {
		return codeanchor.IntelOntologyNodeReadModel{}, nil
	}
	var paths []string
	var nodes []codeanchor.IntelOntologyNode
	var fields []codeanchor.IntelOntologyNodeFieldValue
	var dependencies []codeanchor.IntelOntologyNodeLinkDependency
	now := time.Now().Unix()
	for _, result := range results {
		if strings.TrimSpace(result.path) != "" {
			paths = append(paths, result.path)
		}
		doc := docsByPath[result.path]
		if doc == nil {
			continue
		}
		typeName := ""
		if result.noteType != nil {
			typeName = strings.TrimSpace(result.noteType.TypeName)
		}
		// Untyped notes that don't match any global @source still get a
		// FallbackNote NOTE row so the catalog owns ontology_nodes for every
		// in-scope note. Without this, downstream writers (e.g. the semantic
		// syncer) had to install fallback rows themselves with destructive
		// Replace semantics that wiped catalog-written embedded children.
		projectDoc := *doc
		projectDoc.TypeName = typeName
		projection, err := projectRootNodeForDoc(&projectDoc, schema)
		if err != nil {
			return codeanchor.IntelOntologyNodeReadModel{}, err
		}
		readModel, err := BuildIntelOntologyNodeReadModelWithOptions(schema, projection, now, BuildIntelOntologyNodeReadModelOptions{
			LinkResolver: linkResolver,
		})
		if err != nil {
			return codeanchor.IntelOntologyNodeReadModel{}, err
		}
		paths = append(paths, readModel.NotePaths...)
		nodes = append(nodes, readModel.Nodes...)
		fields = append(fields, readModel.FieldValues...)
		dependencies = append(dependencies, readModel.LinkDependencies...)
	}
	return codeanchor.IntelOntologyNodeReadModel{
		NotePaths:        normalizeStringSet(paths),
		Nodes:            nodes,
		FieldValues:      fields,
		LinkDependencies: dependencies,
	}, nil
}

func ontologyTypeResolverFromSyncResults(results []noteSyncResult, externalTypes map[string]string) func(string) string {
	resolved := make(map[string]string, len(results)+len(externalTypes))
	for path, typeName := range externalTypes {
		if strings.TrimSpace(path) != "" && strings.TrimSpace(typeName) != "" {
			resolved[path] = strings.TrimSpace(typeName)
		}
	}
	for _, result := range results {
		typeName := ""
		if result.assessment != nil {
			typeName = strings.TrimSpace(result.assessment.ResolvedType)
		}
		if typeName == "" && result.noteType != nil {
			typeName = strings.TrimSpace(result.noteType.TypeName)
		}
		if strings.TrimSpace(result.path) != "" && typeName != "" {
			resolved[result.path] = typeName
		}
	}
	return func(path string) string {
		return strings.TrimSpace(resolved[path])
	}
}

func appendFrozenStoryScopeEdges(schema *Schema, docsByPath map[string]*noteDoc, results []noteSyncResult, catalogNodes []codeanchor.IntelOntologyNode) []noteSyncResult {
	if schema == nil || len(results) == 0 || len(catalogNodes) == 0 {
		return results
	}
	storyByID := storyCatalogNodesByID(schema, docsByPath, results, catalogNodes)
	if len(storyByID) == 0 {
		return results
	}
	now := time.Now().Unix()
	for i := range results {
		result := &results[i]
		if result.noteType == nil || result.noteType.TypeName != "EffortNote" {
			continue
		}
		doc := docsByPath[result.path]
		if doc == nil || strings.TrimSpace(doc.Content) == "" {
			continue
		}
		body := sectionBodyByHeading(doc, "Stories In Scope (Frozen)")
		if body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, storyID := range storyIDsInText(body) {
			node, ok := storyByID[storyID]
			if !ok {
				continue
			}
			key := result.path + "\x00" + node.NodeID
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result.edges = append(result.edges, semdb.OntologyEdgeRow{
				SrcPath:      result.path,
				RelationName: "frozenStories",
				DstPath:      node.NotePath,
				DstNodeID:    node.NodeID,
				DstType:      "UserStory",
				Provenance:   "story_id",
				Structural:   true,
				SchemaHash:   schema.Hash,
				UpdatedAt:    now,
			})
		}
	}
	return results
}

func storyCatalogNodesByID(schema *Schema, docsByPath map[string]*noteDoc, results []noteSyncResult, catalogNodes []codeanchor.IntelOntologyNode) map[string]codeanchor.IntelOntologyNode {
	nodesByID := make(map[string]codeanchor.IntelOntologyNode, len(catalogNodes))
	for _, node := range catalogNodes {
		if node.NodeID != "" {
			nodesByID[node.NodeID] = node
		}
	}
	out := make(map[string]codeanchor.IntelOntologyNode)
	for _, result := range results {
		if result.noteType == nil {
			continue
		}
		doc := docsByPath[result.path]
		if doc == nil || strings.TrimSpace(doc.Content) == "" {
			continue
		}
		snapshot, err := documentSnapshotForDoc(doc)
		if err != nil {
			continue
		}
		root, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: doc.Path, Kind: NodeKindNote, TypeName: result.noteType.TypeName})
		if err != nil {
			continue
		}
		walkStoryIDProjections(schema, root, func(projection *NodeProjection) {
			if projection == nil || projection.ResolvedType != "UserStory" {
				return
			}
			idValue := strings.TrimSpace(projectionIdentifierValue(projection))
			if idValue == "" {
				return
			}
			nodeID := OntologyNodeID(projection.Ref)
			node, ok := nodesByID[nodeID]
			if !ok {
				return
			}
			out[idValue] = node
		})
	}
	return out
}

func projectionIdentifierValue(projection *NodeProjection) string {
	if projection == nil || projection.Type == nil {
		return ""
	}
	for _, field := range projection.Type.Fields {
		if field == nil || !field.IsPreferredIdentifier {
			continue
		}
		if binding, ok := projection.Fields[field.Name]; ok && len(binding.Values) > 0 {
			return binding.Values[0]
		}
	}
	for _, field := range projection.Type.Fields {
		if field == nil || !field.IsIdentifier {
			continue
		}
		if binding, ok := projection.Fields[field.Name]; ok && len(binding.Values) > 0 {
			return binding.Values[0]
		}
	}
	return ""
}

func walkStoryIDProjections(schema *Schema, projection *NodeProjection, visit func(*NodeProjection)) {
	if projection == nil {
		return
	}
	visit(projection)
	names := make([]string, 0, len(projection.Fields))
	for name := range projection.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, childRef := range projection.Fields[name].SectionNodes {
			child, err := ProjectBoundNodeFromSnapshot(projection.Snapshot, schema, childRef)
			if err != nil {
				continue
			}
			walkStoryIDProjections(schema, child, visit)
		}
	}
}

// sectionBodyByHeading is a Markdown-only sync helper. Its callers receive
// documents from the format-gated Markdown projection path.
func sectionBodyByHeading(doc *noteDoc, heading string) string {
	if doc == nil {
		return ""
	}
	snapshot, err := BuildDocumentSnapshot(doc.Path, doc.Content, time.Time{})
	if err != nil {
		return ""
	}
	var found *SectionNode
	var walk func([]*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if found != nil {
				return
			}
			if strings.EqualFold(strings.TrimSpace(node.Title), strings.TrimSpace(heading)) {
				found = node
				return
			}
			walk(node.Children)
		}
	}
	walk(snapshot.Sections)
	return SectionBody(found)
}

func storyIDsInText(text string) []string {
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	})
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, token := range tokens {
		token = strings.Trim(token, ".,;:()[]{}<>")
		upper := strings.ToUpper(token)
		if !strings.Contains(upper, ".US") || !strings.HasPrefix(upper, "SPEC-") {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func planOntologySync(ctx context.Context, store *semdb.Store, changedPaths, deletedPaths []string) (_ *syncPlan, err error) {
	phaseCtx := indexingperf.WithPhase(ctx, "plan_ontology")
	done := indexingperf.StartSpan(phaseCtx, "plan_ontology")
	defer func() { done(err) }()

	changedPaths = normalizeStringSet(changedPaths)
	deletedPaths = normalizeStringSet(deletedPaths)
	touched := append([]string(nil), changedPaths...)
	touched = append(touched, deletedPaths...)
	touched = normalizeStringSet(touched)
	if len(touched) == 0 {
		return &syncPlan{ForcePaths: map[string]struct{}{}}, nil
	}

	affected := make(map[string]struct{}, len(touched)*3)
	// Dependency work must survive membership in metadata's expanded candidate set.
	forcePaths := make(map[string]struct{}, len(touched)*3)
	for _, path := range touched {
		affected[path] = struct{}{}
	}

	if len(changedPaths) > 0 {
		rows, err := store.GraphDocNoteEdgesForPaths(phaseCtx, changedPaths)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			affected[row.SrcPath] = struct{}{}
			affected[row.DstPath] = struct{}{}
			forcePaths[row.SrcPath] = struct{}{}
			forcePaths[row.DstPath] = struct{}{}
		}
	}

	edges, err := store.OntologyEdgesForPaths(phaseCtx, touched, true, "", 0)
	if err != nil {
		return nil, err
	}
	for _, edge := range edges {
		affected[edge.SrcPath] = struct{}{}
		affected[edge.DstPath] = struct{}{}
		forcePaths[edge.SrcPath] = struct{}{}
		forcePaths[edge.DstPath] = struct{}{}
	}

	// Renames surface as a (deletedPath, changedPath) pair where the deleted
	// side carried unresolved link dependencies keyed by old basename/alias.
	// Feed both sides into target_input_norm planning so dependents like
	// `[[alice]]` get re-evaluated when alice.md disappears.
	dependencyPaths := append([]string(nil), changedPaths...)
	dependencyPaths = append(dependencyPaths, deletedPaths...)
	linkDependencySources, err := store.OntologyNodeLinkDependencySources(phaseCtx, touched, ontologyDependencyTargetInputNorms(dependencyPaths, notemeta.LoadAliasMap(phaseCtx, store)))
	if err != nil {
		return nil, err
	}
	for _, sourcePath := range linkDependencySources {
		affected[sourcePath] = struct{}{}
		forcePaths[sourcePath] = struct{}{}
	}

	deletedSet := make(map[string]struct{}, len(deletedPaths))
	for _, path := range deletedPaths {
		deletedSet[path] = struct{}{}
	}
	loadPaths := make([]string, 0, len(affected))
	replacePaths := make([]string, 0, len(affected))
	for path := range affected {
		if _, deleted := deletedSet[path]; deleted {
			delete(forcePaths, path)
			continue
		}
		loadPaths = append(loadPaths, path)
		replacePaths = append(replacePaths, path)
	}
	sort.Strings(loadPaths)
	sort.Strings(replacePaths)
	return &syncPlan{
		LoadPaths:    loadPaths,
		ReplacePaths: replacePaths,
		DeletePaths:  deletedPaths,
		ForcePaths:   forcePaths,
	}, nil
}

func ontologyDependencyTargetInputNorms(paths []string, aliasesByPath map[string][]string) []string {
	seen := make(map[string]struct{})
	for _, notePath := range paths {
		notePath = strings.TrimSpace(notePath)
		if notePath == "" {
			continue
		}
		for _, value := range []string{
			notePath,
			strings.TrimSuffix(notePath, path.Ext(notePath)),
			strings.TrimSuffix(path.Base(notePath), path.Ext(notePath)),
		} {
			if norm := normalizeOntologyFieldValue(value); norm != "" {
				seen[norm] = struct{}{}
			}
		}
		for _, alias := range aliasesByPath[notePath] {
			if norm := normalizeOntologyFieldValue(alias); norm != "" {
				seen[norm] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func prepareOntologyJobs(plan *syncPlan, schema *Schema, docsByPath map[string]*noteDoc, metaByPath map[string]semdb.NoteMetadataRow, states map[string]semdb.OntologyNoteStateRow, graphByPath map[string][]semdb.GraphDocEdge, materializationVersion int) ([]noteSyncJob, map[string]struct{}, int) {
	jobs := make([]noteSyncJob, 0, len(plan.LoadPaths))
	skipped := 0
	for _, path := range plan.LoadPaths {
		doc := docsByPath[path]
		if doc == nil {
			continue
		}
		assessment := resolveNoteAssessment(doc, schema)
		fingerprint := ontologyFingerprint(doc, metaByPath[path], schema.Hash, graphByPath[path])
		if _, forced := plan.ForcePaths[path]; !forced && materializationVersion == OntologyMaterializationVersion {
			if state, ok := states[path]; ok && state.SchemaHash == schema.Hash && state.InputFingerprint == fingerprint {
				skipped++
				continue
			}
		}
		jobs = append(jobs, noteSyncJob{
			doc:         doc,
			assessment:  assessment,
			fingerprint: fingerprint,
			meta:        metaByPath[path],
		})
	}
	replaceSet := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		replaceSet[job.doc.Path] = struct{}{}
	}
	return jobs, replaceSet, skipped
}

func loadExternalOntologyTypes(ctx context.Context, store *semdb.Store, schema *Schema, jobs []noteSyncJob, cache *obsidian.NotePathCache, docsByPath map[string]*noteDoc, replaceSet map[string]struct{}, graphRows []semdb.GraphDocEdge) (map[string]string, error) {
	targets := make(map[string]struct{}, len(jobs)*4)
	for _, job := range jobs {
		if job.assessment == nil || job.assessment.ResolvedType == "" {
			if docMatchesAnyGlobalSource(job.doc, schema) {
				globalTargets, err := globalSourceLinkTargets(job.doc, schema, cache)
				if err != nil {
					return nil, err
				}
				for _, targetPath := range globalTargets {
					if _, replacing := replaceSet[targetPath]; replacing {
						continue
					}
					targets[targetPath] = struct{}{}
				}
			}
			continue
		}
		noteType := schema.Types[job.assessment.ResolvedType]
		if noteType != nil {
			for _, field := range noteType.Fields {
				if field.Kind != FieldKindLink {
					continue
				}
				for _, raw := range extractFieldValues(job.doc, field) {
					targetPath, ok := cache.ResolveNote(unwrapLinkValue(raw))
					if !ok {
						continue
					}
					if _, replacing := replaceSet[targetPath]; replacing {
						continue
					}
					targets[targetPath] = struct{}{}
				}
			}
		}
		if docMatchesAnyGlobalSource(job.doc, schema) {
			globalTargets, err := globalSourceLinkTargets(job.doc, schema, cache)
			if err != nil {
				return nil, err
			}
			for _, targetPath := range globalTargets {
				if _, replacing := replaceSet[targetPath]; replacing {
					continue
				}
				targets[targetPath] = struct{}{}
			}
		}
	}
	for _, row := range graphRows {
		for _, path := range []string{row.SrcPath, row.DstPath} {
			if _, replacing := replaceSet[path]; replacing {
				continue
			}
			targets[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(targets))
	for path := range targets {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	rows, err := store.OntologyTypesByPaths(indexingperf.WithPhase(ctx, "load_ontology_inputs"), paths)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for path, row := range rows {
		out[path] = row.TypeName
	}
	return out, nil
}

func globalSourceLinkTargets(doc *noteDoc, schema *Schema, cache *obsidian.NotePathCache) ([]string, error) {
	if doc == nil || schema == nil || cache == nil || strings.TrimSpace(doc.Content) == "" {
		return nil, nil
	}
	snapshot, err := documentSnapshotForDoc(doc)
	if err != nil {
		return nil, err
	}
	refs, err := GlobalSourceNodeRefsFromSnapshot(snapshot, schema)
	if err != nil {
		return nil, err
	}
	seenRefs := map[string]struct{}{}
	seenTargets := map[string]struct{}{}
	var out []string
	var walk func(NodeRef) error
	walk = func(ref NodeRef) error {
		key := ref.String()
		if _, ok := seenRefs[key]; ok {
			return nil
		}
		seenRefs[key] = struct{}{}
		projection, err := ProjectBoundNodeFromSnapshot(snapshot, schema, ref)
		if err != nil {
			return err
		}
		if projection == nil || projection.Type == nil {
			return nil
		}
		for _, field := range projection.Type.Fields {
			if field == nil || field.Kind != FieldKindLink {
				continue
			}
			for _, raw := range projection.Fields[field.Name].Values {
				targetPath, ok := cache.ResolveNote(unwrapLinkValue(raw))
				if !ok {
					continue
				}
				if _, exists := seenTargets[targetPath]; exists {
					continue
				}
				seenTargets[targetPath] = struct{}{}
				out = append(out, targetPath)
			}
		}
		for _, binding := range projection.Fields {
			for _, childRef := range binding.SectionNodes {
				if err := walk(childRef); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, ref := range refs {
		if err := walk(ref); err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func computeOntologyJobs(ctx context.Context, jobs []noteSyncJob, schema *Schema, cache *obsidian.NotePathCache, graphByPath map[string][]semdb.GraphDocEdge, externalTypes map[string]string) (_ []noteSyncResult, err error) {
	if len(jobs) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	phaseCtx := indexingperf.WithPhase(ctx, "compute_ontology")
	done := indexingperf.StartSpan(phaseCtx, "compute_ontology")
	defer func() { done(err) }()

	computedTypes := make(map[string]string, len(jobs))
	docTypes := make(map[string]*NoteType, len(jobs))
	for _, job := range jobs {
		if job.assessment == nil || job.assessment.ResolvedType == "" {
			continue
		}
		computedTypes[job.doc.Path] = job.assessment.ResolvedType
		docTypes[job.doc.Path] = schema.Types[job.assessment.ResolvedType]
	}
	resolveType := func(path string) string {
		if v := strings.TrimSpace(computedTypes[path]); v != "" {
			return v
		}
		return strings.TrimSpace(externalTypes[path])
	}

	workerCount := min(max(runtime.GOMAXPROCS(0), 2), len(jobs))
	jobCh := make(chan noteSyncJob)
	resultCh := make(chan noteSyncResult, len(jobs))
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				res, err := computeOntologyJob(job, schema, cache, graphByPath[job.doc.Path], docTypes[job.doc.Path], resolveType)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				resultCh <- res
			}
		}()
	}

	canceled := false
	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-ctx.Done():
			canceled = true
		}
		if canceled {
			break
		}
	}
	close(jobCh)
	wg.Wait()
	close(resultCh)
	if canceled {
		return nil, ctx.Err()
	}

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	results := make([]noteSyncResult, 0, len(jobs))
	for res := range resultCh {
		results = append(results, res)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].path < results[j].path })
	indexingperf.AddCount(phaseCtx, "ontology.notes_assessed", int64(len(results)))
	return results, nil
}

func computeOntologyJob(job noteSyncJob, schema *Schema, cache *obsidian.NotePathCache, incidentRows []semdb.GraphDocEdge, noteType *NoteType, resolveType func(string) string) (noteSyncResult, error) {
	now := time.Now().Unix()
	res := noteSyncResult{path: job.doc.Path}
	assessment := NoteAssessment{NotePath: job.doc.Path}
	if job.assessment != nil {
		assessment = *job.assessment
	}
	res.assessment = &assessment
	res.row = &semdb.OntologyNoteAssessmentRow{
		NotePath:     job.doc.Path,
		DeclaredType: assessment.DeclaredType,
		ResolvedType: assessment.ResolvedType,
		SchemaHash:   schema.Hash,
		UpdatedAt:    now,
	}
	res.noteState = &semdb.OntologyNoteStateRow{
		NotePath:         job.doc.Path,
		InputFingerprint: job.fingerprint,
		SchemaHash:       schema.Hash,
		ResolvedType:     assessment.ResolvedType,
		UpdatedAt:        now,
	}
	if assessment.ResolvedType == "" || noteType == nil {
		if err := appendGlobalSourceSyncArtifacts(&res, job.doc, schema, cache, resolveType, now, &assessment); err != nil {
			return noteSyncResult{}, err
		}
		setAssessmentRow(res.row, assessment)
		return res, nil
	}
	resolvedDoc := *job.doc
	resolvedDoc.TypeName = assessment.ResolvedType

	res.noteType = &semdb.OntologyNoteTypeRow{
		NotePath:   job.doc.Path,
		TypeName:   assessment.ResolvedType,
		SchemaHash: schema.Hash,
		UpdatedAt:  now,
	}
	fieldAssessments := make([]FieldAssessment, 0, len(noteType.Fields))
	relationAssessments := make([]RelationAssessment, 0, len(noteType.Fields))
	relationByName := make(map[string]*RelationAssessment, len(noteType.Fields))
	structuralPairs := make(map[string]struct{})
	for _, field := range noteType.Fields {
		switch field.Kind {
		case FieldKindLink:
			fieldAssessment, relationAssessment, fieldEdges, fieldIssues := assessLinkField(&resolvedDoc, field, cache, resolveType, schema, now)
			fieldAssessments = append(fieldAssessments, fieldAssessment)
			relationAssessments = append(relationAssessments, relationAssessment)
			relationByName[field.Name] = &relationAssessments[len(relationAssessments)-1]
			res.edges = append(res.edges, fieldEdges...)
			res.issues = append(res.issues, fieldIssues...)
			for _, edge := range fieldEdges {
				structuralPairs[edge.SrcPath+"\x00"+edge.DstPath] = struct{}{}
			}
		case FieldKindNeighbor, FieldKindReverse:
			fieldAssessments = append(fieldAssessments, emptyFieldAssessment(field))
			relationAssessments = append(relationAssessments, emptyRelationAssessment(field))
			relationByName[field.Name] = &relationAssessments[len(relationAssessments)-1]
		case FieldKindSection:
			fieldAssessment, fieldIssues := assessSectionField(&resolvedDoc, noteType, field, schema)
			fieldAssessments = append(fieldAssessments, fieldAssessment)
			res.issues = append(res.issues, fieldIssues...)
		default:
			fieldAssessment, fieldIssues := assessScalarField(&resolvedDoc, noteType, field, schema)
			fieldAssessments = append(fieldAssessments, fieldAssessment)
			res.issues = append(res.issues, fieldIssues...)
		}
	}
	nodeEdges, err := buildNodeScopedStructuralEdges(&resolvedDoc, schema, cache, resolveType, now)
	if err != nil {
		return noteSyncResult{}, err
	}
	res.edges = append(res.edges, nodeEdges...)
	ambientEdges := buildAmbientEdgesForSync(job.doc.Path, assessment.ResolvedType, incidentRows, structuralPairs, resolveType, schema, now)
	res.edges = append(res.edges, ambientEdges...)
	for _, edge := range res.edges {
		relation := relationByName[edge.RelationName]
		if relation == nil {
			continue
		}
		target := RelationTarget{
			Path:       edge.DstPath,
			TypeName:   edge.DstType,
			Provenance: edge.Provenance,
			Structural: edge.Structural,
		}
		if relationHasTarget(relation.Targets, edge) {
			continue
		}
		relation.Present = true
		relation.Targets = append(relation.Targets, target)
	}
	assessment.Fields = fieldAssessments
	assessment.Relations = relationAssessments
	customIssues := semanticValidationIssues(&resolvedDoc, noteType, &assessment, schema)
	res.issues = append(res.issues, customIssues...)
	assessment.Issues = append(assessment.Issues, res.issues...)
	sort.SliceStable(assessment.Issues, func(i, j int) bool {
		if assessment.Issues[i].Code != assessment.Issues[j].Code {
			return assessment.Issues[i].Code < assessment.Issues[j].Code
		}
		return assessment.Issues[i].FieldName < assessment.Issues[j].FieldName
	})
	res.relationCnt = len(res.edges)
	setAssessmentRow(res.row, assessment)
	return res, nil
}

func appendGlobalSourceSyncArtifacts(res *noteSyncResult, doc *noteDoc, schema *Schema, cache *obsidian.NotePathCache, resolveType func(string) string, now int64, assessment *NoteAssessment) error {
	if res == nil || assessment == nil || !docMatchesAnyGlobalSource(doc, schema) {
		return nil
	}
	nodeEdges, err := buildNodeScopedStructuralEdges(doc, schema, cache, resolveType, now)
	if err != nil {
		return err
	}
	res.edges = append(res.edges, nodeEdges...)
	snapshot, err := documentSnapshotForDoc(doc)
	if err != nil {
		return err
	}
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: doc.Path, Kind: NodeKindNote})
	if err != nil {
		return err
	}
	embeddedIssues := validateEmbeddedProjectionFields(snapshot, schema, projection)
	res.issues = append(res.issues, embeddedIssues...)
	assessment.Issues = append(assessment.Issues, res.issues...)
	sort.SliceStable(assessment.Issues, func(i, j int) bool {
		if assessment.Issues[i].Code != assessment.Issues[j].Code {
			return assessment.Issues[i].Code < assessment.Issues[j].Code
		}
		return assessment.Issues[i].FieldName < assessment.Issues[j].FieldName
	})
	res.relationCnt = len(res.edges)
	return nil
}

func buildOntologyDelta(schema *Schema, noteState semdb.NoteMetadataState, recomputePaths []string, deletePaths []string, replaceSet map[string]struct{}, results []noteSyncResult, existingEdges []semdb.OntologyEdgeRow, fullRebuild bool) (semdb.OntologyDelta, []ValidationIssue, int) {
	delta := semdb.OntologyDelta{
		FullRebuild:  fullRebuild,
		DeletePaths:  deletePaths,
		ReplacePaths: recomputePaths,
		EdgeSources:  recomputePaths,
		SchemaState: &semdb.OntologySchemaState{
			SchemaHash:             schema.Hash,
			NotesHash:              noteState.NotesHash,
			MaterializationVersion: OntologyMaterializationVersion,
			LoadedAt:               noteState.LoadedAt,
			Ready:                  true,
		},
	}
	if fullRebuild {
		delta.TypePolicies = buildTypePolicyRows(schema, noteState.LoadedAt)
	}
	issues := make([]ValidationIssue, 0)
	relations := 0
	newStructural := make([]semdb.OntologyEdgeRow, 0)
	assessmentByPath := make(map[string]*NoteAssessment, len(results))
	for i := range results {
		res := results[i]
		if res.row != nil {
			delta.Assessments = append(delta.Assessments, *res.row)
		}
		if res.noteState != nil {
			delta.NoteStates = append(delta.NoteStates, *res.noteState)
		}
		if res.noteType != nil {
			delta.NoteTypes = append(delta.NoteTypes, *res.noteType)
		}
		if len(res.edges) > 0 {
			delta.Edges = append(delta.Edges, res.edges...)
			for _, edge := range res.edges {
				if edge.Structural {
					newStructural = append(newStructural, edge)
				}
			}
		}
		if res.assessment != nil {
			assessmentByPath[res.path] = res.assessment
			issues = append(issues, res.assessment.Issues...)
		}
		relations += res.relationCnt
	}
	existingStructural := make(map[string]struct{}, len(existingEdges))
	for _, edge := range existingEdges {
		if !edge.Structural {
			continue
		}
		if _, replacing := replaceSet[edge.SrcPath]; replacing {
			continue
		}
		existingStructural[edge.SrcPath+"\x00"+edge.RelationName+"\x00"+edge.DstPath] = struct{}{}
	}
	inverseIssues := validateInversesIncremental(newStructural, assessmentByPath, schema, existingStructural)
	issues = append(issues, inverseIssues...)
	appendIssuesToAssessments(assessmentByPath, inverseIssues)
	for i := range delta.Assessments {
		assessment := assessmentByPath[delta.Assessments[i].NotePath]
		if assessment == nil {
			continue
		}
		setAssessmentRow(&delta.Assessments[i], *assessment)
	}
	if delta.SchemaState != nil {
		delta.SchemaState.ErrorJSON = mustJSON(issues)
	}
	delta = semdb.NormalizeOntologyDelta(delta)
	return delta, issues, relations
}

func validateInversesIncremental(edges []semdb.OntologyEdgeRow, assessments map[string]*NoteAssessment, schema *Schema, existingStructural map[string]struct{}) []ValidationIssue {
	seen := make(map[string]struct{}, len(edges)+len(existingStructural))
	for key := range existingStructural {
		seen[key] = struct{}{}
	}
	for _, edge := range edges {
		seen[edge.SrcPath+"\x00"+edge.RelationName+"\x00"+edge.DstPath] = struct{}{}
	}
	issues := make([]ValidationIssue, 0)
	for _, edge := range edges {
		assessment := assessments[edge.SrcPath]
		if assessment == nil || assessment.ResolvedType == "" {
			continue
		}
		srcType := schema.Types[assessment.ResolvedType]
		if srcType == nil {
			continue
		}
		field := srcType.ByName[edge.RelationName]
		if field == nil || field.Inverse == "" || field.TypeName == "Note" {
			continue
		}
		inverseKey := edge.DstPath + "\x00" + field.Inverse + "\x00" + edge.SrcPath
		if _, ok := seen[inverseKey]; ok {
			continue
		}
		issues = append(issues, withTypeFieldVariant(ValidationIssue{
			Code:         "inverse_mismatch",
			NotePath:     edge.SrcPath,
			TypeName:     assessment.ResolvedType,
			FieldName:    field.Name,
			Message:      fmt.Sprintf("field %s expects inverse %s on %s", field.Name, field.Inverse, edge.DstPath),
			FixTarget:    edge.DstPath,
			FixFieldName: field.Inverse,
		}))
	}
	return issues
}

func loadNoteDocsFromStoreByPaths(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, notePaths []string) (map[string]*noteDoc, map[string]semdb.NoteMetadataRow, error) {
	rowsByPath, err := store.CurrentNoteMetadataRowsByPaths(ctx, notePaths)
	if err != nil {
		return nil, nil, err
	}
	sources, err := noteMetadata.LoadNoteSourceSnapshots(ctx, vaultDef, noteMgr, store, notePaths)
	if err != nil {
		return nil, nil, err
	}
	docsByPath := make(map[string]*noteDoc, len(sources))
	for _, source := range sources {
		doc, err := projectNoteSource(source)
		if err != nil {
			return nil, nil, err
		}
		docsByPath[doc.Path] = doc
	}
	return docsByPath, rowsByPath, nil
}

func buildAmbientEdgesForSync(srcPath, srcType string, rows []semdb.GraphDocEdge, structuralPairs map[string]struct{}, resolveType func(string) string, schema *Schema, updatedAt int64) []semdb.OntologyEdgeRow {
	if strings.TrimSpace(srcType) == "" || len(rows) == 0 {
		return nil
	}
	out := make([]semdb.OntologyEdgeRow, 0, len(rows))
	for _, row := range rows {
		var targetPath, provenance string
		var bodyLink bool
		switch {
		case row.SrcPath == srcPath && row.DstPath != srcPath:
			targetPath, provenance, bodyLink = row.DstPath, "body_link", true
		case row.DstPath == srcPath && row.SrcPath != srcPath:
			targetPath, provenance = row.SrcPath, "backlink"
		default:
			continue
		}
		dstType := resolveType(targetPath)
		if strings.TrimSpace(dstType) == "" {
			continue
		}
		if _, ok := structuralPairs[srcPath+"\x00"+targetPath]; ok {
			continue
		}
		out = append(out, semdb.OntologyEdgeRow{
			SrcPath:      srcPath,
			RelationName: ambientRelationName(schema, srcType, dstType, bodyLink),
			DstPath:      targetPath,
			DstType:      dstType,
			Provenance:   provenance,
			Structural:   false,
			SchemaHash:   schema.Hash,
			UpdatedAt:    updatedAt,
		})
	}
	return dedupeEdges(out)
}

func groupIncidentGraphRows(rows []semdb.GraphDocEdge) map[string][]semdb.GraphDocEdge {
	out := make(map[string][]semdb.GraphDocEdge, len(rows))
	for _, row := range rows {
		out[row.SrcPath] = append(out[row.SrcPath], row)
		if row.DstPath != row.SrcPath {
			out[row.DstPath] = append(out[row.DstPath], row)
		}
	}
	return out
}

func ontologyFingerprint(doc *noteDoc, meta semdb.NoteMetadataRow, schemaHash string, incidentRows []semdb.GraphDocEdge) string {
	hasher := sha256.New()
	writeFingerprintPart := func(parts ...string) {
		for _, part := range parts {
			_, _ = hasher.Write([]byte(part))
			_, _ = hasher.Write([]byte{0})
		}
	}
	writeFingerprintPart(schemaHash, doc.Path, meta.ContentHash, doc.TypeName)
	keys := make([]string, 0, len(doc.Frontmatter))
	for key := range doc.Frontmatter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		writeFingerprintPart("fm", key, canonicalValueString(doc.Frontmatter[key]))
	}
	inlineKeys := make([]string, 0, len(doc.Inline))
	for key := range doc.Inline {
		inlineKeys = append(inlineKeys, key)
	}
	sort.Strings(inlineKeys)
	for _, key := range inlineKeys {
		values := append([]string(nil), doc.Inline[key]...)
		sort.Strings(values)
		for _, value := range values {
			writeFingerprintPart("inline", key, value)
		}
	}
	tags := append([]string(nil), doc.Tags...)
	sort.Strings(tags)
	for _, tag := range tags {
		writeFingerprintPart("tag", tag)
	}
	graphParts := make([]string, 0, len(incidentRows))
	for _, row := range incidentRows {
		graphParts = append(graphParts, strings.Join([]string{row.SrcPath, row.DstPath, row.Kind}, "\x1f"))
	}
	sort.Strings(graphParts)
	for _, part := range graphParts {
		writeFingerprintPart("graph", part)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func canonicalValueString(v interface{}) string {
	switch value := v.(type) {
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, canonicalValueString(item))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []string:
		parts := append([]string(nil), value...)
		sort.Strings(parts)
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
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
