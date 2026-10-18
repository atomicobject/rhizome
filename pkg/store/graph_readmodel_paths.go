package store

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"sort"
	"strings"
)

func (s *Store) GraphIndexedPaths(ctx context.Context, includeCode bool) ([]readmodel.GraphPathRow, error) {
	notePaths, codePaths, err := s.GraphDocPaths(ctx)
	if err != nil {
		return nil, err
	}
	if includeCode {
		indexedCodePaths, err := s.IndexedFilePaths(ctx)
		if err != nil {
			return nil, err
		}
		codePaths = append(codePaths, indexedCodePaths...)
	}
	out := make([]readmodel.GraphPathRow, 0, len(notePaths))
	for _, path := range notePaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		out = append(out, readmodel.GraphPathRow{Path: path, Kind: "note"})
	}
	seenPaths := make(map[string]struct{}, len(out))
	for _, row := range out {
		seenPaths[row.Kind+"\x00"+row.Path] = struct{}{}
	}
	edges, err := s.allGraphDocEdgesReadmodel(ctx, 0, true, true)
	if err != nil {
		return nil, err
	}
	pathKinds := graphReadmodelPathKinds(notePaths, codePaths, edges)
	for path, kind := range pathKinds {
		if !includeCode && kind != "note" {
			continue
		}
		key := kind + "\x00" + path
		if _, found := seenPaths[key]; found {
			continue
		}
		seenPaths[key] = struct{}{}
		out = append(out, readmodel.GraphPathRow{Path: path, Kind: kind})
	}
	if !includeCode {
		sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
		return out, nil
	}
	seenCodePaths := make(map[string]struct{}, len(codePaths))
	for _, path := range codePaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		seenCodePaths[path] = struct{}{}
	}
	for path := range seenCodePaths {
		key := "code\x00" + path
		if _, found := seenPaths[key]; found {
			continue
		}
		seenPaths[key] = struct{}{}
		out = append(out, readmodel.GraphPathRow{Path: path, Kind: "code"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func graphNodeRowFromIntel(row readmodel.NodeRow) readmodel.GraphNodeRow {
	return readmodel.GraphNodeRow{
		NodeID:        row.NodeID,
		NotePath:      row.NotePath,
		NodeKind:      row.NodeKind,
		TypeName:      row.TypeName,
		Title:         row.Title,
		DisplayLabel:  row.DisplayLabel,
		SourceLocator: row.SourceLocator,
		Fragment:      row.Fragment,
		BlockID:       row.BlockID,
		ParentNodeID:  row.ParentNodeID,
		NodeRefJSON:   row.NodeRefJSON,
		UpdatedAt:     row.UpdatedAt,
	}
}

func graphTypedEdgeRowFromSQLite(row OntologyEdgeRow) readmodel.GraphTypedEdgeRow {
	return readmodel.GraphTypedEdgeRow{
		SrcPath:      row.SrcPath,
		SrcNodeID:    row.SrcNodeID,
		RelationName: row.RelationName,
		DstPath:      row.DstPath,
		DstNodeID:    row.DstNodeID,
		DstType:      row.DstType,
		Provenance:   row.Provenance,
		Structural:   row.Structural,
		UpdatedAt:    row.UpdatedAt,
	}
}

func pathMatchesAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if hasPrefixBoundary(path, prefix) {
			return true
		}
	}
	return false
}

func hasPrefixBoundary(path, prefix string) bool {
	path = strings.TrimSpace(path)
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func graphReadmodelPathKinds(notes, code []string, rows []GraphDocEdge) map[string]string {
	out := make(map[string]string, len(notes)+len(code))
	for _, path := range notes {
		if path = strings.TrimSpace(path); path != "" {
			out[path] = "note"
		}
	}
	for _, path := range code {
		if path = strings.TrimSpace(path); path != "" {
			if _, isNote := out[path]; !isNote {
				out[path] = "code"
			}
		}
	}
	for _, row := range rows {
		sourceKind, targetKind := graphDocEdgeEndpointKinds(row)
		setGraphReadmodelPathKind(out, row.SrcPath, sourceKind)
		setGraphReadmodelPathKind(out, row.DstPath, targetKind)
	}
	return out
}

func setGraphReadmodelPathKind(pathKinds map[string]string, path, kind string) {
	path = strings.TrimSpace(path)
	if path == "" || (kind != "note" && kind != "code") {
		return
	}
	if existing := pathKinds[path]; existing == "note" {
		return
	}
	pathKinds[path] = kind
}

func graphDocEdgeEndpointKinds(row GraphDocEdge) (string, string) {
	sourcePath := strings.TrimSpace(row.SrcPath)
	targetPath := strings.TrimSpace(row.DstPath)
	if sourcePath == "" || targetPath == "" {
		return "", ""
	}
	sourceKind := strings.TrimSpace(row.SourceKind)
	targetKind := strings.TrimSpace(row.TargetKind)
	if (sourceKind != "note" && sourceKind != "code") || (targetKind != "note" && targetKind != "code") {
		return "", ""
	}
	return sourceKind, targetKind
}
