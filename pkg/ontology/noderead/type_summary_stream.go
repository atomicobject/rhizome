package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func (s *Scope) streamTypeSummariesLocked(ctx context.Context, accepted map[string][]string, limit int, results map[string]ontology.TypeListResult) (bool, error) {
	byType := map[string][]string{}
	seen := map[string]map[string]bool{}
	issueHosts := map[string]map[string]bool{}
	for name, types := range accepted {
		seen[name] = map[string]bool{}
		issueHosts[name] = map[string]bool{}
		for _, t := range types {
			byType[t] = append(byType[t], name)
		}
	}
	types := make([]string, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	return s.service.Store.VisitOntologySummaryRows(ctx, types, ontology.OntologyMaterializationVersion, func(row readmodel.SummaryRow) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := row.Node
		issue := row.HasIssues
		if s.flagKnown[n.NotePath] {
			issue = s.issueByPath[n.NotePath]
		}
		item := ontology.NodeListItem{Ref: nodeRefFromCatalogRow(n), Title: firstNonEmptyString(n.DisplayLabel, n.Title, n.SourceLocator, n.NotePath),
			ResolvedType: n.TypeName, NotePath: n.NotePath, UpdatedAt: n.UpdatedAt, HasIssues: issue}
		if n.NodeKind == string(ontology.NodeKindNote) {
			title, mtime := row.MetadataTitle, row.MetadataMtime
			if s.noteRowKnown[n.NotePath] {
				title, mtime = s.noteRowByPath[n.NotePath].Title, s.noteRowByPath[n.NotePath].Mtime
			}
			item.Ref = ontology.NodeRef{NotePath: n.NotePath, Kind: ontology.NodeKindNote}
			item.Title = firstNonEmptyString(title, n.NotePath)
			item.UpdatedAt = mtime
		}
		key := RefIdentityKey(item.Ref)
		for _, name := range byType[n.TypeName] {
			if seen[name][key] {
				continue
			}
			seen[name][key] = true
			result := results[name]
			result.Count++
			if issue && !issueHosts[name][n.NotePath] {
				result.IssueCount++
				issueHosts[name][n.NotePath] = true
			}
			if limit > 0 {
				// Keep the stable first winner when both public sort keys tie.
				result.Items = append(result.Items, item)
				sortNodeListItems(result.Items)
				if len(result.Items) > limit {
					result.Items = result.Items[:limit]
				}
			}
			results[name] = result
		}
		return nil
	})
}

// SummaryTotals preserves the legacy assessment path until materialized flags are current.
func (s *Scope) SummaryTotals(ctx context.Context) (readmodel.SummaryTotals, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version := ontology.OntologyMaterializationVersion
	if s.service.Schema == nil {
		version = 0
	}
	if !s.hasOverlay() && s.service.ExactNoteMetadataRows == nil {
		result, usable, err := s.service.Store.OntologySummaryTotals(ctx, version)
		if err != nil || usable {
			return result, err
		}
	}
	if err := s.ensureNoteState(ctx); err != nil {
		return readmodel.SummaryTotals{}, err
	}
	result := readmodel.SummaryTotals{Notes: len(s.allNotePaths)}
	for _, path := range s.allNotePaths {
		if _, ok := s.typeRowsByPath[path]; ok {
			result.Typed++
		}
		if s.flagsByPath[path].HasIssues {
			result.Issues++
		}
		if s.flagsByPath[path].TypeAmbiguous {
			result.Ambiguous++
		}
	}
	return result, nil
}
