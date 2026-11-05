package ontology

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

// Display titles are independent of the capped titles and ITEM_SUMMARY used by embeddings.
func sourceSpanDisplayTitle(content string, span *MarkdownSourceSpan) string {
	if span.Shape != ontoschema.EmbeddedSourceShapeListItem && span.Shape != ontoschema.EmbeddedSourceShapeCheckboxItem {
		return strings.TrimSpace(span.Title)
	}
	headline := sourceSpanItemHeadline(content, span.ContentRange, span.Markers)
	if title, _, ok := splitTitledItemHeadline(headline); ok {
		return title
	}
	if headline != "" {
		return headline
	}
	return strings.TrimSpace(span.Title)
}

func nodeCatalogDisplayLabel(projection *NodeProjection) string {
	if projection != nil && projection.Snapshot != nil && projection.Ref.Kind == NodeKindEmbedded {
		if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
			return sourceSpanDisplayTitle(projection.Snapshot.Content, span)
		}
	}
	return nodeCatalogTitle(projection)
}
