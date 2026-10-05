package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type ValidationIssue struct {
	Code       string `json:"code"`
	NotePath   string `json:"notePath,omitempty"`
	TypeName   string `json:"typeName,omitempty"`
	FieldName  string `json:"fieldName,omitempty"`
	NodeRef    string `json:"nodeRef,omitempty"`
	NodeID     string `json:"nodeId,omitempty"`
	Structural string `json:"structuralFingerprint,omitempty"`
	Line       int    `json:"line,omitempty"`
	LinkKind   string `json:"linkKind,omitempty"`
	LinkTarget string `json:"linkTarget,omitempty"`
	Message    string `json:"message"`
	// FixTarget is the note path that should be edited to resolve this issue.
	// For inverse_mismatch, this is the target note missing the inverse link.
	FixTarget string `json:"fixTarget,omitempty"`
	// FixFieldName is the field on the fix target that should receive the fix.
	// For inverse_mismatch, this is the inverse relation field name.
	FixFieldName string `json:"fixFieldName,omitempty"`
	// VariantKey and VariantLabel name the finding's case within its code
	// (issue_variant.go). Both are empty for codes without cases.
	VariantKey   string `json:"variantKey,omitempty"`
	VariantLabel string `json:"variantLabel,omitempty"`
	// CandidateTypes lists the sorted types a type_ambiguous note matches.
	CandidateTypes []string `json:"candidateTypes,omitempty"`
}

type BuildResult struct {
	Schema           *Schema
	NotesHash        string
	Assessments      []NoteAssessment
	AssessmentRows   []semdb.OntologyNoteAssessmentRow
	NoteStates       []semdb.OntologyNoteStateRow
	NoteTypes        []semdb.OntologyNoteTypeRow
	Edges            []semdb.OntologyEdgeRow
	TypePolicies     []semdb.OntologyTypePolicyRow
	ValidationIssues []ValidationIssue
	NodePaths        []string
	Nodes            []codeanchor.IntelOntologyNode
	NodeFieldValues  []codeanchor.IntelOntologyNodeFieldValue
	NodeLinkDeps     []codeanchor.IntelOntologyNodeLinkDependency
}

type EnsureResult struct {
	Schema    *Schema
	NotesHash string
	Dirty     bool
	Issues    []ValidationIssue
}

type WalkOptions struct {
	MaxDepth       int
	RelationFilter string
	IncludeAmbient bool
}

type WalkNode struct {
	Path        string
	TypeName    string
	Title       string
	Frontmatter map[string]interface{}
}

type WalkEdge struct {
	RelationName string
	Source       string
	Destination  string
	Provenance   string
	Structural   bool
}

type WalkResult struct {
	SeedType string
	Nodes    []WalkNode
	Edges    []WalkEdge
}

type noteDoc struct {
	Path        string
	Title       string
	Content     string
	Frontmatter map[string]interface{}
	Inline      map[string][]string
	Tags        []string
	Links       []notemeta.ResolvedNoteLink
	Projection  noteformat.Projection
	TypeName    string
	Root        *RootDocumentSnapshot
	Snapshot    *DocumentSnapshot
}

type noteSourceReader struct {
	byPath map[string]notemeta.NoteSourceSnapshot
	paths  []string
}

func newNoteSourceReader(sources []notemeta.NoteSourceSnapshot) *noteSourceReader {
	r := &noteSourceReader{byPath: make(map[string]notemeta.NoteSourceSnapshot, len(sources))}
	for _, source := range sources {
		path := source.NotePathString()
		if strings.TrimSpace(path) == "" {
			continue
		}
		r.byPath[path] = source
		r.paths = append(r.paths, path)
	}
	sort.Strings(r.paths)
	return r
}

func (r *noteSourceReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	source, ok := r.byPath[path]
	if !ok {
		return "", fmt.Errorf("note not found: %s", path)
	}
	return source.Content, nil
}

func (r *noteSourceReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return append([]string(nil), r.paths...), nil
}

func (r *noteSourceReader) GetModTime(_ obsidian.VaultDefinition, path string) (time.Time, error) {
	source, ok := r.byPath[path]
	if !ok {
		return time.Time{}, fmt.Errorf("note not found: %s", path)
	}
	if source.Mtime <= 0 {
		return time.Time{}, nil
	}
	return time.Unix(source.Mtime, 0), nil
}

func (r *noteSourceReader) Title(path string) (string, bool) {
	source, ok := r.byPath[path]
	if !ok {
		return "", false
	}
	return source.Title, strings.TrimSpace(source.Title) != ""
}

// projectNoteSource is the provider-neutral raw-note -> ontology root seam.
// Root-only formats remain one file-backed node over provider-owned facts.
func projectNoteSource(source notemeta.NoteSourceSnapshot) (*noteDoc, error) {
	if source.Format == "" {
		// Compatibility for direct in-memory callers that predate explicit
		// provider identity. Durable rows never omit Format.
		source.Format = noteformat.FormatID("markdown")
		return projectMarkdownNoteSource(source)
	}
	if source.Projection.Capabilities.Has(noteformat.CapabilityStructuralContentMutation) {
		return projectMarkdownNoteSource(source)
	}
	return projectRootNoteSource(source)
}

func projectRootNoteSource(source notemeta.NoteSourceSnapshot) (*noteDoc, error) {
	path := source.NotePathString()
	root, err := BuildRootDocumentSnapshot(source)
	if err != nil {
		return nil, err
	}
	fm := cloneAnyMap(source.Frontmatter)
	if fm == nil {
		fm = map[string]any{}
	}
	return &noteDoc{
		Path:        path,
		Title:       firstNonEmpty(strings.TrimSpace(source.Title), titleFromNotePath(path)),
		Content:     string(root.RawSource),
		Frontmatter: fm,
		Inline:      cloneNoteInline(source.InlineProps),
		Tags:        append([]string(nil), source.Tags...),
		Links:       append([]notemeta.ResolvedNoteLink(nil), source.Links...),
		Projection:  source.Projection.Copy(),
		TypeName:    stringValue(fm[typeFieldName]),
		Root:        root,
	}, nil
}

func projectMarkdownNoteSource(source notemeta.NoteSourceSnapshot) (*noteDoc, error) {
	if source.Format != noteformat.FormatID("markdown") {
		return nil, fmt.Errorf("ontology Markdown projection requires Markdown source, got %q", source.Format)
	}
	doc, err := projectRootNoteSource(source)
	if err != nil {
		return nil, err
	}
	modTime := time.Time{}
	if source.Mtime > 0 {
		modTime = time.Unix(source.Mtime, 0)
	}
	snapshot, err := BuildDocumentSnapshot(doc.Path, string(doc.Root.RawSource), modTime)
	if err != nil {
		return nil, err
	}
	doc.Root.MarkdownStructure = snapshot
	doc.Snapshot = snapshot
	return doc, nil
}

