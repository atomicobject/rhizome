package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func (s *Store) GraphOntologyNodes(ctx context.Context, query readmodel.GraphNodeQuery) ([]readmodel.GraphNodeRow, error) {
	// Docs: [[ontology-indexed-read-model-contract#^spec-0040-endpoint-selector]]
	// requires endpoint selectors to resolve by catalog node id, block/fragment,
	// source_locator, and structured NodeRefJSON without broadening scope.
	var rows []codeanchor.IntelOntologyNode
	var err error
	switch {
	case len(query.EndpointSelectors) > 0:
		rows, err = s.ontologyNodesByEndpointSelectors(ctx, query.EndpointSelectors, query.Limit)
	case len(query.Paths) > 0:
		rows, err = s.OntologyNodesByPaths(ctx, query.Paths)
	case len(query.PathPrefixes) > 0:
		rows, err = s.ontologyNodesByPathPrefixes(ctx, query.PathPrefixes, query.Limit)
	default:
		rows, err = s.AllOntologyNodes(ctx)
	}
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.GraphNodeRow, 0, len(rows))
	for _, row := range rows {
		if len(query.PathPrefixes) > 0 && !pathMatchesAnyPrefix(row.NotePath, query.PathPrefixes) {
			continue
		}
		out = append(out, graphNodeRowFromIntel(row))
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

func (s *Store) GraphOntologyEdges(ctx context.Context, query readmodel.GraphEdgeQuery) ([]readmodel.GraphTypedEdgeRow, error) {
	limit := query.Limit
	var rows []OntologyEdgeRow
	var err error
	switch {
	case len(query.EndpointSelectors) > 0:
		rows, err = s.ontologyEdgesForEndpointSelectors(ctx, query.EndpointSelectors, query.IncludeAmbient, limit)
	case len(query.Paths) > 0:
		rows, err = s.OntologyEdgesForPaths(ctx, query.Paths, query.IncludeAmbient, "", limit)
	case len(query.PathPrefixes) > 0:
		rows, err = s.ontologyEdgesForPathPrefixes(ctx, query.PathPrefixes, query.IncludeAmbient, limit)
	default:
		rows, err = s.AllOntologyEdges(ctx, query.IncludeAmbient, limit)
	}
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.GraphTypedEdgeRow, 0, len(rows))
	for _, row := range rows {
		if len(query.PathPrefixes) > 0 && !pathMatchesAnyPrefix(row.SrcPath, query.PathPrefixes) && !pathMatchesAnyPrefix(row.DstPath, query.PathPrefixes) {
			continue
		}
		out = append(out, graphTypedEdgeRowFromSQLite(row))
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

func (s *Store) GraphDocEdges(ctx context.Context, query readmodel.GraphDocEdgeQuery) ([]readmodel.GraphDocEdgeRow, error) {
	// Docs: [[ontology-indexed-read-model-contract]]
	// keeps code-code edges behind an explicit IncludeCodeEdges request.
	limit := query.Limit
	if query.IncludeCodeEdges {
		query.IncludeCode = true
	}
	var rows []GraphDocEdge
	var err error
	switch {
	case len(query.PathPrefixes) > 0 && len(query.Paths) == 0:
		for _, prefix := range query.PathPrefixes {
			var prefixRows []GraphDocEdge
			prefixRows, err = s.graphDocEdgesForPrefixReadmodel(ctx, prefix, limit, query.IncludeCode, query.IncludeCodeEdges)
			if err != nil {
				return nil, err
			}
			rows = append(rows, prefixRows...)
			if limit > 0 && len(rows) >= limit {
				rows = rows[:limit]
				break
			}
		}
	case len(query.Paths) > 0:
		rows, err = s.graphDocEdgesForPathsReadmodel(ctx, query.Paths, limit, query.IncludeCode, query.IncludeCodeEdges)
	default:
		rows, err = s.allGraphDocEdgesReadmodel(ctx, limit, query.IncludeCode, query.IncludeCodeEdges)
	}
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.GraphDocEdgeRow, 0, len(rows))
	for _, row := range rows {
		if len(query.PathPrefixes) > 0 && !pathMatchesAnyPrefix(row.SrcPath, query.PathPrefixes) && !pathMatchesAnyPrefix(row.DstPath, query.PathPrefixes) {
			continue
		}
		sourceKind, targetKind := graphDocEdgeEndpointKinds(row)
		if sourceKind == "" || targetKind == "" {
			continue
		}
		if !query.IncludeCode && (sourceKind != "note" || targetKind != "note") {
			continue
		}
		out = append(out, readmodel.GraphDocEdgeRow{
			SrcPath:         row.SrcPath,
			DstPath:         row.DstPath,
			SourceKind:      sourceKind,
			TargetKind:      targetKind,
			Kind:            row.Kind,
			Weight:          row.Weight,
			Confidence:      row.Confidence,
			ConfidenceScore: row.ConfidenceScore,
		})
		if query.Limit > 0 && len(out) >= query.Limit {
			break
		}
	}
	return out, nil
}

func (s *Store) ontologyNodesByPathPrefixes(ctx context.Context, prefixes []string, limit int) ([]codeanchor.IntelOntologyNode, error) {
	conditions, args := graphPrefixConditions("note_path", prefixes)
	if len(conditions) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(`
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		WHERE %s
		ORDER BY note_path, node_kind, node_id
	`, strings.Join(conditions, " OR "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.IntelOntologyNode
	for rows.Next() {
		var node codeanchor.IntelOntologyNode
		if err := scanOntologyNode(rows, &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (s *Store) ontologyNodesByEndpointSelectors(ctx context.Context, selectors []readmodel.GraphEndpointSelector, limit int) ([]codeanchor.IntelOntologyNode, error) {
	conditions := make([]string, 0, len(selectors))
	args := make([]any, 0, len(selectors)*4)
	for _, selector := range selectors {
		condition, conditionArgs := graphEndpointSelectorNodeCondition("", selector)
		if condition == "" {
			continue
		}
		conditions = append(conditions, "("+condition+")")
		args = append(args, conditionArgs...)
	}
	if len(conditions) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(`
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		WHERE %s
		ORDER BY note_path, node_kind, node_id
	`, strings.Join(conditions, " OR "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.IntelOntologyNode
	for rows.Next() {
		var node codeanchor.IntelOntologyNode
		if err := scanOntologyNode(rows, &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (s *Store) ontologyEdgesForPathPrefixes(ctx context.Context, prefixes []string, includeAmbient bool, limit int) ([]OntologyEdgeRow, error) {
	srcConditions, args := graphPrefixConditions("src_path", prefixes)
	dstConditions, dstArgs := graphPrefixConditions("dst_path", prefixes)
	args = append(args, dstArgs...)
	if len(srcConditions) == 0 && len(dstConditions) == 0 {
		return nil, nil
	}
	conditions := []string{"(" + strings.Join(append(srcConditions, dstConditions...), " OR ") + ")"}
	if !includeAmbient {
		conditions = append(conditions, "structural = 1")
	}
	query := fmt.Sprintf(`
		SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
		FROM ontology_edges
		WHERE %s
		ORDER BY structural DESC, src_path, relation_name, dst_path, src_node_id, dst_node_id
	`, strings.Join(conditions, " AND "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OntologyEdgeRow
	for rows.Next() {
		var row OntologyEdgeRow
		var structural int
		if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Structural = structural != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) ontologyEdgesForEndpointSelectors(ctx context.Context, selectors []readmodel.GraphEndpointSelector, includeAmbient bool, limit int) ([]OntologyEdgeRow, error) {
	conditions := make([]string, 0, len(selectors))
	args := make([]any, 0, len(selectors)*8)
	for _, selector := range selectors {
		condition, conditionArgs := graphEndpointSelectorEdgeCondition(selector)
		if condition == "" {
			continue
		}
		conditions = append(conditions, "("+condition+")")
		args = append(args, conditionArgs...)
	}
	if len(conditions) == 0 {
		return nil, nil
	}
	where := []string{"(" + strings.Join(conditions, " OR ") + ")"}
	if !includeAmbient {
		where = append(where, "structural = 1")
	}
	query := fmt.Sprintf(`
		SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
		FROM ontology_edges
		WHERE %s
		ORDER BY structural DESC, src_path, relation_name, dst_path, src_node_id, dst_node_id
	`, strings.Join(where, " AND "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OntologyEdgeRow
	for rows.Next() {
		var row OntologyEdgeRow
		var structural int
		if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Structural = structural != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

func graphEndpointSelectorEdgeCondition(selector readmodel.GraphEndpointSelector) (string, []any) {
	nodeID := strings.TrimSpace(selector.NodeID)
	path := strings.TrimSpace(selector.Path)
	conditions := []string{}
	args := []any{}
	if nodeID != "" {
		if path != "" {
			conditions = append(conditions, "((src_path = ? AND src_node_id = ?) OR (dst_path = ? AND dst_node_id = ?))")
			args = append(args, path, nodeID, path, nodeID)
		} else {
			conditions = append(conditions, "(src_node_id = ? OR dst_node_id = ?)")
			args = append(args, nodeID, nodeID)
		}
	}
	srcCondition, srcArgs := graphEndpointSelectorNodeCondition("src_node", selector)
	dstCondition, dstArgs := graphEndpointSelectorNodeCondition("dst_node", selector)
	if srcCondition == "" && dstCondition == "" && len(conditions) == 0 {
		return "", nil
	}
	if srcCondition != "" {
		conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM ontology_nodes src_node WHERE src_node.node_id = ontology_edges.src_node_id AND %s)", srcCondition))
		args = append(args, srcArgs...)
	}
	if dstCondition != "" {
		conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM ontology_nodes dst_node WHERE dst_node.node_id = ontology_edges.dst_node_id AND %s)", dstCondition))
		args = append(args, dstArgs...)
	}
	return strings.Join(conditions, " OR "), args
}

func graphEndpointSelectorNodeCondition(alias string, selector readmodel.GraphEndpointSelector) (string, []any) {
	col := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	conditions := []string{}
	args := []any{}
	if path := strings.TrimSpace(selector.Path); path != "" {
		conditions = append(conditions, col("note_path")+" = ?")
		args = append(args, path)
	}
	if nodeID := strings.TrimSpace(selector.NodeID); nodeID != "" {
		nodeIDConditions := []string{
			col("node_id") + " = ?",
			col("block_id") + " = ?",
			col("fragment") + " = ?",
			col("fragment") + " = ?",
		}
		nodeIDArgs := []any{nodeID, strings.TrimPrefix(nodeID, "^"), nodeID, strings.TrimPrefix(nodeID, "^")}
		if encoded, err := json.Marshal(nodeID); err == nil {
			nodeIDConditions = append(nodeIDConditions, col("node_ref_json")+" LIKE ?")
			nodeIDArgs = append(nodeIDArgs, `%"nodeId":`+string(encoded)+`%`)
		}
		if path := strings.TrimSpace(selector.Path); path != "" {
			block := strings.TrimPrefix(nodeID, "^")
			nodeIDConditions = append(nodeIDConditions, col("source_locator")+" = ?", col("source_locator")+" = ?")
			nodeIDArgs = append(nodeIDArgs, path+"#"+nodeID, path+"#^"+block)
		}
		conditions = append(conditions, "("+strings.Join(nodeIDConditions, " OR ")+")")
		args = append(args, nodeIDArgs...)
	}
	if kind := strings.TrimSpace(selector.Kind); kind != "" {
		conditions = append(conditions, col("node_kind")+" = ?")
		args = append(args, kind)
	}
	if locator := strings.TrimSpace(selector.SourceLocator); locator != "" {
		conditions = append(conditions, col("source_locator")+" = ?")
		args = append(args, locator)
	}
	if fragment := strings.TrimPrefix(strings.TrimSpace(selector.Fragment), "#"); fragment != "" {
		block := strings.TrimPrefix(fragment, "^")
		fragmentConditions := []string{col("fragment") + " = ?", col("block_id") + " = ?"}
		fragmentArgs := []any{fragment, block}
		if path := strings.TrimSpace(selector.Path); path != "" {
			fragmentConditions = append(fragmentConditions, col("source_locator")+" = ?", col("source_locator")+" = ?")
			fragmentArgs = append(fragmentArgs, path+"#"+fragment, path+"#^"+block)
		}
		conditions = append(conditions, "("+strings.Join(fragmentConditions, " OR ")+")")
		args = append(args, fragmentArgs...)
	}
	if structural := strings.TrimSpace(selector.Structural); structural != "" {
		conditions = append(conditions, "("+col("structural_fingerprint")+" = ? OR "+col("fragment")+" = ? OR "+col("block_id")+" = ?)")
		args = append(args, structural, structural, strings.TrimPrefix(structural, "^"))
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return strings.Join(conditions, " AND "), args
}

func graphPrefixConditions(column string, prefixes []string) ([]string, []any) {
	prefixes = normalizeNonEmptyStrings(prefixes)
	conditions := make([]string, 0, len(prefixes))
	args := make([]any, 0, len(prefixes)*2)
	for _, prefix := range prefixes {
		prefix = strings.Trim(strings.TrimSpace(prefix), "/")
		if prefix == "" {
			continue
		}
		conditions = append(conditions, fmt.Sprintf("(%s = ? OR %s LIKE ?)", column, column))
		args = append(args, prefix, prefix+"/%")
	}
	return conditions, args
}

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

func graphNodeRowFromIntel(row codeanchor.IntelOntologyNode) readmodel.GraphNodeRow {
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
