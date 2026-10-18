package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strconv"
	"strings"
)

func normalizeGraphRequest(req GraphRequest) GraphRequest {
	req.Sources = normalizeGraphSources(req.Sources)
	req.Paths = normalizeStrings(req.Paths)
	req.PathPrefixes = normalizeStrings(req.PathPrefixes)
	if req.Limit <= 0 {
		req.Limit = 500
	}
	if req.NodeLimit == 0 {
		req.NodeLimit = req.Limit
	}
	if req.EdgeLimit == 0 {
		req.EdgeLimit = req.Limit
	}
	if req.Profile == "" {
		req.Profile = GraphProfileOntologyNative
	}
	return req
}

func graphRequestCacheKey(req GraphRequest) string {
	var b strings.Builder
	b.WriteString(string(req.Profile))
	b.WriteString("|nl=")
	b.WriteString(strconv.Itoa(req.NodeLimit))
	b.WriteString("|el=")
	b.WriteString(strconv.Itoa(req.EdgeLimit))
	b.WriteString("|diag=")
	b.WriteString(strconv.FormatBool(req.Diagnostics))
	b.WriteString("|codeEdges=")
	b.WriteString(strconv.FormatBool(req.IncludeCodeEdges))
	b.WriteString("|sources=")
	for _, ref := range req.Sources {
		b.WriteString(RefIdentityKey(ref))
		b.WriteString(";")
	}
	b.WriteString("|paths=")
	for _, path := range req.Paths {
		b.WriteString(path)
		b.WriteString(";")
	}
	b.WriteString("|prefixes=")
	for _, prefix := range req.PathPrefixes {
		b.WriteString(prefix)
		b.WriteString(";")
	}
	return b.String()
}

func cloneGraphResult(result GraphResult) GraphResult {
	out := result
	out.sourceIDs = make(map[string]struct{}, len(result.sourceIDs))
	for id := range result.sourceIDs {
		out.sourceIDs[id] = struct{}{}
	}
	out.Nodes = append([]GraphEndpoint(nil), result.Nodes...)
	out.Edges = append([]GraphReadEdge(nil), result.Edges...)
	if result.Diagnostics != nil {
		diag := *result.Diagnostics
		diag.Nodes = append([]GraphNodeDiagnostic(nil), result.Diagnostics.Nodes...)
		diag.Edges = append([]GraphEdgeDiagnostic(nil), result.Diagnostics.Edges...)
		diag.Skips = append([]GraphSkipDiagnostic(nil), result.Diagnostics.Skips...)
		diag.Dedupe = append([]GraphDedupeDiagnostic(nil), result.Diagnostics.Dedupe...)
		out.Diagnostics = &diag
	}
	return out
}

func graphOptionsForProfile(profile GraphProfile) graphOptions {
	switch profile {
	case GraphProfileNotesOnly:
		return graphOptions{includeEmbedded: true, includeUntyped: true, includeCode: false, includeCodeEdges: false, includeDocEdges: true}
	case GraphProfileCodeAware:
		return graphOptions{includeEmbedded: true, includeUntyped: true, includeCode: true, includeCodeEdges: false, includeDocEdges: true}
	default:
		return graphOptions{includeEmbedded: true, includeUntyped: false, includeCode: false, includeCodeEdges: false, includeDocEdges: false}
	}
}

func normalizeGraphSources(refs []ontology.NodeRef) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() {
			continue
		}
		key := RefIdentityKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func graphSourcePaths(refs []ontology.NodeRef) []string {
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.NotePath != "" {
			paths = append(paths, ref.NotePath)
		}
	}
	return normalizeStrings(paths)
}

func graphRequestPaths(req GraphRequest) []string {
	paths := make([]string, 0, len(req.Sources)+len(req.Paths))
	paths = append(paths, graphSourcePaths(req.Sources)...)
	paths = append(paths, req.Paths...)
	return normalizeStrings(paths)
}

func graphEndpointSelectors(refs []ontology.NodeRef) []readmodel.GraphEndpointSelector {
	selectors := make([]readmodel.GraphEndpointSelector, 0, len(refs))
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() || !graphRefIsEndpointScoped(ref) {
			continue
		}
		selectors = append(selectors, readmodel.GraphEndpointSelector{
			Path:          ref.NotePath,
			NodeID:        ref.NodeID,
			Kind:          string(ref.Kind),
			Fragment:      ref.Fragment,
			Structural:    ref.Structural,
			SourceLocator: ontology.NodeSourceLocator(ref),
		})
	}
	return selectors
}

func graphHasEndpointScopedSources(refs []ontology.NodeRef) bool {
	for _, ref := range refs {
		if graphRefIsEndpointScoped(ref) {
			return true
		}
	}
	return false
}

func graphRefIsEndpointScoped(ref ontology.NodeRef) bool {
	return ref.Kind == ontology.NodeKindEmbedded || ref.Kind == ontology.NodeKindSection || strings.TrimSpace(ref.NodeID) != "" || strings.TrimSpace(ref.Fragment) != "" || strings.TrimSpace(ref.Structural) != ""
}
