package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func (s *Scope) catalogListItemsLocked(ctx context.Context, rows []readmodel.NodeRow) ([]ontology.NodeListItem, error) {
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, row.NotePath)
	}
	if err := s.ensureIssueFlagsLocked(ctx, paths); err != nil {
		return nil, err
	}
	items := make([]ontology.NodeListItem, 0, len(rows))
	for _, row := range rows {
		ref := nodeRefFromCatalogRow(row)
		if row.NodeKind == string(ontology.NodeKindNote) {
			ref = ontology.NodeRef{NotePath: row.NotePath, Kind: ontology.NodeKindNote}
		}
		items = append(items, ontology.NodeListItem{Ref: ref, Title: firstNonEmptyString(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath), ResolvedType: row.TypeName, NotePath: row.NotePath, UpdatedAt: row.UpdatedAt, HasIssues: s.issueByPath[row.NotePath]})
	}
	return items, nil
}
