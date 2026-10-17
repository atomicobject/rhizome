package ontology

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

// projectedNote keeps provider-current type resolution and one resolver together
// for assessment, dependency discovery, edges, and catalog materialization.
type projectedNote struct {
	doc        *noteDoc
	assessment *NoteAssessment
	resolver   *projectionResolver
	root       *NodeProjection
}

func projectResolvedNote(doc *noteDoc, schema *ontoschema.Schema, assessment *NoteAssessment) (*projectedNote, error) {
	note := &projectedNote{doc: doc, assessment: assessment}
	if doc.Snapshot != nil {
		resolver, err := newProjectionResolverForDoc(doc.Snapshot, schema, doc, assessment)
		if err != nil {
			return nil, err
		}
		note.resolver = resolver
	}
	root, err := projectRootNodeWithAssessment(doc, schema, assessment, note.resolver)
	if err != nil {
		return nil, err
	}
	note.root = root
	return note, nil
}