func cloneNoteInline(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func documentSnapshotForDoc(doc *noteDoc) (*DocumentSnapshot, error) {
	if doc == nil {
		return nil, fmt.Errorf("note document is required")
	}
	if doc.Snapshot != nil {
		return doc.Snapshot, nil
	}
	return BuildDocumentSnapshot(doc.Path, doc.Content, time.Time{})
}

// EnsureIndexed brings ontology read-model tables up to date for the current
// schema hash and note metadata hash.
//
// This is the full-snapshot convergence path. Incremental index runs should
// prefer SyncPaths so changed/deleted notes and their relation neighbors can be
// scoped without rebuilding every ontology row.
func EnsureIndexed(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store) (*EnsureResult, error) {
	if store == nil {
		return nil, nil
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

	schema, schemaErr := LoadSchema(vaultDef.BasePath())
	if schemaErr != nil {
		if schemaErr == ErrNoOntologyFiles {
			if err := store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{}); err != nil {
				return nil, err
			}
			return &EnsureResult{}, nil
		}
		state := semdb.OntologySchemaState{
			SchemaHash:             "",
			NotesHash:              "",
			MaterializationVersion: OntologyMaterializationVersion,
			LoadedAt:               time.Now().Unix(),
			Ready:                  false,
			ErrorJSON:              mustJSON([]ValidationIssue{{Code: "schema_invalid", Message: schemaErr.Error()}}),
		}
		if err := store.ReplaceOntologySnapshot(ctx, semdb.OntologySnapshot{SchemaState: state}); err != nil {
			return nil, err
		}
		return nil, schemaErr
	}
	if _, err := noteMetadata.EnsureIndexed(ctx, vaultDef, noteMgr, store); err != nil {
		return nil, err
	}
	noteState, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, err
	}
	if state.Ready && state.SchemaHash == schema.Hash && state.NotesHash == noteState.NotesHash && state.MaterializationVersion == OntologyMaterializationVersion {
		materialized, err := ontologyStateMaterialized(ctx, noteMetadata, store, schema, state.ErrorJSON)
		if err != nil {
			return nil, err
		}
		if materialized {
			return &EnsureResult{
				Schema:    schema,
				NotesHash: noteState.NotesHash,
				Dirty:     false,
				Issues:    decodeIssues(state.ErrorJSON),
			}, nil
		}
	}

	build, err := BuildIndexWithStore(ctx, noteMetadata, vaultDef, noteMgr, store, schema, noteState.NotesHash)
	if err != nil {
		return nil, err
	}
	snapshot := semdb.OntologySnapshot{
		Assessments:  build.AssessmentRows,
		NoteStates:   build.NoteStates,
		NoteTypes:    build.NoteTypes,
		Edges:        build.Edges,
		TypePolicies: build.TypePolicies,
		SchemaState: semdb.OntologySchemaState{
			SchemaHash:             schema.Hash,
			NotesHash:              build.NotesHash,
			MaterializationVersion: OntologyMaterializationVersion,
			LoadedAt:               noteState.LoadedAt,
			Ready:                  true,
			ErrorJSON:              mustJSON(build.ValidationIssues),
		},
	}
	if err := store.ApplyOntologyDelta(ctx, semdb.OntologyDelta{
		FullRebuild: true,
		Assessments: snapshot.Assessments, NoteStates: snapshot.NoteStates,
		NoteTypes: snapshot.NoteTypes, Edges: snapshot.Edges,
		TypePolicies: snapshot.TypePolicies, SchemaState: &snapshot.SchemaState,
		ReadModels: []codeanchor.IntelOntologyNodeReadModel{{
			FullReplace: true, NotePaths: build.NodePaths, Nodes: build.Nodes,
			FieldValues: build.NodeFieldValues, LinkDependencies: build.NodeLinkDeps,
		}},
	}); err != nil {
		return nil, err
	}

	return &EnsureResult{
		Schema:    schema,
		NotesHash: build.NotesHash,
		Dirty:     true,
		Issues:    build.ValidationIssues,
	}, nil
}

func ontologyStateMaterialized(ctx context.Context, noteMetadata notemeta.Indexer, store *semdb.Store, schema *Schema, stateIssuesJSON string) (bool, error) {
	if schema == nil {
		return true, nil
	}
	schemaHash := schema.Hash
	paths, err := ProjectableMetadataPaths(ctx, noteMetadata, store)
	if err != nil {
		return false, err
	}
	if len(paths) == 0 {
		return true, nil
	}
	states, err := store.OntologyNoteStatesByPaths(ctx, paths)
	if err != nil {
		return false, err
	}
	if len(states) != len(paths) {
		return false, nil
	}
	assessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	if err != nil {
		return false, err
	}
	if len(assessments) != len(paths) {
		return false, nil
	}
	assessmentIssues := make([]ValidationIssue, 0)
	expectedCatalog := make(map[string]catalogWitness, len(paths))
	for _, path := range paths {
		assessment, ok := assessments[path]
		if !ok || strings.TrimSpace(assessment.SchemaHash) != schemaHash || strings.TrimSpace(assessment.AssessmentJSON) == "" {
			return false, nil
		}
		decoded, err := AssessmentFromJSON(assessment.AssessmentJSON)
		if err != nil || decoded == nil || decoded.NotePath != path || decoded.CatalogNodeCount == nil || *decoded.CatalogNodeCount < 0 || decoded.CatalogNodeDigest == "" {
			return false, nil
		}
		expectedCatalog[path] = catalogWitness{Count: *decoded.CatalogNodeCount, Digest: decoded.CatalogNodeDigest}
		assessmentIssues = append(assessmentIssues, decoded.Issues...)
	}
	var stateIssues []ValidationIssue
	if strings.TrimSpace(stateIssuesJSON) != "" {
		if err := json.Unmarshal([]byte(stateIssuesJSON), &stateIssues); err != nil {
			return false, nil
		}
	}
	if !equalValidationIssues(assessmentIssues, stateIssues) {
		return false, nil
	}
	nodes, err := store.OntologyNodesByPaths(ctx, paths)
	if err != nil {
		return false, err
	}
	catalog := catalogNodeWitnesses(nodes)
	nodesByPath := make(map[string][]codeanchor.IntelOntologyNode, len(paths))
	nodeByID := make(map[string]codeanchor.IntelOntologyNode, len(nodes))
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.SchemaHash) != schemaHash {
			return false, nil
		}
		nodesByPath[node.NotePath] = append(nodesByPath[node.NotePath], node)
		if strings.TrimSpace(node.NodeID) != "" {
			nodeByID[node.NodeID] = node
			nodeIDs = append(nodeIDs, node.NodeID)
		}
	}
	fieldRowsByNode := map[string]map[string]struct{}{}
	if len(nodeIDs) > 0 {
		fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
		if err != nil {
			return false, err
		}
		for _, field := range fields {
			node, ok := nodeByID[field.NodeID]
			if !ok || strings.TrimSpace(field.SchemaHash) != schemaHash || field.UpdatedAt < node.UpdatedAt {
				continue
			}
			if fieldRowsByNode[field.NodeID] == nil {
				fieldRowsByNode[field.NodeID] = map[string]struct{}{}
			}
			fieldRowsByNode[field.NodeID][strings.ToLower(strings.TrimSpace(field.FieldName))] = struct{}{}
		}
	}
	for _, path := range paths {
		state, ok := states[path]
		if !ok {
			return false, nil
		}
		if strings.TrimSpace(state.SchemaHash) != schemaHash {
			return false, nil
		}
		if strings.TrimSpace(state.InputFingerprint) == "" {
			return false, nil
		}
		pathNodes := nodesByPath[path]
		if assessments[path].UpdatedAt < state.UpdatedAt || catalogWitnessForPath(catalog, path) != expectedCatalog[path] {
			return false, nil
		}
		for _, node := range pathNodes {
			if node.UpdatedAt < state.UpdatedAt {
				return false, nil
			}
			if !ontologyNodeRequiredFieldRowsMaterialized(schema, node, fieldRowsByNode[node.NodeID]) {
				return false, nil
			}
		}
	}
	return true, nil
}

