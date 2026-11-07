package ontology

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/noteformat"
)

func (p *PreparedDocument) recordShapeObservation(id noteformat.ValueID, step, expected string) {
	key := fmt.Sprintf("%d:%s:%s", id, step, expected)
	if p.shapeObservations == nil {
		p.shapeObservations = map[string]bool{}
	}
	if p.shapeObservations[key] {
		return
	}
	p.shapeObservations[key] = true
	owner := p.owners[p.ownerByValue[id]]
	if owner == nil {
		return
	}
	p.diagnostics = append(p.diagnostics, RecordDiagnostic{Code: "record_selector_shape", Owner: owner.Key, Address: owner.Address, Message: fmt.Sprintf("selector step %q expected %s", step, expected)})
}
