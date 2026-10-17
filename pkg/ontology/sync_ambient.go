package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func buildAmbientEdgesForSync(srcPath, srcType string, rows []semdb.GraphDocEdge, structuralPairs map[string]struct{}, resolveType func(string) string, schema *ontoschema.Schema, updatedAt int64) []semdb.OntologyEdgeRow {
	if strings.TrimSpace(srcType) == "" || len(rows) == 0 {
		return nil
	}
	out := make([]semdb.OntologyEdgeRow, 0, len(rows))
	for _, row := range rows {
		var targetPath, provenance string
		var bodyLink bool
		switch {
		case row.SrcPath == srcPath && row.DstPath != srcPath:
			targetPath, provenance, bodyLink = row.DstPath, "body_link", true
		case row.DstPath == srcPath && row.SrcPath != srcPath:
			targetPath, provenance = row.SrcPath, "backlink"
		default:
			continue
		}
		dstType := resolveType(targetPath)
		if strings.TrimSpace(dstType) == "" {
			continue
		}
		if _, ok := structuralPairs[srcPath+"\x00"+targetPath]; ok {
			continue
		}
		out = append(out, semdb.OntologyEdgeRow{
			SrcPath:      srcPath,
			RelationName: ambientRelationName(schema, srcType, dstType, bodyLink),
			DstPath:      targetPath,
			DstType:      dstType,
			Provenance:   provenance,
			Structural:   false,
			SchemaHash:   schema.Hash,
			UpdatedAt:    updatedAt,
		})
	}
	return dedupeEdges(out)
}

func groupIncidentGraphRows(rows []semdb.GraphDocEdge) map[string][]semdb.GraphDocEdge {
	out := make(map[string][]semdb.GraphDocEdge, len(rows))
	for _, row := range rows {
		out[row.SrcPath] = append(out[row.SrcPath], row)
		if row.DstPath != row.SrcPath {
			out[row.DstPath] = append(out[row.DstPath], row)
		}
	}
	return out
}
