package embeddings

const (
	// OntologyNodeOwnerType identifies ontology node semantic chunks.
	OntologyNodeOwnerType = "ontology_node"
	// GranularityOntologyNodeBody marks source-preserving node body chunks.
	GranularityOntologyNodeBody = "node_body"
	// GranularityOntologyNodeVisible marks provider-projected visible root evidence.
	GranularityOntologyNodeVisible = "node_body_visible"
	// GranularityOntologyNodeSupplemental marks bounded inline script/data evidence.
	// Root evidence assigns it only to ontology_node owners; visible retrieval relies on that invariant.
	GranularityOntologyNodeSupplemental = "node_body_supplemental"
)
