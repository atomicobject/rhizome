package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"strings"
)

func ontologyMaterializationFromReadiness(snapshot semdb.OntologyReadinessSnapshot) ontologyMaterializationSnapshot {
	return ontologyMaterializationSnapshot{Metadata: snapshot.Metadata, NoteStates: snapshot.NoteStates, Assessments: snapshot.Assessments, Nodes: snapshot.Nodes, FieldValues: snapshot.FieldValues, NoteTypes: snapshot.NoteTypes, Edges: snapshot.Edges, LinkDependencies: snapshot.LinkDependencies, TypePolicies: snapshot.TypePolicies, ErrorJSON: snapshot.SchemaState.ErrorJSON}
}

type ontologyMaterializationSnapshot struct {
	Metadata         []semdb.NoteMetadataRow
	NoteStates       map[string]semdb.OntologyNoteStateRow
	Assessments      map[string]semdb.OntologyNoteAssessmentRow
	Nodes            []readmodel.NodeRow
	FieldValues      []readmodel.FieldValueRow
	NoteTypes        []semdb.OntologyNoteTypeRow
	Edges            []semdb.OntologyEdgeRow
	LinkDependencies []readmodel.LinkDependencyRow
	TypePolicies     []semdb.OntologyTypePolicyRow
	ErrorJSON        string
}

func OntologyProviderHash(formats noteformat.Runtime) string {
	parts := []string{formats.Registry().ManifestFingerprint()}
	for _, id := range formats.Registry().IDs() {
		parts = append(parts, fmt.Sprintf("%s:%t", id, formats.CanProject(id)))
	}
	return hashText(strings.Join(parts, "\x00"))
}

