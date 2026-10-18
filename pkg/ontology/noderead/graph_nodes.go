package noderead

import (
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"sort"
	"strings"
)

func graphEndpointFromCatalogRow(row readmodel.GraphNodeRow) GraphEndpoint {
	ref := nodeRefFromGraphNodeRow(row)
	locator := graphPreferredSourceLocator(row)
	return GraphEndpoint{
		ID:            graphEndpointID(graphEndpointKindForOntologyNodeKind(row.NodeKind), row.NodeID),
		Ref:           ref,
		Kind:          graphEndpointKindForOntologyNodeKind(row.NodeKind),
		Path:          firstNonEmptyString(locator, row.SourceLocator, row.NotePath),
		NotePath:      row.NotePath,
		NodeID:        row.NodeID,
		TypeName:      row.TypeName,
		Label:         firstNonEmptyString(row.DisplayLabel, row.Title, locator, row.SourceLocator, row.NotePath),
		SourceLocator: locator,
		ParentID:      row.ParentNodeID,
	}
}

func graphEndpointKindForOntologyNodeKind(kind string) GraphEndpointKind {
	switch ontology.NodeKind(kind) {
	case ontology.NodeKindSection:
		return GraphEndpointSection
	case ontology.NodeKindEmbedded:
		return GraphEndpointEmbedded
	default:
		return GraphEndpointNote
	}
}

func uniqueGraphNodeRows(rows map[string]readmodel.GraphNodeRow) []readmodel.GraphNodeRow {
	seen := map[string]struct{}{}
	out := make([]readmodel.GraphNodeRow, 0, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.NodeID)
		if key == "" {
			key = strings.TrimSpace(row.SourceLocator)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return graphEndpointID(graphEndpointKindForOntologyNodeKind(out[i].NodeKind), out[i].NodeID) < graphEndpointID(graphEndpointKindForOntologyNodeKind(out[j].NodeKind), out[j].NodeID)
	})
	return out
}

func graphEndpointFromNote(path, typeName string, row readmodel.GraphNodeRow) GraphEndpoint {
	ref := ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote, TypeName: typeName}
	nodeID := ""
	sourceLocator := path
	endpointTypeName := typeName
	if row.NodeRefJSON != "" {
		ref = nodeRefFromGraphNodeRow(row)
		ref.Kind = ontology.NodeKindNote
		ref.NotePath = path
		ref.Fragment = ""
		ref.NodeID = strings.TrimSpace(ref.NodeID)
	}
	nodeID = row.NodeID
	sourceLocator = firstNonEmptyString(row.SourceLocator, path)
	label := firstNonEmptyString(row.DisplayLabel, row.Title, noteTitle(path, nil))
	endpointTypeName = firstNonEmptyString(typeName, row.TypeName)
	return GraphEndpoint{
		ID:            graphEndpointID(GraphEndpointNote, path),
		Ref:           ref,
		Kind:          GraphEndpointNote,
		Path:          path,
		NotePath:      path,
		NodeID:        nodeID,
		TypeName:      endpointTypeName,
		Label:         label,
		SourceLocator: sourceLocator,
	}
}

func nodeRefFromGraphNodeRow(row readmodel.GraphNodeRow) ontology.NodeRef {
	if strings.TrimSpace(row.NodeRefJSON) != "" {
		var ref ontology.NodeRef
		if err := json.Unmarshal([]byte(row.NodeRefJSON), &ref); err == nil && !ref.IsZero() {
			return ref
		}
	}
	ref := ontology.NodeRef{
		NotePath: row.NotePath,
		NodeID:   row.NodeID,
		TypeName: row.TypeName,
		Kind:     ontology.NodeKind(row.NodeKind),
	}
	if ref.Kind == "" {
		ref.Kind = ontology.NodeKindNote
	}
	if strings.Contains(row.SourceLocator, "#") {
		parts := strings.SplitN(row.SourceLocator, "#", 2)
		if strings.TrimSpace(parts[0]) != "" {
			ref.NotePath = strings.TrimSpace(parts[0])
		}
		ref.Fragment = strings.TrimSpace(parts[1])
	}
	if ref.Fragment == "" && row.Fragment != "" {
		ref.Fragment = strings.TrimSpace(row.Fragment)
	}
	if strings.TrimSpace(row.BlockID) != "" {
		ref.Fragment = "^" + strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^")
	}
	return ref
}

