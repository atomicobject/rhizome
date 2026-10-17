package ontology

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"sort"
	"strings"
	"time"
)

type BuildResult struct {
	Schema           *ontoschema.Schema
	NotesHash        string
	Assessments      []NoteAssessment
	AssessmentRows   []semdb.OntologyNoteAssessmentRow
	NoteStates       []semdb.OntologyNoteStateRow
	NoteTypes        []semdb.OntologyNoteTypeRow
	Edges            []semdb.OntologyEdgeRow
	TypePolicies     []semdb.OntologyTypePolicyRow
	ValidationIssues []ValidationIssue
	NodePaths        []string
	Nodes            []readmodel.NodeRow
	NodeFieldValues  []readmodel.FieldValueRow
	NodeLinkDeps     []readmodel.LinkDependencyRow
}

// BuildIndexFromNoteSources builds an in-memory ontology read model from the
// canonical raw-note contract. It is the pure counterpart to the store-assisted
// production loader used by BuildIndexWithStore.
func BuildIndexFromNoteSources(ctx context.Context, vaultDef obsidian.VaultDefinition, sources []notemeta.NoteSourceSnapshot, schema *ontoschema.Schema, notesHash string) (*BuildResult, error) {
	if schema == nil {
		return &BuildResult{NotesHash: notesHash}, nil
	}
	docs := make([]*noteDoc, 0, len(sources))
	allNotes := make([]string, 0, len(sources))
	aliases := make(map[string][]string, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, err := projectNoteSource(source)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
		allNotes = append(allNotes, doc.Path)
		aliases[doc.Path] = append([]string(nil), source.Aliases...)
	}
	return buildIndexFromDocs(ctx, nil, schema, notesHash, docs, allNotes, aliases)
}

// BuildIndexWithStore projects typed notes into validation, relation, policy,
// and node-catalog rows.
//
// The expensive work is intentionally front-loaded into one pass over typed
// docs: resolve note types, validate fields, collect structural/ambient edges,
// then build the ontology-node catalog used by noderead, graph, search, and
// semantic indexing.
func BuildIndexWithStore(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, schema *ontoschema.Schema, notesHash string) (*BuildResult, error) {
	if schema == nil {
		return &BuildResult{NotesHash: notesHash}, nil
	}
	if store == nil {
		return nil, fmt.Errorf("ontology metadata store is required")
	}
	if err := noteMetadata.Validate(); err != nil {
		return nil, err
	}
	docs, allNotes, err := loadNoteDocs(ctx, noteMetadata, vaultDef, noteMgr, store)
	if err != nil {
		return nil, err
	}
	aliases, err := notemeta.LoadAliasMap(ctx, store)
	if err != nil {
		return nil, err
	}
	return buildIndexFromDocs(ctx, store, schema, notesHash, docs, allNotes, aliases)
}

func buildIndexFromDocs(ctx context.Context, store *semdb.Store, schema *ontoschema.Schema, notesHash string, docs []*noteDoc, allNotes []string, aliases map[string][]string) (*BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	metaByPath := map[string]semdb.NoteMetadataRow{}
	var graphRows []semdb.GraphDocEdge
	if store != nil {
		rows, err := store.CurrentNoteMetadataRows(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			metaByPath[row.Path] = row
		}
		graphRows, err = store.GraphDocNoteEdgesForPaths(ctx, allNotes)
		if err != nil {
			return nil, err
		}
	} else {
		for _, doc := range docs {
			for _, link := range doc.Links {
				if target := link.TargetPath.String(); target != "" {
					graphRows = append(graphRows, semdb.GraphDocEdge{SrcPath: doc.Path, DstPath: target})
				}
			}
		}
	}
	docsByPath := make(map[string]*noteDoc, len(docs))
	plan := &syncPlan{ForcePaths: map[string]struct{}{}}
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		docsByPath[doc.Path] = doc
		plan.LoadPaths = append(plan.LoadPaths, doc.Path)
		plan.ForcePaths[doc.Path] = struct{}{}
	}
	sort.Strings(plan.LoadPaths)
	graphByPath := groupIncidentGraphRows(graphRows)
	jobs, replacePaths, _, err := prepareOntologyJobs(plan, schema, docsByPath, metaByPath, nil, graphByPath, OntologyMaterializationVersion)
	if err != nil {
		return nil, err
	}
	cache := obsidian.BuildNotePathCacheWithAliases(allNotes, aliases)
	results, err := computeOntologyJobs(ctx, jobs, schema, cache, graphByPath, nil)
	if err != nil {
		return nil, err
	}
	readModel, err := buildOntologyNodeCatalogReadModel(schema, results, ontologyLinkTargetResolver(cache, ontologyTypeResolverFromSyncResults(results, nil)))
	if err != nil {
		return nil, err
	}
	catalog := catalogNodeWitnesses(readModel.Nodes)
	fieldCatalog := catalogFieldWitnesses(readModel.FieldValues)
	for _, result := range results {
		witness := catalogWitnessForPath(catalog, result.path)
		result.assessment.CatalogNodeCount = &witness.Count
		result.assessment.CatalogNodeDigest = witness.Digest
		fieldWitness := catalogWitnessForPath(fieldCatalog, result.path)
		result.assessment.CatalogFieldCount = &fieldWitness.Count
		result.assessment.CatalogFieldDigest = fieldWitness.Digest
		result.assessment.SourceContentHash = metaByPath[result.path].ContentHash
	}
	setOntologyOutputWitnesses(results, readModel.LinkDependencies)
	delta, issues, _ := buildOntologyDelta(schema, semdb.NoteMetadataState{NotesHash: notesHash, LoadedAt: time.Now().Unix()}, plan.LoadPaths, nil, replacePaths, results, nil, true)
	build := &BuildResult{
		Schema: schema, NotesHash: notesHash, AssessmentRows: delta.Assessments,
		NoteStates: delta.NoteStates, NoteTypes: delta.NoteTypes, Edges: delta.Edges,
		TypePolicies: delta.TypePolicies, ValidationIssues: issues,
		NodePaths: readModel.NotePaths, Nodes: readModel.Nodes,
		NodeFieldValues: readModel.FieldValues, NodeLinkDeps: readModel.LinkDependencies,
	}
	for _, row := range delta.Assessments {
		assessment, err := AssessmentFromJSON(row.AssessmentJSON)
		if err != nil {
			return nil, err
		}
		build.Assessments = append(build.Assessments, *assessment)
	}
	return build, nil
}

