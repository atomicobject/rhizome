package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	"sort"
)

func (s *Scope) listAll() ontology.TypeListResult {
	items := make([]ontology.NodeListItem, 0, len(s.noteRows))
	for _, row := range s.noteRows {
		resolved := ""
		if typeRow, ok := s.typeRowsByPath[row.Path]; ok {
			resolved = typeRow.TypeName
			if ontoschema.OntologyTypeVisibility(resolved) == ontoschema.TypeVisibilityInternal {
				resolved = ""
			}
		}
		items = append(items, ontology.NodeListItem{
			Ref:          ontology.NodeRef{NotePath: row.Path, Kind: ontology.NodeKindNote},
			Title:        firstNonEmptyString(row.Title, row.Path),
			ResolvedType: resolved,
			NotePath:     row.Path,
			UpdatedAt:    row.Mtime,
			HasIssues:    s.issueByPath[row.Path],
		})
	}
	sortNodeListItems(items)
	return ontology.TypeListResult{
		TypeDoc:    allNotesTypeDoc(),
		Count:      len(items),
		IssueCount: s.issueCountForItems(items),
		Items:      items,
	}
}

func (s *Scope) listIssues() ontology.TypeListResult {
	items := make([]ontology.NodeListItem, 0)
	for _, row := range s.noteRows {
		if !s.issueByPath[row.Path] {
			continue
		}
		resolved := ""
		if typeRow, ok := s.typeRowsByPath[row.Path]; ok {
			resolved = typeRow.TypeName
			if ontoschema.OntologyTypeVisibility(resolved) == ontoschema.TypeVisibilityInternal {
				resolved = ""
			}
		}
		items = append(items, ontology.NodeListItem{
			Ref:          ontology.NodeRef{NotePath: row.Path, Kind: ontology.NodeKindNote},
			Title:        firstNonEmptyString(row.Title, row.Path),
			ResolvedType: resolved,
			NotePath:     row.Path,
			UpdatedAt:    row.Mtime,
			HasIssues:    true,
		})
	}
	sortNodeListItems(items)
	return ontology.TypeListResult{
		TypeDoc:    &schemadoc.TypeDoc{Name: ontology.TypeScopeIssues, Label: "Notes with issues", Description: "Every note that has at least one validation issue."},
		Count:      len(items),
		IssueCount: s.issueCountForItems(items),
		Items:      items,
	}
}

func (s *Scope) noteRootItem(path string, resolvedType string) ontology.NodeListItem {
	row := s.noteRowByPath[path]
	return ontology.NodeListItem{
		Ref:          ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote},
		Title:        firstNonEmptyString(row.Title, path),
		ResolvedType: resolvedType,
		NotePath:     path,
		UpdatedAt:    row.Mtime,
		HasIssues:    s.issueByPath[path],
	}
}

func (s *Scope) issueCountForItems(items []ontology.NodeListItem) int {
	seen := map[string]struct{}{}
	total := 0
	for _, item := range items {
		if _, ok := seen[item.NotePath]; ok {
			continue
		}
		seen[item.NotePath] = struct{}{}
		if s.issueByPath[item.NotePath] {
			total++
		}
	}
	return total
}

func sortNodeListItems(items []ontology.NodeListItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		return items[i].Ref.String() < items[j].Ref.String()
	})
}