// ProjectableMetadataPaths selects current metadata rows whose registered
// format has an executable projector. Provider facts support the shared root
// read model; only structurally capable providers add child nodes.
func ProjectableMetadataPaths(ctx context.Context, noteMetadata notemeta.Indexer, store *semdb.Store) ([]string, error) {
	formats, err := noteMetadata.FormatRuntime()
	if err != nil {
		return nil, err
	}
	allPaths, err := store.CurrentNoteMetadataPaths(ctx)
	if err != nil {
		return nil, err
	}
	rowsByPath, err := store.CurrentNoteMetadataRowsByPaths(ctx, allPaths)
	if err != nil {
		return nil, err
	}
	projectable := make([]string, 0, len(allPaths))
	for _, notePath := range allPaths {
		row, ok := rowsByPath[notePath]
		if !ok {
			return nil, fmt.Errorf("current note metadata row is missing for %q", notePath)
		}
		eligible, err := ontologyProjectableMetadataRow(formats, row)
		if err != nil {
			return nil, err
		}
		if eligible {
			projectable = append(projectable, notePath)
		}
	}
	return projectable, nil
}

func ontologyProjectableMetadataRow(formats noteformat.Runtime, row semdb.NoteMetadataRow) (bool, error) {
	notePath, err := paths.CleanNotePath(row.Path)
	if err != nil || notePath.String() != row.Path {
		return false, fmt.Errorf("note metadata path %q is not canonical", row.Path)
	}
	provider, selected := formats.ProviderForPath(paths.RelPath(notePath))
	if !selected {
		return false, fmt.Errorf("no note format provider selects metadata path %q", row.Path)
	}
	descriptor := provider.Descriptor()
	if row.FormatID != string(descriptor.ID) {
		return false, fmt.Errorf("note metadata format %q does not match selected format %q for %q", row.FormatID, descriptor.ID, row.Path)
	}
	return formats.CanProject(descriptor.ID) && row.Projection.Status == semdb.NoteProjectionStatusCurrent, nil
}

func ontologyNodeRequiredFieldRowsMaterialized(schema *Schema, node codeanchor.IntelOntologyNode, rows map[string]struct{}) bool {
	noteType := schema.Types[strings.TrimSpace(node.TypeName)]
	if noteType == nil {
		return true
	}
	for _, field := range noteType.Fields {
		if field == nil || !field.Required || !readModelFieldKind(field.Kind) {
			continue
		}
		if _, ok := rows[strings.ToLower(strings.TrimSpace(field.Name))]; !ok {
			return false
		}
	}
	return true
}

func readModelFieldKind(kind FieldKind) bool {
	switch kind {
	case FieldKindScalar, FieldKindEnum, FieldKindLink:
		return true
	default:
		return false
	}
}

// BuildIndexFromNoteSources builds an in-memory ontology read model from the
// canonical raw-note contract. It is the pure counterpart to the store-assisted
// production loader used by BuildIndexWithStore.
func BuildIndexFromNoteSources(ctx context.Context, vaultDef obsidian.VaultDefinition, sources []notemeta.NoteSourceSnapshot, schema *Schema, notesHash string) (*BuildResult, error) {
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
	reader := newNoteSourceReader(sources)
	return buildIndexFromDocs(ctx, vaultDef, reader, nil, schema, notesHash, docs, allNotes, aliases)
}

// BuildIndexWithStore projects typed notes into validation, relation, policy,
// and node-catalog rows.
//
// The expensive work is intentionally front-loaded into one pass over typed
// docs: resolve note types, validate fields, collect structural/ambient edges,
// then build the ontology-node catalog used by noderead, graph, search, and
// semantic indexing.
func BuildIndexWithStore(ctx context.Context, noteMetadata notemeta.Indexer, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, schema *Schema, notesHash string) (*BuildResult, error) {
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
	return buildIndexFromDocs(ctx, vaultDef, noteMgr, store, schema, notesHash, docs, allNotes, notemeta.LoadAliasMap(ctx, store))
}

