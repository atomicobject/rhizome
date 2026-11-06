package ontology

func nodeCatalogDisplayLabel(projection *NodeProjection) string {
	if projection != nil && projection.Snapshot != nil && projection.Ref.Kind == NodeKindEmbedded {
		if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
			return sourceSpanDisplayTitle(projection.Snapshot.Content, span)
		}
	}
	return nodeCatalogTitle(projection)
}
