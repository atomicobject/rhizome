package ontology

import (
	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

func ontologyReadinessPublication(schema *ontoschema.Schema, formats noteformat.Runtime, schemaState semdb.OntologySchemaState, metadataState semdb.NoteMetadataState) *semdb.OntologyReadinessPublication {
	return &semdb.OntologyReadinessPublication{
		SchemaState:   schemaState,
		MetadataState: metadataState,
		ProviderHash:  OntologyProviderHash(formats),
		Validate: func(snapshot semdb.OntologyReadinessSnapshot) (bool, error) {
			return ontologySnapshotMaterialized(schema, formats, ontologyMaterializationFromReadiness(snapshot))
		},
	}
}