func buildIndexFromDocs(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, schema *Schema, notesHash string, docs []*noteDoc, allNotes []string, aliases map[string][]string) (*BuildResult, error) {
	cache := obsidian.BuildNotePathCacheWithAliases(allNotes, aliases)

	now := time.Now().Unix()
	issues := make([]ValidationIssue, 0)
	assessments := make([]NoteAssessment, 0, len(docs))
	assessmentRows := make([]semdb.OntologyNoteAssessmentRow, 0, len(docs))
	noteStates := make([]semdb.OntologyNoteStateRow, 0, len(docs))
	noteTypes := make([]semdb.OntologyNoteTypeRow, 0, len(docs))
	edges := make([]semdb.OntologyEdgeRow, 0)
	nodePaths := make([]string, 0, len(docs))
	nodes := make([]codeanchor.IntelOntologyNode, 0, len(docs))
	nodeFieldValues := make([]codeanchor.IntelOntologyNodeFieldValue, 0)
	nodeLinkDeps := make([]codeanchor.IntelOntologyNodeLinkDependency, 0)
	resolvedDocs := make(map[string]*noteDoc)
	assessmentByPath := make(map[string]*NoteAssessment)
	relationByPath := make(map[string]map[string]*RelationAssessment)
	metaByPath := make(map[string]semdb.NoteMetadataRow)
	graphByPath := map[string][]semdb.GraphDocEdge{}
	if store != nil {
		if rows, err := store.CurrentNoteMetadataRows(ctx); err == nil {
			for _, row := range rows {
				metaByPath[row.Path] = row
			}
		}
		if graphRows, err := store.GraphDocNoteEdgesForPaths(ctx, allNotes); err == nil {
			graphByPath = groupIncidentGraphRows(graphRows)
		}
	}

	for _, doc := range docs {
		if doc == nil {
			continue
		}
		nodePaths = append(nodePaths, doc.Path)
		// First pass establishes type identity for every document. Relation
		// assessment below depends on a complete resolvedDocs map so link targets
		// can be typed without per-field rediscovery.
		assessment := resolveNoteAssessment(doc, schema)
		resolvedType := ""
		if assessment != nil {
			resolvedType = assessment.ResolvedType
		}
		noteStates = append(noteStates, semdb.OntologyNoteStateRow{
			NotePath:         doc.Path,
			InputFingerprint: ontologyFingerprint(doc, metaByPath[doc.Path], schema.Hash, graphByPath[doc.Path]),
			SchemaHash:       schema.Hash,
			ResolvedType:     resolvedType,
			UpdatedAt:        now,
		})
		if assessment == nil {
			assessment = &NoteAssessment{NotePath: doc.Path}
		}
		assessmentByPath[doc.Path] = assessment
		issues = append(issues, assessment.Issues...)
		if assessment.ResolvedType != "" {
			resolvedDoc := *doc
			resolvedDoc.TypeName = assessment.ResolvedType
			resolvedDocs[doc.Path] = &resolvedDoc
			noteTypes = append(noteTypes, semdb.OntologyNoteTypeRow{
				NotePath:   doc.Path,
				TypeName:   assessment.ResolvedType,
				SchemaHash: schema.Hash,
				UpdatedAt:  now,
			})
		}
	}

	resolveType := func(path string) string {
		if doc := resolvedDocs[path]; doc != nil {
			return doc.TypeName
		}
		return ""
	}
	for path, doc := range resolvedDocs {
		assessment := assessmentByPath[path]
		noteType := schema.Types[doc.TypeName]
		if assessment == nil || noteType == nil {
			continue
		}
		fieldAssessments := make([]FieldAssessment, 0, len(noteType.Fields))
		relationAssessments := make([]RelationAssessment, 0, len(noteType.Fields))
		relationByName := make(map[string]*RelationAssessment)
		for _, field := range noteType.Fields {
			switch field.Kind {
			case FieldKindLink:
				fieldAssessment, relationAssessment, fieldEdges, fieldIssues := assessLinkField(doc, field, cache, resolveType, schema, now)
				fieldAssessments = append(fieldAssessments, fieldAssessment)
				relationAssessments = append(relationAssessments, relationAssessment)
				relationByName[field.Name] = &relationAssessments[len(relationAssessments)-1]
				issues = append(issues, fieldIssues...)
				assessment.Issues = append(assessment.Issues, fieldIssues...)
				edges = append(edges, fieldEdges...)
			case FieldKindNeighbor, FieldKindReverse:
				fieldAssessments = append(fieldAssessments, emptyFieldAssessment(field))
				relationAssessments = append(relationAssessments, emptyRelationAssessment(field))
				relationByName[field.Name] = &relationAssessments[len(relationAssessments)-1]
			case FieldKindSection:
				fieldAssessment, fieldIssues := assessSectionField(doc, noteType, field, schema)
				fieldAssessments = append(fieldAssessments, fieldAssessment)
				issues = append(issues, fieldIssues...)
				assessment.Issues = append(assessment.Issues, fieldIssues...)
			default:
				fieldAssessment, fieldIssues := assessScalarField(doc, noteType, field, schema)
				fieldAssessments = append(fieldAssessments, fieldAssessment)
				issues = append(issues, fieldIssues...)
				assessment.Issues = append(assessment.Issues, fieldIssues...)
			}
		}
		assessment.Fields = fieldAssessments
		assessment.Relations = relationAssessments
		customIssues := semanticValidationIssues(doc, noteType, assessment, schema)
		issues = append(issues, customIssues...)
		assessment.Issues = append(assessment.Issues, customIssues...)
		blockIDIssues := validationIssuesFromBlockIDs(doc, schema)
		issues = append(issues, blockIDIssues...)
		assessment.Issues = append(assessment.Issues, blockIDIssues...)
		relationByPath[path] = relationByName
	}
	linkResolver := ontologyLinkTargetResolver(cache, resolveType)
	nodeSourceDocs := docsWithCatalogRows(docs, resolvedDocs)
	for _, doc := range nodeSourceDocs {
		assessment := assessmentByPath[doc.Path]
		// Build node-scoped edges and catalog rows from the same parsed snapshot.
		// These are independent read models, but both must preserve NodeRef
		// identity for embedded/section endpoints.
		nodeEdges, err := buildNodeScopedStructuralEdges(doc, schema, cache, resolveType, now)
		if err != nil {
			return nil, err
		}
		edges = append(edges, nodeEdges...)
		projection, err := projectRootNodeForDoc(doc, schema)
		if err != nil {
			return nil, err
		}
		readModel, err := BuildIntelOntologyNodeReadModelWithOptions(schema, projection, now, BuildIntelOntologyNodeReadModelOptions{
			LinkResolver: linkResolver,
		})
		if err != nil {
			return nil, err
		}
		nodePaths = append(nodePaths, readModel.NotePaths...)
		nodes = append(nodes, readModel.Nodes...)
		nodeFieldValues = append(nodeFieldValues, readModel.FieldValues...)
		nodeLinkDeps = append(nodeLinkDeps, readModel.LinkDependencies...)
		if doc.Snapshot != nil {
			embeddedIssues := validateEmbeddedProjectionFields(doc.Snapshot, schema, projection)
			issues = append(issues, embeddedIssues...)
			if assessment != nil {
				assessment.Issues = append(assessment.Issues, embeddedIssues...)
			}
		}
	}
	frozenStoryResults := make([]noteSyncResult, 0, len(noteTypes))
	for _, row := range noteTypes {
		row := row
		frozenStoryResults = append(frozenStoryResults, noteSyncResult{
			path:     row.NotePath,
			noteType: &row,
		})
	}
	frozenStoryResults = appendFrozenStoryScopeEdges(schema, resolvedDocs, frozenStoryResults, nodes)
	for _, result := range frozenStoryResults {
		edges = append(edges, result.edges...)
	}

	inverseIssues := validateInverses(edges, resolvedDocs, schema)
	issues = append(issues, inverseIssues...)
	appendIssuesToAssessments(assessmentByPath, inverseIssues)

	// Ambient edges come after structural edges so dedupe can keep typed
	// structural relations authoritative over body-link evidence.
	edges = append(edges, buildAmbientEdges(store, resolvedDocs, schema, edges)...)
	edges = dedupeEdges(edges)
	populateRelationAssessments(assessmentByPath, relationByPath, edges)
	sort.Slice(edges, func(i, j int) bool {
		a := edges[i]
		b := edges[j]
		switch {
		case a.SrcPath != b.SrcPath:
			return a.SrcPath < b.SrcPath
		case a.SrcNodeID != b.SrcNodeID:
			return a.SrcNodeID < b.SrcNodeID
		case a.RelationName != b.RelationName:
			return a.RelationName < b.RelationName
		case a.DstPath != b.DstPath:
			return a.DstPath < b.DstPath
		case a.DstNodeID != b.DstNodeID:
			return a.DstNodeID < b.DstNodeID
		default:
			return a.Provenance < b.Provenance
		}
	})

	catalog := catalogNodeWitnesses(nodes)
	for _, assessment := range assessmentByPath {
		witness := catalogWitnessForPath(catalog, assessment.NotePath)
		assessment.CatalogNodeCount = &witness.Count
		assessment.CatalogNodeDigest = witness.Digest
		assessment.SourceContentHash = metaByPath[assessment.NotePath].ContentHash
		sort.SliceStable(assessment.Issues, func(i, j int) bool {
			if assessment.Issues[i].Code != assessment.Issues[j].Code {
				return assessment.Issues[i].Code < assessment.Issues[j].Code
			}
			return assessment.Issues[i].FieldName < assessment.Issues[j].FieldName
		})
		assessments = append(assessments, *assessment)
		row := semdb.OntologyNoteAssessmentRow{
			NotePath:     assessment.NotePath,
			DeclaredType: assessment.DeclaredType,
			ResolvedType: assessment.ResolvedType,
			SchemaHash:   schema.Hash,
			UpdatedAt:    now,
		}
		setAssessmentRow(&row, *assessment)
		assessmentRows = append(assessmentRows, row)
	}
	sort.Slice(assessments, func(i, j int) bool { return assessments[i].NotePath < assessments[j].NotePath })
	sort.Slice(assessmentRows, func(i, j int) bool { return assessmentRows[i].NotePath < assessmentRows[j].NotePath })
	sort.Slice(noteStates, func(i, j int) bool { return noteStates[i].NotePath < noteStates[j].NotePath })

	return &BuildResult{
		Schema:           schema,
		NotesHash:        notesHash,
		Assessments:      assessments,
		AssessmentRows:   assessmentRows,
		NoteStates:       noteStates,
		NoteTypes:        noteTypes,
		Edges:            edges,
		TypePolicies:     buildTypePolicyRows(schema, now),
		ValidationIssues: issues,
		NodePaths:        uniqueSortedNodeCatalogStrings(nodePaths),
		Nodes:            nodes,
		NodeFieldValues:  nodeFieldValues,
		NodeLinkDeps:     nodeLinkDeps,
	}, nil
}