func graphPreferredSourceLocator(row readmodel.GraphNodeRow) string {
	if strings.TrimSpace(row.NotePath) != "" && strings.TrimSpace(row.BlockID) != "" {
		return strings.TrimSpace(row.NotePath) + "#^" + strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^")
	}
	if strings.TrimSpace(row.NotePath) != "" && strings.TrimSpace(row.Fragment) != "" {
		return strings.TrimSpace(row.NotePath) + "#" + strings.TrimPrefix(strings.TrimSpace(row.Fragment), "#")
	}
	return strings.TrimSpace(row.SourceLocator)
}

func graphEndpointFromPath(kind GraphEndpointKind, path string) GraphEndpoint {
	ref := ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}
	notePath := path
	if kind == GraphEndpointCode {
		ref = ontology.NodeRef{}
		notePath = ""
	}
	return GraphEndpoint{
		ID:            graphEndpointID(kind, path),
		Ref:           ref,
		Kind:          kind,
		Path:          path,
		NotePath:      notePath,
		Label:         noteTitle(path, nil),
		SourceLocator: path,
	}
}

func graphEndpointID(kind GraphEndpointKind, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return string(kind) + ":" + key
}

func addGraphEndpoint(nodes map[string]GraphEndpoint, node GraphEndpoint) {
	if node.ID == "" {
		return
	}
	if existing, ok := nodes[node.ID]; ok {
		if existing.TypeName == "" {
			existing.TypeName = node.TypeName
		}
		if existing.NodeID == "" {
			existing.NodeID = node.NodeID
		}
		if existing.SourceLocator == "" {
			existing.SourceLocator = node.SourceLocator
		}
		if existing.Ref.IsZero() {
			existing.Ref = node.Ref
		}
		nodes[node.ID] = existing
		return
	}
	nodes[node.ID] = node
}

func addGraphEdge(edges map[string]GraphReadEdge, edge GraphReadEdge) {
	if edge.Source == "" || edge.Target == "" || edge.Source == edge.Target {
		return
	}
	if edge.Weight <= 0 {
		edge.Weight = 1
	}
	key := graphReadEdgeKey(edge)
	if existing, ok := edges[key]; ok {
		existing.Weight += edge.Weight
		edges[key] = existing
		return
	}
	edges[key] = edge
}

func graphReadEdgeKey(edge GraphReadEdge) string {
	return edge.Source + "|" + edge.Target + "|" + edge.Kind + "|" + edge.RelationName
}

type graphEndpointPair struct {
	first  string
	second string
}

func newGraphEndpointPair(first, second string) graphEndpointPair {
	if first > second {
		first, second = second, first
	}
	return graphEndpointPair{first: first, second: second}
}

func graphDocRowEndpoint(path, kind string) (GraphEndpointKind, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	switch GraphEndpointKind(kind) {
	case GraphEndpointNote, GraphEndpointCode:
		return GraphEndpointKind(kind), path
	default:
		return "", ""
	}
}

func graphPathHasAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

func sortedGraphEndpoints(nodes map[string]GraphEndpoint, limit int) []GraphEndpoint {
	out := make([]GraphEndpoint, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func sortedGraphEdges(edges map[string]GraphReadEdge, limit int) []GraphReadEdge {
	out := make([]GraphReadEdge, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := graphEdgePriority(out[i]), graphEdgePriority(out[j]); a != b {
			return a < b
		}
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].RelationName < out[j].RelationName
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func relationLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var out []rune
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, ' ')
		}
		if i == 0 && r >= 'a' && r <= 'z' {
			r = r - 'a' + 'A'
		}
		out = append(out, r)
	}
	return string(out)
}
