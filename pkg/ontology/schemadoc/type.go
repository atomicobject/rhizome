package schemadoc

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
)

// Type returns the presentation for one named type. An empty name has no subject.
func Type(schema *ontoschema.Schema, typeName, personType string) (*TypeDoc, error) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	docs, err := SchemaDocs(schema, typeName, personType)
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	return &docs[0], nil
}
