package ontology

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

// projectedNote keeps provider-current type resolution and one resolver together
// for assessment, dependency discovery, edges, and catalog materialization.
type projectedNote struct {
	doc        *noteDoc
	assessment *NoteAssessment
	prepared   *PreparedDocument
	resolver   *projectionResolver
	root       *NodeProjection
}

func projectResolvedNote(doc *noteDoc, schema *ontoschema.Schema, assessment *NoteAssessment) (*projectedNote, error) {
	prepared, err := prepareDocumentForDoc(doc, schema, assessment)
	if err != nil {
		return nil, err
	}
	return &projectedNote{doc: doc, assessment: assessment, prepared: prepared, resolver: prepared.markdown, root: prepared.rootProjection}, nil
}