func validateEmbeddedProjectionFields(snapshot *DocumentSnapshot, schema *Schema, root *NodeProjection) []ValidationIssue {
	if snapshot == nil || schema == nil || root == nil {
		return nil
	}
	// WHY: one resolver per traversal. Constructing a resolver is O(document)
	// (inline-property scan, note assessment, global-source span indexing), so
	// projecting each child through ProjectNodeFromSnapshot made this
	// O(embedded nodes x document).
	resolver, resolverErr := newProjectionResolver(snapshot, schema)
	if resolverErr != nil {
		resolver = nil
	}
	lines := newLineIndex(snapshot.Content)
	seen := map[string]struct{}{}
	var issues []ValidationIssue
	var walk func(*NodeProjection)
	visit := func(ref NodeRef) {
		if resolver == nil {
			return
		}
		if key := ref.String(); key != "" {
			if _, ok := seen[key]; ok {
				return
			}
		}
		if child, err := resolver.projectCurrent(ref); err == nil {
			walk(child)
		}
	}
	walk = func(projection *NodeProjection) {
		if projection == nil {
			return
		}
		key := projection.Ref.String()
		if key != "" {
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
		}
		if projection.Type != nil && projection.Type.Role == TypeRoleEmbeddedNode {
			issues = append(issues, validateTitleConstraint(
				snapshot.NotePath,
				projection.Type,
				projection.Ref.String(),
				projectionTitle(snapshot, projection.Ref),
				projection.Ref.Structural,
			)...)
			for _, field := range projection.Type.Fields {
				if field == nil || (field.Kind != FieldKindScalar && field.Kind != FieldKindEnum) {
					continue
				}
				binding := projection.Fields[field.Name]
				issues = append(issues, validateEmbeddedScalarBinding(snapshot, lines, projection, field, binding, schema)...)
			}
		}
		for collectionName, collection := range projection.Collections {
			if projection.Type == nil {
				continue
			}
			field := projection.Type.ByName[collectionName]
			if field == nil || field.Kind != FieldKindSection {
				continue
			}
			issues = append(issues, validateCollectionCardinality(snapshot, lines, projection, field, collection)...)
		}
		for _, binding := range projection.Fields {
			for _, ref := range binding.SectionNodes {
				visit(ref)
			}
		}
		for _, collection := range projection.Collections {
			for _, item := range collection.Items {
				visit(item.Ref)
			}
		}
	}
	walk(root)
	if resolver != nil {
		for _, ref := range resolver.globalSourceNodeRefs() {
			visit(ref)
		}
	}
	return issues
}

func projectionTitle(snapshot *DocumentSnapshot, ref NodeRef) string {
	if snapshot == nil {
		return ""
	}
	if section := snapshot.SectionsByID[ref.NodeID]; section != nil {
		return section.Title
	}
	if span := snapshot.SourceSpansByID[ref.NodeID]; span != nil {
		return span.Title
	}
	return ""
}

func validateCollectionCardinality(snapshot *DocumentSnapshot, lines lineIndex, projection *NodeProjection, field *Field, collection CollectionBinding) []ValidationIssue {
	if snapshot == nil || projection == nil || field == nil {
		return nil
	}
	count := len(collection.Items)
	if field.ContainsMin > 0 && count < field.ContainsMin {
		return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
			Code:       "contains_min_not_met",
			NotePath:   snapshot.NotePath,
			TypeName:   projection.ResolvedType,
			FieldName:  field.Name,
			NodeRef:    projection.Ref.String(),
			NodeID:     projection.Ref.NodeID,
			Structural: projection.Ref.Structural,
			Line:       lines.lineAt(projection.Ref.StartByte),
			Message:    fmt.Sprintf("field %s must contain at least %d item(s)", field.Name, field.ContainsMin),
		})}
	}
	if field.ContainsMax >= 0 && count > field.ContainsMax {
		return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
			Code:       "contains_max_exceeded",
			NotePath:   snapshot.NotePath,
			TypeName:   projection.ResolvedType,
			FieldName:  field.Name,
			NodeRef:    projection.Ref.String(),
			NodeID:     projection.Ref.NodeID,
			Structural: projection.Ref.Structural,
			Line:       lines.lineAt(collection.Range.Start),
			Message:    fmt.Sprintf("field %s must contain at most %d item(s)", field.Name, field.ContainsMax),
		})}
	}
	return nil
}

func docsWithCatalogRows(allDocs []*noteDoc, resolvedDocs map[string]*noteDoc) []*noteDoc {
	byPath := make(map[string]*noteDoc, len(resolvedDocs))
	for path, doc := range resolvedDocs {
		byPath[path] = doc
	}
	for _, doc := range allDocs {
		if doc == nil || strings.TrimSpace(doc.Path) == "" || byPath[doc.Path] != nil {
			continue
		}
		// Full rebuilds replace all catalog paths, so every in-scope note needs
		// its fallback row rebuilt here. Incremental sync already emits the same
		// _FallbackNote row for plain notes; keeping full and incremental aligned
		// prevents rebuilds from deleting catalog-visible untyped notes.
		byPath[doc.Path] = doc
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]*noteDoc, 0, len(paths))
	for _, path := range paths {
		out = append(out, byPath[path])
	}
	return out
}

func docMatchesAnyGlobalSource(doc *noteDoc, schema *Schema) bool {
	if doc == nil || doc.Snapshot == nil {
		return false
	}
	for _, noteType := range schema.Types {
		if noteType == nil || noteType.Role != TypeRoleEmbeddedNode || noteType.SourceShape == "" {
			continue
		}
		if sourceMatchersMatchNote(noteType.SourceMatchers, doc) {
			return true
		}
	}
	return false
}

