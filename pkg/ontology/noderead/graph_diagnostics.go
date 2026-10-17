package noderead

import ()

type graphOptions struct {
	includeEmbedded  bool
	includeUntyped   bool
	includeCode      bool
	includeCodeEdges bool
	includeDocEdges  bool
}

type graphDiagnosticsCollector struct {
	enabled bool
	result  GraphDiagnostics
}

func newGraphDiagnostics(enabled bool) *graphDiagnosticsCollector {
	if !enabled {
		return nil
	}
	return &graphDiagnosticsCollector{enabled: true}
}

func (d *graphDiagnosticsCollector) node(node GraphEndpoint, source, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Nodes = append(d.result.Nodes, GraphNodeDiagnostic{
		ID:      node.ID,
		Source:  source,
		Reason:  reason,
		Ref:     node.Ref,
		Locator: node.SourceLocator,
	})
}

func (d *graphDiagnosticsCollector) edge(edge GraphReadEdge, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Edges = append(d.result.Edges, GraphEdgeDiagnostic{
		Source:       edge.Source,
		Target:       edge.Target,
		Kind:         edge.Kind,
		RelationName: edge.RelationName,
		Provenance:   edge.Provenance,
		Reason:       reason,
	})
}

func (d *graphDiagnosticsCollector) skip(id, kind, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Skips = append(d.result.Skips, GraphSkipDiagnostic{
		ID:     id,
		Kind:   kind,
		Reason: reason,
	})
}

func (d *graphDiagnosticsCollector) dedupe(edge GraphReadEdge, preferredKind, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Dedupe = append(d.result.Dedupe, GraphDedupeDiagnostic{
		Source:         edge.Source,
		Target:         edge.Target,
		SuppressedKind: edge.Kind,
		PreferredKind:  preferredKind,
		Reason:         reason,
	})
}

func (d *graphDiagnosticsCollector) resultPtr() *GraphDiagnostics {
	if d == nil || !d.enabled {
		return nil
	}
	out := d.result
	return &out
}
