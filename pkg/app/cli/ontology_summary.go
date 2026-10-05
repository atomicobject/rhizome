package actions

import (
	"context"
	"sort"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// OntologyTypeCount summarizes resolved ontology node counts for one type.
type OntologyTypeCount struct {
	TypeName string   `json:"typeName"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

// OntologyVaultSummary is a compact ontology-first vault summary.
type OntologyVaultSummary struct {
	Available    bool                     `json:"available"`
	Ready        bool                     `json:"ready"`
	SchemaHash   string                   `json:"schemaHash,omitempty"`
	TotalNotes   int                      `json:"totalNotes"`
	TypedNotes   int                      `json:"typedNotes"`
	UntypedNotes int                      `json:"untypedNotes"`
	TypeCounts   []OntologyTypeCount      `json:"typeCounts,omitempty"`
	IssueCount   int                      `json:"issueCount,omitempty"`
	Issues       []obsidian.OntologyIssue `json:"issues,omitempty"`
}

// BuildOntologyVaultSummary computes ontology type counts and issue summaries
// from an already-loaded ontology runtime snapshot.
func BuildOntologyVaultSummary(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store, schema *ontology.Schema, issues []ontology.ValidationIssue, maxExamples int) (*OntologyVaultSummary, error) {
	if store == nil || schema == nil {
		return nil, nil
	}
	if maxExamples <= 0 {
		maxExamples = 3
	}

	summary := &OntologyVaultSummary{
		Available:  true,
		Ready:      true,
		SchemaHash: schema.Hash,
	}
	if len(issues) > 0 {
		summary.IssueCount = len(issues)
		summary.Issues = make([]obsidian.OntologyIssue, 0, len(issues))
		for _, issue := range issues {
			summary.Issues = append(summary.Issues, obsidian.OntologyIssue{
				Code:       issue.Code,
				NotePath:   issue.NotePath,
				TypeName:   issue.TypeName,
				FieldName:  issue.FieldName,
				Line:       issue.Line,
				LinkKind:   issue.LinkKind,
				LinkTarget: issue.LinkTarget,
				Message:    issue.Message,
			})
		}
	}

	allNotes := make([]string, 0)
	if noteMgr != nil {
		notes, err := noteMgr.GetNotesList(vaultDef)
		if err != nil {
			return nil, err
		}
		allNotes = notes
	}
	summary.TotalNotes = len(allNotes)
	scope := noderead.NewService(vaultDef, noteMgr, store, schema).NewScope(ctx, noderead.ScopeOptions{})

	typeNames := make([]string, 0, len(schema.Types))
	for typeName, typeDef := range schema.Types {
		if typeDef == nil {
			continue
		}
		if typeDef.Role != ontology.TypeRoleNote && typeDef.Role != ontology.TypeRoleEmbeddedNode {
			continue
		}
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)

	typedSet := make(map[string]struct{}, len(allNotes))
	for _, typeName := range typeNames {
		result, err := scope.TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: typeName})
		if err != nil {
			return nil, err
		}
		examples := make([]string, 0, min(maxExamples, len(result.Items)))
		for _, item := range result.Items {
			if item.NotePath != "" {
				typedSet[string(paths.NormalizeNotePath(item.NotePath))] = struct{}{}
			}
			if len(examples) == maxExamples {
				continue
			}
			examples = append(examples, item.Ref.String())
		}
		entry := OntologyTypeCount{
			TypeName: typeName,
			Count:    result.Count,
			Examples: examples,
		}
		summary.TypeCounts = append(summary.TypeCounts, entry)
	}

	summary.TypedNotes = len(typedSet)
	if len(allNotes) > 0 {
		untyped := 0
		for _, notePath := range allNotes {
			norm := string(paths.NormalizeNotePath(notePath))
			if _, ok := typedSet[norm]; !ok {
				untyped++
			}
		}
		summary.UntypedNotes = untyped
	}

	return summary, nil
}