func validateEmbeddedScalarBinding(snapshot *DocumentSnapshot, lines lineIndex, projection *NodeProjection, field *Field, binding FieldBinding, schema *Schema) []ValidationIssue {
	if len(binding.Values) == 0 {
		if field.Required && !fieldAllowsMissingAuthoredValue(field) {
			return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
				Code:       "missing_required_field",
				NotePath:   snapshot.NotePath,
				TypeName:   projection.ResolvedType,
				FieldName:  field.Name,
				NodeRef:    projection.Ref.String(),
				NodeID:     projection.Ref.NodeID,
				Structural: projection.Ref.Structural,
				Line:       lines.lineAt(projection.Ref.StartByte),
				Message:    fmt.Sprintf("required field %s is missing", field.Name),
			})}
		}
		return nil
	}
	if !field.List && len(binding.Values) > 1 {
		return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
			Code:       "field_shape_mismatch",
			NotePath:   snapshot.NotePath,
			TypeName:   projection.ResolvedType,
			FieldName:  field.Name,
			NodeRef:    projection.Ref.String(),
			NodeID:     projection.Ref.NodeID,
			Structural: projection.Ref.Structural,
			Line:       embeddedFieldLine(lines, binding, projection.Ref.StartByte),
			Message:    fmt.Sprintf("field %s expects a single value", field.Name),
		})}
	}
	issues := make([]ValidationIssue, 0)
	issues = append(issues, validateEmbeddedAuthoringStyle(snapshot, lines, projection, field, binding)...)
	for _, raw := range binding.Values {
		if !validateScalarValue(raw, field, schema) {
			issues = append(issues, withTypeFieldVariant(ValidationIssue{
				Code:       "field_type_mismatch",
				NotePath:   snapshot.NotePath,
				TypeName:   projection.ResolvedType,
				FieldName:  field.Name,
				NodeRef:    projection.Ref.String(),
				NodeID:     projection.Ref.NodeID,
				Structural: projection.Ref.Structural,
				Line:       embeddedFieldLine(lines, binding, projection.Ref.StartByte),
				Message:    fmt.Sprintf("field %s value %q does not match %s", field.Name, raw, field.TypeName),
			}))
			continue
		}
		if issue := validateFormattedValue(snapshot.NotePath, projection.ResolvedType, field.Name, projection.Ref.String(), projection.Ref.Structural, raw, field.Format); issue != nil {
			issue.NodeID = projection.Ref.NodeID
			issue.Line = embeddedFieldLine(lines, binding, projection.Ref.StartByte)
			issues = append(issues, *issue)
		}
	}
	return issues
}

func validateEmbeddedAuthoringStyle(snapshot *DocumentSnapshot, lines lineIndex, projection *NodeProjection, field *Field, binding FieldBinding) []ValidationIssue {
	if field == nil || field.AuthoringStyle == "" || field.AuthoringStyle == FieldAuthoringStyleAny {
		return nil
	}
	valid := true
	switch field.AuthoringStyle {
	case FieldAuthoringStyleListMetadata, FieldAuthoringStyleInlineProperty:
		if len(binding.InlineSpans) == 0 {
			valid = false
			break
		}
		for _, span := range binding.InlineSpans {
			if span.AuthoringStyle != field.AuthoringStyle {
				valid = false
				break
			}
		}
	case FieldAuthoringStyleItemText:
		valid = field.SourceKind == FieldSourceItemTitle || field.SourceKind == FieldSourceItemSummary || field.SourceKind == FieldSourceItemDetail
	case FieldAuthoringStyleCheckbox:
		valid = field.SourceKind == FieldSourceCheckbox
	case FieldAuthoringStyleFrontmatter:
		valid = field.SourceKind == FieldSourceFrontmatter
	}
	if valid {
		return nil
	}
	return []ValidationIssue{withTypeFieldVariant(ValidationIssue{
		Code:       "field_authoring_style_mismatch",
		NotePath:   snapshot.NotePath,
		TypeName:   projection.ResolvedType,
		FieldName:  field.Name,
		NodeRef:    projection.Ref.String(),
		NodeID:     projection.Ref.NodeID,
		Structural: projection.Ref.Structural,
		Line:       embeddedFieldLine(lines, binding, projection.Ref.StartByte),
		Message:    fmt.Sprintf("field %s must be authored as %s", field.Name, field.AuthoringStyle),
	})}
}

func embeddedFieldLine(lines lineIndex, binding FieldBinding, fallback int) int {
	if len(binding.ValueRanges) > 0 {
		return lines.lineAt(binding.ValueRanges[0].Start)
	}
	return lines.lineAt(fallback)
}

// WHY: Per SPEC-0023's usage-driven block-id lifecycle, default validation
// reports drift (duplicate, malformed) but not absence. The
// missing_embedded_block_id diagnostic is intentionally dropped here so vault-
// wide sweeps cannot eagerly insert block IDs on un-cited embedded nodes.
// ValidateEmbeddedBlockIDs remains callable for explicit/scoped runs.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us2]]
func validationIssuesFromBlockIDs(doc *noteDoc, schema *Schema) []ValidationIssue {
	if doc == nil || schema == nil {
		return nil
	}
	snapshot, err := documentSnapshotForDoc(doc)
	if err != nil {
		return nil
	}
	blockIssues := ValidateEmbeddedBlockIDs(snapshot, schema)
	lines := newLineIndex(snapshot.Content)
	out := make([]ValidationIssue, 0, len(blockIssues))
	for _, issue := range blockIssues {
		if issue.Code == "missing_embedded_block_id" {
			continue
		}
		out = append(out, ValidationIssue{
			Code:       issue.Code,
			NotePath:   issue.NotePath,
			TypeName:   issue.TypeName,
			NodeRef:    issue.Ref.String(),
			NodeID:     issue.Ref.NodeID,
			Structural: issue.Ref.Structural,
			Line:       lines.lineAt(issue.Ref.StartByte),
			LinkKind:   "block",
			LinkTarget: issue.BlockID,
			Message:    issue.Message,
			FixTarget:  issue.NotePath,
		})
	}
	return out
}

func buildTypePolicyRows(schema *Schema, now int64) []semdb.OntologyTypePolicyRow {
	policies := CompileTypePolicies(schema)
	if len(policies) == 0 {
		return nil
	}
	out := make([]semdb.OntologyTypePolicyRow, 0, len(policies))
	for _, policy := range policies {
		encoded := MarshalTypePolicy(policy)
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

func validateInverses(edges []semdb.OntologyEdgeRow, typedDocs map[string]*noteDoc, schema *Schema) []ValidationIssue {
	seen := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		key := edge.SrcPath + "\x00" + edge.RelationName + "\x00" + edge.DstPath
		seen[key] = struct{}{}
	}

	issues := make([]ValidationIssue, 0)
	for _, edge := range edges {
		if !edge.Structural {
			continue
		}
		srcDoc := typedDocs[edge.SrcPath]
		if srcDoc == nil {
			continue
		}
		srcType := schema.Types[srcDoc.TypeName]
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
			TypeName:     srcDoc.TypeName,
			FieldName:    field.Name,
			Message:      fmt.Sprintf("field %s expects inverse %s on %s", field.Name, field.Inverse, edge.DstPath),
			FixTarget:    edge.DstPath,
			FixFieldName: field.Inverse,
		}))
	}
	return issues
}