func docMatchesAnyGlobalSource(doc *noteDoc, schema *ontoschema.Schema) bool {
	if doc == nil || doc.Snapshot == nil {
		return false
	}
	for _, noteType := range schema.Types {
		if noteType == nil || noteType.Role != ontoschema.TypeRoleEmbeddedNode || noteType.SourceShape == "" {
			continue
		}
		if sourceMatchersMatchNote(noteType.SourceMatchers, doc) {
			return true
		}
	}
	return false
}

func buildTypePolicyRows(schema *ontoschema.Schema, now int64) []semdb.OntologyTypePolicyRow {
	policies := ontoschema.CompileTypePolicies(schema)
	if len(policies) == 0 {
		return nil
	}
	out := make([]semdb.OntologyTypePolicyRow, 0, len(policies))
	for _, policy := range policies {
		encoded := ontoschema.MarshalTypePolicy(policy)
		if encoded == "" {
			continue
		}
		out = append(out, semdb.OntologyTypePolicyRow{
			TypeName:   policy.TypeName,
			PolicyJSON: encoded,
			SchemaHash: schema.Hash,
			UpdatedAt:  now,
		})
	}
	return out
}

func loadNoteDocs(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store) ([]*noteDoc, []string, error) {
	sources, err := noteMetadata.LoadNoteSourceSnapshots(ctx, vaultDef, noteMgr, store, nil)
	if err != nil {
		return nil, nil, err
	}
	docs := make([]*noteDoc, 0, len(sources))
	allNotes := make([]string, 0, len(sources))
	for _, source := range sources {
		doc, err := projectNoteSource(source)
		if err != nil {
			return nil, nil, err
		}
		docs = append(docs, doc)
		allNotes = append(allNotes, doc.Path)
	}
	return docs, allNotes, nil
}

func ambientRelationName(schema *ontoschema.Schema, srcTypeName, dstTypeName string, bodyLink bool) string {
	if schema == nil || strings.TrimSpace(srcTypeName) == "" || strings.TrimSpace(dstTypeName) == "" {
		return "related"
	}
	srcType := schema.Types[srcTypeName]
	if srcType == nil {
		return "related"
	}
	matches := make([]string, 0, 1)
	for _, field := range srcType.Fields {
		switch field.Kind {
		case ontoschema.FieldKindLink:
			if bodyLink && !field.IncludeBodyLinks {
				continue
			}
			if !bodyLink && !field.IncludeBacklinks {
				continue
			}
			if !ontoschema.TypeMatchesOrImplements(schema, dstTypeName, field.TypeName) {
				continue
			}
		case ontoschema.FieldKindNeighbor:
			switch field.Direction {
			case ontoschema.NeighborDirectionOutbound:
				if !bodyLink {
					continue
				}
			case ontoschema.NeighborDirectionInbound:
				if bodyLink {
					continue
				}
			case ontoschema.NeighborDirectionBoth:
				// allow either
			}
			if !ontoschema.TypeMatchesOrImplements(schema, dstTypeName, field.TypeName) {
				continue
			}
		default:
			continue
		}
		matches = append(matches, field.Name)
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return "related"
}
