package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func mergeCatalogRefForRequest(requested, catalog ontology.NodeRef) ontology.NodeRef {
	if catalog.IsZero() {
		return requested
	}
	if catalog.Fragment == "" && requested.Fragment != "" {
		catalog.Fragment = requested.Fragment
	}
	if catalog.NodeID == "" && requested.NodeID != "" {
		catalog.NodeID = requested.NodeID
	}
	if catalog.TypeName == "" && requested.TypeName != "" {
		catalog.TypeName = requested.TypeName
	}
	if catalog.Kind == "" && requested.Kind != "" {
		catalog.Kind = requested.Kind
	}
	return catalog
}

func (s *Scope) recordFromCatalogRow(row readmodel.NodeRow) NodeRecord {
	ref := nodeRefFromCatalogRow(row)
	linkTarget := catalogLinkTarget(row, ref)
	var nodeLocator *ontology.NodeLocator
	if row.SourceLocator != "" || linkTarget != nil {
		nodeLocator = &ontology.NodeLocator{
			Ref:           ref,
			Kind:          ref.Kind,
			SourceLocator: firstNonEmptyString(row.SourceLocator, ontology.NodeSourceLocator(ref)),
			Status:        ontology.NodeLocatorStatus(firstNonEmptyString(row.LocatorStatus, string(ontology.NodeLocatorLinkable))),
			LinkTarget:    linkTarget,
		}
	}
	return NodeRecord{
		catalogNodeID:    row.NodeID,
		catalogStartByte: int(row.StartByte),
		Ref:              ref,
		Path:             firstNonEmptyString(row.SourceLocator, row.NotePath),
		Title:            firstNonEmptyString(row.DisplayLabel, row.Title, row.SourceLocator, row.NotePath),
		TypeName:         row.TypeName,
		NodeLocator:      nodeLocator,
		LinkTarget:       linkTarget,
		UpdatedAt:        row.UpdatedAt,
		HasIssues:        s.issueByPath[row.NotePath],
	}
}

func catalogLinkTarget(row readmodel.NodeRow, ref ontology.NodeRef) *ontology.NodeLinkTarget {
	if row.SourceLocator == "" {
		return nil
	}
	return &ontology.NodeLinkTarget{
		Ref:          ref,
		Markdown:     row.SourceLocator,
		Wikilink:     "",
		DisplayLabel: firstNonEmptyString(row.DisplayLabel, row.Title),
		Exists:       strings.TrimSpace(row.LocatorStatus) == "" || row.LocatorStatus == string(ontology.NodeLocatorLinkable),
		BlockID:      strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^"),
	}
}
