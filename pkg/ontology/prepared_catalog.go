package ontology

import (
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"sort"
	"strings"
)

func buildPreparedNodeReadModel(prepared *PreparedDocument, projection *NodeProjection, now int64, opts BuildIntelOntologyNodeReadModelOptions) (readmodel.NodeReadModel, error) {
	result := readmodel.NodeReadModel{}
	seen := map[string]bool{}
	var collect func(*NodeProjection, *NodeProjection) error
	collect = func(node, parent *NodeProjection) error {
		if node == nil {
			return nil
		}
		row, err := IntelOntologyNodeForProjection(prepared.schema, node, parent, now)
		if err != nil {
			return err
		}
		if seen[row.NodeID] {
			return nil
		}
		seen[row.NodeID] = true
		result.Nodes = append(result.Nodes, row)
		if node.Record != nil {
			result.RecordSearchTexts = append(result.RecordSearchTexts, readmodel.RecordSearchText{NodeID: row.NodeID, NotePath: row.NotePath, Title: row.Title, Body: recordSearchBody(node)})
		}
		fields, deps := IntelOntologyNodeFieldReadModelForProjection(prepared.schema, node, row, now, opts)
		result.FieldValues = append(result.FieldValues, fields...)
		result.LinkDependencies = append(result.LinkDependencies, deps...)
		names := make([]string, 0, len(node.Fields))
		for name := range node.Fields {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, ref := range node.Fields[name].ChildRefs {
				child, err := prepared.ProjectBound(ref)
				if err != nil {
					return err
				}
				if err = collect(child, semanticChildParent(node, parent)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	root := projection
	if strings.TrimSpace(root.ResolvedType) == "" {
		if fallback, ok := AsFallbackNoteProjection(root); ok {
			root = fallback
		}
	}
	if root.ResolvedType != "" {
		if err := collect(root, nil); err != nil {
			return result, err
		}
		if IsFallbackNoteProjection(root) {
			for _, section := range FallbackSectionProjections(root) {
				prepared.attach(section)
				if err := collect(section, root); err != nil {
					return result, err
				}
			}
		}
	}
	if prepared.markdown != nil {
		for _, ref := range prepared.markdown.globalSourceNodeRefs() {
			child, err := prepared.ProjectBound(ref)
			if err != nil {
				return result, err
			}
			if err = collect(child, root); err != nil {
				return result, err
			}
		}
	}
	for _, record := range prepared.records {
		if !record.valid {
			continue
		}
		parent := root
		if record.parent != nil {
			parent = record.parent.projection
		}
		if err := collect(record.projection, parent); err != nil {
			return result, err
		}
	}
	for _, node := range result.Nodes {
		result.NotePaths = append(result.NotePaths, node.NotePath)
	}
	result.NotePaths = uniqueSortedNodeCatalogStrings(result.NotePaths)
	return result, nil
}
