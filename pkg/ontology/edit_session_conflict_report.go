package ontology

import "errors"

func conflictFromError(notePath string, op editOp, err error) ConflictReport {
	ref, field := op.conflictTarget()
	kind := ConflictKindUnsupportedTarget
	switch {
	case errors.Is(err, errMissingNode):
		kind = ConflictKindMissingNode
	case errors.Is(err, errMissingField):
		kind = ConflictKindMissingField
	case errors.Is(err, errCollectionDrift):
		kind = ConflictKindCollectionDrift
	case errors.Is(err, ErrEditSourceChanged):
		kind = ConflictKindSourceChanged
	}
	return ConflictReport{Kind: kind, NotePath: notePath, NodeRef: ref.String(), Field: field, Message: err.Error()}
}