func ontologySnapshotMaterialized(schema *ontoschema.Schema, formats noteformat.Runtime, snapshot ontologyMaterializationSnapshot) (bool, error) {
	if schema == nil {
		return true, nil
	}
	projectablePaths := make([]string, 0, len(snapshot.Metadata))
	metadata := make(map[string]semdb.NoteMetadataRow, len(snapshot.Metadata))
	for _, row := range snapshot.Metadata {
		eligible, err := ontologyProjectableMetadataRow(formats, row)
		if err != nil {
			return false, err
		}
		if !eligible {
			continue
		}
		provider, _ := formats.ProviderForPath(paths.RelPath(row.Path))
		descriptor := provider.Descriptor()
		if row.Projection.ProviderVersion != descriptor.ProviderVersion || row.Projection.ProjectionVersion != descriptor.ProjectionVersion || row.Projection.SourceContentHash != row.ContentHash {
			return false, nil
		}
		projectablePaths = append(projectablePaths, row.Path)
		metadata[row.Path] = row
	}
	if !ontologyPoliciesMatch(schema, snapshot.TypePolicies) {
		return false, nil
	}
	if len(snapshot.NoteStates) != len(projectablePaths) || len(snapshot.Assessments) != len(projectablePaths) {
		return false, nil
	}
	schemaHash := schema.Hash
	assessmentIssues := make([]ValidationIssue, 0)
	expectedCatalog := make(map[string]catalogWitness, len(projectablePaths))
	expectedOutputs := make(map[string]catalogWitness, len(projectablePaths))
	expectedFields := make(map[string]catalogWitness, len(projectablePaths))
	decodedAssessments := make(map[string]*NoteAssessment, len(projectablePaths))
	for _, path := range projectablePaths {
		assessment, ok := snapshot.Assessments[path]
		if !ok || assessment.SchemaHash != schemaHash || strings.TrimSpace(assessment.AssessmentJSON) == "" {
			return false, nil
		}
		decoded, err := AssessmentFromJSON(assessment.AssessmentJSON)
		if err != nil || decoded == nil || !assessmentRowConsistent(assessment, decoded) || decoded.NotePath != path || decoded.CatalogNodeCount == nil || *decoded.CatalogNodeCount < 0 || decoded.CatalogNodeDigest == "" || decoded.CatalogFieldCount == nil || *decoded.CatalogFieldCount < 0 || decoded.CatalogFieldDigest == "" || decoded.OutputRowCount == nil || *decoded.OutputRowCount < 0 || decoded.OutputRowDigest == "" || decoded.SourceContentHash != metadata[path].ContentHash {
			return false, nil
		}
		expectedOutputs[path] = catalogWitness{Count: *decoded.OutputRowCount, Digest: decoded.OutputRowDigest}
		expectedCatalog[path] = catalogWitness{Count: *decoded.CatalogNodeCount, Digest: decoded.CatalogNodeDigest}
		decodedAssessments[path] = decoded
		expectedFields[path] = catalogWitness{Count: *decoded.CatalogFieldCount, Digest: decoded.CatalogFieldDigest}
		assessmentIssues = append(assessmentIssues, decoded.Issues...)
	}
	var stateIssues []ValidationIssue
	if strings.TrimSpace(snapshot.ErrorJSON) != "" {
		if err := json.Unmarshal([]byte(snapshot.ErrorJSON), &stateIssues); err != nil {
			return false, nil
		}
	}
	if !equalValidationIssues(assessmentIssues, stateIssues) {
		return false, nil
	}
	catalog := catalogNodeWitnesses(snapshot.Nodes)
	fields := catalogFieldWitnesses(snapshot.FieldValues)
	outputs := ontologyOutputWitnesses(snapshot.NoteTypes, snapshot.Edges, snapshot.LinkDependencies)
	for path := range outputs {
		if _, ok := metadata[path]; !ok {
			return false, nil
		}
	}
	if len(staleOntologyOutputPaths(snapshot.NoteStates, nodeRowsByID(snapshot.Nodes), snapshot.NoteTypes, snapshot.Edges, snapshot.LinkDependencies, schemaHash)) > 0 {
		return false, nil
	}
	nodesByPath := make(map[string][]readmodel.NodeRow, len(projectablePaths))
	nodeByID := make(map[string]readmodel.NodeRow, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if node.SchemaHash != schemaHash {
			return false, nil
		}
		if _, ok := metadata[node.NotePath]; !ok {
			return false, nil
		}
		nodesByPath[node.NotePath] = append(nodesByPath[node.NotePath], node)
		nodeByID[node.NodeID] = node
	}
	fieldRowsByNode := map[string]map[string]struct{}{}
	for _, field := range snapshot.FieldValues {
		node, ok := nodeByID[field.NodeID]
		if !ok || field.NotePath != node.NotePath || field.SchemaHash != schemaHash || field.UpdatedAt < node.UpdatedAt {
			return false, nil
		}
		if fieldRowsByNode[field.NodeID] == nil {
			fieldRowsByNode[field.NodeID] = map[string]struct{}{}
		}
		fieldRowsByNode[field.NodeID][strings.ToLower(strings.TrimSpace(field.FieldName))] = struct{}{}
	}
	for _, path := range projectablePaths {
		state, ok := snapshot.NoteStates[path]
		if !ok || state.SchemaHash != schemaHash || state.InputFingerprint == "" || state.ResolvedType != decodedAssessments[path].ResolvedType || snapshot.Assessments[path].UpdatedAt < state.UpdatedAt || catalogWitnessForPath(catalog, path) != expectedCatalog[path] || catalogWitnessForPath(fields, path) != expectedFields[path] || catalogWitnessForPath(outputs, path) != expectedOutputs[path] {
			return false, nil
		}
		for _, node := range nodesByPath[path] {
			if node.UpdatedAt < state.UpdatedAt || !ontologyNodeRequiredFieldRowsMaterialized(schema, node, fieldRowsByNode[node.NodeID], decodedAssessments[path]) {
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

func ontologyNodeRequiredFieldRowsMaterialized(schema *ontoschema.Schema, node readmodel.NodeRow, rows map[string]struct{}, assessment *NoteAssessment) bool {
	noteType := schema.Types[strings.TrimSpace(node.TypeName)]
	if noteType == nil {
		return true
	}
	for _, field := range noteType.Fields {
		if field == nil || !field.Required || !readModelFieldKind(field.Kind) {
			continue
		}
		if _, ok := rows[strings.ToLower(strings.TrimSpace(field.Name))]; ok {
			continue
		}
		missing := false
		if assessment != nil {
			for _, issue := range assessment.Issues {
				if issue.Code != "missing_required_field" || issue.FieldName != field.Name || issue.TypeName != node.TypeName {
					continue
				}
				if issue.NodeRef == node.SourceLocator || (node.NodeKind == string(NodeKindNote) && issue.NodeRef == "") {
					missing = true
					break
				}
			}
		}
		if !missing {
			return false
		}
	}
	return true
}

func readModelFieldKind(kind ontoschema.FieldKind) bool {
	switch kind {
	case ontoschema.FieldKindScalar, ontoschema.FieldKindEnum, ontoschema.FieldKindLink:
		return true
	default:
		return false
	}
}