func buildAmbientEdges(store *semdb.Store, typedDocs map[string]*noteDoc, schema *Schema, structuralEdges []semdb.OntologyEdgeRow) []semdb.OntologyEdgeRow {
	if store != nil {
		if rows, err := store.GraphDocNoteEdges(context.Background()); err == nil && len(rows) > 0 {
			return buildAmbientEdgesFromRows(rows, typedDocs, schema, structuralEdges)
		}
	}
	out := make([]semdb.OntologyEdgeRow, 0)
	structuralPairs := make(map[string]struct{}, len(structuralEdges))
	for _, edge := range structuralEdges {
		if !edge.Structural {
			continue
		}
		if strings.TrimSpace(edge.SrcNodeID) != "" || strings.TrimSpace(edge.DstNodeID) != "" {
			continue
		}
		structuralPairs[edge.SrcPath+"\x00"+edge.DstPath] = struct{}{}
	}
	for _, doc := range typedDocs {
		seenTargets := make(map[string]struct{})
		for _, link := range doc.Links {
			targetPath := link.TargetPath.String()
			if strings.TrimSpace(targetPath) == "" {
				continue
			}
			if _, duplicate := seenTargets[targetPath]; duplicate {
				continue
			}
			seenTargets[targetPath] = struct{}{}
			targetDoc := typedDocs[targetPath]
			if targetDoc == nil || targetPath == doc.Path {
				continue
			}
			key := doc.Path + "\x00" + targetPath
			if _, ok := structuralPairs[key]; ok {
				continue
			}
			relationName := ambientRelationName(schema, doc.TypeName, targetDoc.TypeName, true)
			out = append(out, semdb.OntologyEdgeRow{
				SrcPath:      doc.Path,
				RelationName: relationName,
				DstPath:      targetPath,
				DstType:      targetDoc.TypeName,
				Provenance:   "body_link",
				Structural:   false,
				SchemaHash:   schema.Hash,
				UpdatedAt:    time.Now().Unix(),
			})
			backlinkRelation := ambientRelationName(schema, targetDoc.TypeName, doc.TypeName, false)
			if _, ok := structuralPairs[targetPath+"\x00"+doc.Path]; ok {
				continue
			}
			out = append(out, semdb.OntologyEdgeRow{
				SrcPath:      targetPath,
				RelationName: backlinkRelation,
				DstPath:      doc.Path,
				DstType:      doc.TypeName,
				Provenance:   "backlink",
				Structural:   false,
				SchemaHash:   schema.Hash,
				UpdatedAt:    time.Now().Unix(),
			})
		}
	}
	return out
}

func buildAmbientEdgesFromRows(rows []semdb.GraphDocEdge, typedDocs map[string]*noteDoc, schema *Schema, structuralEdges []semdb.OntologyEdgeRow) []semdb.OntologyEdgeRow {
	out := make([]semdb.OntologyEdgeRow, 0)
	structuralPairs := make(map[string]struct{}, len(structuralEdges))
	for _, edge := range structuralEdges {
		if !edge.Structural {
			continue
		}
		if strings.TrimSpace(edge.SrcNodeID) != "" || strings.TrimSpace(edge.DstNodeID) != "" {
			continue
		}
		structuralPairs[edge.SrcPath+"\x00"+edge.DstPath] = struct{}{}
	}
	for _, row := range rows {
		srcDoc := typedDocs[row.SrcPath]
		dstDoc := typedDocs[row.DstPath]
		if srcDoc == nil || dstDoc == nil || row.SrcPath == row.DstPath {
			continue
		}
		if _, ok := structuralPairs[row.SrcPath+"\x00"+row.DstPath]; !ok {
			out = append(out, semdb.OntologyEdgeRow{
				SrcPath:      row.SrcPath,
				RelationName: ambientRelationName(schema, srcDoc.TypeName, dstDoc.TypeName, true),
				DstPath:      row.DstPath,
				DstType:      dstDoc.TypeName,
				Provenance:   "body_link",
				Structural:   false,
				SchemaHash:   schema.Hash,
				UpdatedAt:    time.Now().Unix(),
			})
		}
		if _, ok := structuralPairs[row.DstPath+"\x00"+row.SrcPath]; ok {
			continue
		}
		out = append(out, semdb.OntologyEdgeRow{
			SrcPath:      row.DstPath,
			RelationName: ambientRelationName(schema, dstDoc.TypeName, srcDoc.TypeName, false),
			DstPath:      row.SrcPath,
			DstType:      srcDoc.TypeName,
			Provenance:   "backlink",
			Structural:   false,
			SchemaHash:   schema.Hash,
			UpdatedAt:    time.Now().Unix(),
		})
	}
	return out
}

func ambientRelationName(schema *Schema, srcTypeName, dstTypeName string, bodyLink bool) string {
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
		case FieldKindLink:
			if bodyLink && !field.IncludeBodyLinks {
				continue
			}
			if !bodyLink && !field.IncludeBacklinks {
				continue
			}
			if !typeMatchesOrImplements(schema, dstTypeName, field.TypeName) {
				continue
			}
		case FieldKindNeighbor:
			switch field.Direction {
			case NeighborDirectionOutbound:
				if !bodyLink {
					continue
				}
			case NeighborDirectionInbound:
				if bodyLink {
					continue
				}
			case NeighborDirectionBoth:
				// allow either
			}
			if !typeMatchesOrImplements(schema, dstTypeName, field.TypeName) {
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

func extractFieldValues(doc *noteDoc, field *Field) []string {
	if doc == nil || field == nil {
		return nil
	}
	switch field.SourceKind {
	case FieldSourceInline:
		for _, name := range FieldSourceNames(field) {
			if values := normalizeStringValues(caseInsensitiveInlineValues(doc.Inline, name)); len(values) > 0 {
				return values
			}
		}
		return nil
	default:
		for _, name := range FieldSourceNames(field) {
			value := caseInsensitiveFrontmatterValue(doc.Frontmatter, name)
			if value == nil {
				continue
			}
			values := flattenValueStrings(value)
			if len(values) > 0 || caseInsensitiveFrontmatterHasKey(doc.Frontmatter, name) {
				return values
			}
		}
		return nil
	}
}

func caseInsensitiveFrontmatterValue(values map[string]interface{}, key string) interface{} {
	if len(values) == 0 {
		return nil
	}
	if value, ok := values[key]; ok {
		return value
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey, value := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return value
		}
	}
	return nil
}

func caseInsensitiveInlineValues(values map[string][]string, key string) []string {
	if len(values) == 0 {
		return nil
	}
	if value, ok := values[key]; ok {
		return value
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey, value := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return value
		}
	}
	return nil
}

func fieldPresent(doc *noteDoc, field *Field) bool {
	if doc == nil || field == nil {
		return false
	}
	switch field.SourceKind {
	case FieldSourceInline:
		for _, name := range FieldSourceNames(field) {
			if caseInsensitiveInlineHasKey(doc.Inline, name) {
				return true
			}
		}
		return false
	default:
		for _, name := range FieldSourceNames(field) {
			if caseInsensitiveFrontmatterHasKey(doc.Frontmatter, name) {
				return true
			}
		}
		return false
	}
}

