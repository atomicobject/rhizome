package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func buildOntologyDelta(schema *ontoschema.Schema, noteState semdb.NoteMetadataState, recomputePaths []string, deletePaths []string, replaceSet map[string]struct{}, results []noteSyncResult, existingEdges []semdb.OntologyEdgeRow, fullRebuild bool) (semdb.OntologyDelta, []ValidationIssue, int) {
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
	delta.ReplaceTypePolicies = true
	delta.TypePolicies = buildTypePolicyRows(schema, noteState.LoadedAt)
	var issues []ValidationIssue
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
		sortValidationIssues(assessment.Issues)
		setAssessmentRow(&delta.Assessments[i], *assessment)
	}
	sortValidationIssues(issues)
	if delta.SchemaState != nil {
		delta.SchemaState.ErrorJSON = mustJSON(issues)
	}
	delta = semdb.NormalizeOntologyDelta(delta)
	return delta, issues, relations
}

func validateInversesIncremental(edges []semdb.OntologyEdgeRow, assessments map[string]*NoteAssessment, schema *ontoschema.Schema, existingStructural map[string]struct{}) []ValidationIssue {
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
