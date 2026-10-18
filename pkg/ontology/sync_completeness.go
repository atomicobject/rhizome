package ontology

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"strings"
)

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

func incompleteOntologyPaths(paths []string, snapshot semdb.OntologyReadinessSnapshot, schemaHash string) ([]string, map[string]*NoteAssessment) {
	assessments, states, nodes, fields := snapshot.Assessments, snapshot.NoteStates, snapshot.Nodes, snapshot.FieldValues
	metadata := make(map[string]semdb.NoteMetadataRow, len(snapshot.Metadata))
	for _, row := range snapshot.Metadata {
		metadata[row.Path] = row
	}
	incomplete := make([]string, 0)
	decoded := make(map[string]*NoteAssessment, len(paths))
	catalog := catalogNodeWitnesses(nodes)
	fieldCatalog := catalogFieldWitnesses(fields)
	outputCatalog := ontologyOutputWitnesses(snapshot.NoteTypes, snapshot.Edges, snapshot.LinkDependencies)
	staleCatalog := make(map[string]bool)
	nodesByID := make(map[string]readmodel.NodeRow, len(nodes))
	for _, node := range nodes {
		nodesByID[node.NodeID] = node
		if node.SchemaHash != schemaHash || node.UpdatedAt < states[node.NotePath].UpdatedAt {
			staleCatalog[node.NotePath] = true
		}
	}
	for _, field := range fields {
		node, ok := nodesByID[field.NodeID]
		if !ok || field.NotePath != node.NotePath || field.SchemaHash != schemaHash || field.UpdatedAt < node.UpdatedAt {
			staleCatalog[field.NotePath] = true
		}
	}
	for path := range staleOntologyOutputPaths(states, nodesByID, snapshot.NoteTypes, snapshot.Edges, snapshot.LinkDependencies, schemaHash) {
		staleCatalog[path] = true
	}
	for _, notePath := range paths {
		assessment, assessmentOK := assessments[notePath]
		if assessmentOK && assessment.SchemaHash == schemaHash && strings.TrimSpace(assessment.AssessmentJSON) != "" {
			parsed, err := AssessmentFromJSON(assessment.AssessmentJSON)
			assessmentOK = err == nil && parsed != nil && assessmentRowConsistent(assessment, parsed) && parsed.NotePath == notePath && parsed.CatalogNodeCount != nil && *parsed.CatalogNodeCount >= 0 && parsed.CatalogNodeDigest != "" && parsed.CatalogFieldCount != nil && *parsed.CatalogFieldCount >= 0 && parsed.CatalogFieldDigest != "" && parsed.OutputRowCount != nil && *parsed.OutputRowCount >= 0 && parsed.OutputRowDigest != "" && parsed.SourceContentHash == metadata[notePath].ContentHash
			if assessmentOK {
				decoded[notePath] = parsed
			}
		} else {
			assessmentOK = false
		}
		state, stateOK := states[notePath]
		stateOK = stateOK && assessmentOK && state.ResolvedType == decoded[notePath].ResolvedType && state.SchemaHash == schemaHash && strings.TrimSpace(state.InputFingerprint) != ""
		witness := catalogWitnessForPath(catalog, notePath)
		fieldWitness := catalogWitnessForPath(fieldCatalog, notePath)
		outputWitness := catalogWitnessForPath(outputCatalog, notePath)
		catalogOK := assessmentOK && witness.Count == *decoded[notePath].CatalogNodeCount && witness.Digest == decoded[notePath].CatalogNodeDigest && fieldWitness.Count == *decoded[notePath].CatalogFieldCount && fieldWitness.Digest == decoded[notePath].CatalogFieldDigest && outputWitness.Count == *decoded[notePath].OutputRowCount && outputWitness.Digest == decoded[notePath].OutputRowDigest && !staleCatalog[notePath] && assessment.UpdatedAt >= state.UpdatedAt
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

func orphanedOntologyPaths(paths []string, snapshot semdb.OntologyReadinessSnapshot) []string {
	current := make(map[string]bool, len(paths))
	for _, path := range paths {
		current[path] = true
	}
	stale := map[string]bool{}
	for path := range snapshot.NoteStates {
		if !current[path] {
			stale[path] = true
		}
	}
	for path := range snapshot.Assessments {
		if !current[path] {
			stale[path] = true
		}
	}
	for _, node := range snapshot.Nodes {
		if !current[node.NotePath] {
			stale[node.NotePath] = true
		}
	}
	for _, field := range snapshot.FieldValues {
		if !current[field.NotePath] {
			stale[field.NotePath] = true
		}
	}
	for _, row := range snapshot.NoteTypes {
		if !current[row.NotePath] {
			stale[row.NotePath] = true
		}
	}
	for _, row := range snapshot.Edges {
		if !current[row.SrcPath] {
			stale[row.SrcPath] = true
		}
	}
	for _, row := range snapshot.LinkDependencies {
		if !current[row.SourceNotePath] {
			stale[row.SourceNotePath] = true
		}
	}
	out := make([]string, 0, len(stale))
	for path := range stale {
		out = append(out, path)
	}
	return normalizeStringSet(out)
}

func nodeRowsByID(nodes []readmodel.NodeRow) map[string]readmodel.NodeRow {
	byID := make(map[string]readmodel.NodeRow, len(nodes))
	for _, node := range nodes {
		byID[node.NodeID] = node
	}
	return byID
}

func staleOntologyOutputPaths(states map[string]semdb.OntologyNoteStateRow, nodes map[string]readmodel.NodeRow, types []semdb.OntologyNoteTypeRow, edges []semdb.OntologyEdgeRow, dependencies []readmodel.LinkDependencyRow, schemaHash string) map[string]bool {
	stale := map[string]bool{}
	for _, row := range types {
		if row.SchemaHash != schemaHash || row.UpdatedAt < states[row.NotePath].UpdatedAt {
			stale[row.NotePath] = true
		}
	}
	for _, row := range edges {
		if row.SchemaHash != schemaHash || row.UpdatedAt < states[row.SrcPath].UpdatedAt {
			stale[row.SrcPath] = true
		}
	}
	for _, row := range dependencies {
		node, ok := nodes[row.NodeID]
		if !ok || row.SourceNotePath != node.NotePath || row.SchemaHash != schemaHash || row.UpdatedAt < node.UpdatedAt {
			stale[row.SourceNotePath] = true
		}
	}
	return stale
}