func caseInsensitiveFrontmatterHasKey(values map[string]interface{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	if _, ok := values[key]; ok {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return true
		}
	}
	return false
}

func caseInsensitiveInlineHasKey(values map[string][]string, key string) bool {
	if len(values) == 0 {
		return false
	}
	if _, ok := values[key]; ok {
		return true
	}
	needle := strings.ToLower(strings.TrimSpace(key))
	for currentKey := range values {
		if strings.ToLower(strings.TrimSpace(currentKey)) == needle {
			return true
		}
	}
	return false
}

func flattenValueStrings(v interface{}) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case []string:
		return normalizeStringValues(val)
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			out = append(out, flattenValueStrings(item)...)
		}
		return normalizeStringValues(out)
	case string:
		return normalizeStringValues([]string{val})
	case bool:
		return []string{strconv.FormatBool(val)}
	case int:
		return []string{strconv.Itoa(val)}
	case int64:
		return []string{strconv.FormatInt(val, 10)}
	case float64:
		return []string{strconv.FormatFloat(val, 'f', -1, 64)}
	case time.Time:
		if val.Hour() == 0 && val.Minute() == 0 && val.Second() == 0 {
			return []string{val.Format("2006-01-02")}
		}
		return []string{val.Format(time.RFC3339)}
	default:
		return []string{strings.TrimSpace(fmt.Sprint(val))}
	}
}

func normalizeStringValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func validateScalarValue(raw string, field *Field, schema *Schema) bool {
	raw = strings.TrimSpace(raw)
	switch field.Kind {
	case FieldKindEnum:
		values := EnumValuesSet(schema, field.TypeName)
		_, ok := values[raw]
		return ok
	case FieldKindScalar:
		switch field.TypeName {
		case "String":
			return true
		case "ID":
			return raw != ""
		case "URL":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "url"
		case "Date":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "date"
		case "DateTime":
			return obsidian.AnalyzePropertyValue(raw).ValueType == "datetime"
		case "Int":
			_, err := strconv.ParseInt(raw, 10, 64)
			return err == nil
		case "Float":
			_, err := strconv.ParseFloat(raw, 64)
			return err == nil
		case "Boolean":
			_, err := strconv.ParseBool(strings.ToLower(raw))
			return err == nil
		default:
			return raw != ""
		}
	default:
		return true
	}
}

func unwrapLinkValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "[[") && strings.HasSuffix(raw, "]]") {
		raw = strings.TrimPrefix(strings.TrimSuffix(raw, "]]"), "[[")
		if idx := strings.Index(raw, "|"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	return strings.TrimSpace(raw)
}

func ontologyLinkTargetResolver(cache *obsidian.NotePathCache, resolveType func(string) string) OntologyLinkTargetResolver {
	if cache == nil {
		return nil
	}
	return OntologyLinkTargetResolverFunc(func(field *Field, targetInput string) (OntologyLinkTarget, bool) {
		targetInput = unwrapLinkValue(targetInput)
		target, ok := cache.ResolveNoteTarget(targetInput)
		if !ok {
			return OntologyLinkTarget{}, false
		}
		targetType := ""
		if resolveType != nil {
			targetType = strings.TrimSpace(resolveType(target.Path))
		}
		ref := NodeRef{
			NotePath:  target.Path,
			Fragment:  target.Fragment,
			TypeName:  targetType,
			Kind:      NodeKindNote,
			StartByte: 0,
			EndByte:   0,
		}
		refJSON, err := json.Marshal(ref)
		if err != nil {
			return OntologyLinkTarget{}, false
		}
		return OntologyLinkTarget{
			NotePath:      ref.NotePath,
			TypeName:      ref.TypeName,
			NodeID:        OntologyNodeID(ref),
			RefJSON:       string(refJSON),
			SourceLocator: NodeSourceLocator(ref),
		}, true
	})
}

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func decodeIssues(raw string) []ValidationIssue {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var issues []ValidationIssue
	if err := json.Unmarshal([]byte(raw), &issues); err != nil {
		return []ValidationIssue{{Code: "schema_state_decode_error", Message: err.Error()}}
	}
	return issues
}

func ValidationIssuesFromJSON(raw string) []ValidationIssue {
	return decodeIssues(raw)
}

func equalValidationIssues(left, right []ValidationIssue) bool {
	left = append([]ValidationIssue(nil), left...)
	right = append([]ValidationIssue(nil), right...)
	sortValidationIssues(left)
	sortValidationIssues(right)
	return reflect.DeepEqual(left, right)
}

func sortValidationIssues(issues []ValidationIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		switch {
		case a.NotePath != b.NotePath:
			return a.NotePath < b.NotePath
		case a.Code != b.Code:
			return a.Code < b.Code
		case a.TypeName != b.TypeName:
			return a.TypeName < b.TypeName
		case a.FieldName != b.FieldName:
			return a.FieldName < b.FieldName
		case a.NodeRef != b.NodeRef:
			return a.NodeRef < b.NodeRef
		case a.NodeID != b.NodeID:
			return a.NodeID < b.NodeID
		case a.Structural != b.Structural:
			return a.Structural < b.Structural
		case a.Line != b.Line:
			return a.Line < b.Line
		case a.LinkKind != b.LinkKind:
			return a.LinkKind < b.LinkKind
		case a.LinkTarget != b.LinkTarget:
			return a.LinkTarget < b.LinkTarget
		case a.Message != b.Message:
			return a.Message < b.Message
		case a.FixTarget != b.FixTarget:
			return a.FixTarget < b.FixTarget
		default:
			return a.FixFieldName < b.FixFieldName
		}
	})
}

func dedupeEdges(edges []semdb.OntologyEdgeRow) []semdb.OntologyEdgeRow {
	seen := make(map[string]semdb.OntologyEdgeRow, len(edges))
	for _, edge := range edges {
		key := edge.SrcPath + "\x00" + edge.SrcNodeID + "\x00" + edge.RelationName + "\x00" + edge.DstPath + "\x00" + edge.DstNodeID + "\x00" + edge.Provenance
		if existing, ok := seen[key]; ok {
			if existing.Structural {
				continue
			}
		}
		seen[key] = edge
	}
	out := make([]semdb.OntologyEdgeRow, 0, len(seen))
	for _, edge := range seen {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i]
		b := out[j]
		switch {
		case a.SrcPath != b.SrcPath:
			return a.SrcPath < b.SrcPath
		case a.SrcNodeID != b.SrcNodeID:
			return a.SrcNodeID < b.SrcNodeID
		case a.RelationName != b.RelationName:
			return a.RelationName < b.RelationName
		case a.DstPath != b.DstPath:
			return a.DstPath < b.DstPath
		case a.DstNodeID != b.DstNodeID:
			return a.DstNodeID < b.DstNodeID
		default:
			return a.Provenance < b.Provenance
		}
	})
	return out
}

func titleFromNotePath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(v interface{}) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
