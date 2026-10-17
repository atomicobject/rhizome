package ontology

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type EditOperationID string

func CanonicalEditPath(input string) (paths.NotePath, error) {
	path, err := paths.CleanNotePath(input)
	if err != nil {
		return "", fmt.Errorf("canonical edit path: %w", err)
	}
	return path, nil
}

func CanonicalEditRef(ref NodeRef) (NodeRef, error) {
	path, err := CanonicalEditPath(ref.NotePath)
	if err != nil {
		return NodeRef{}, err
	}
	ref.NotePath = path.String()
	return ref, nil
}

func canonicalizeEditOp(op editOp) (editOp, error) {
	ref, _ := op.conflictTarget()
	canonical, err := CanonicalEditRef(ref)
	if err != nil {
		return nil, err
	}
	return op.withCanonicalRef(canonical), nil
}

func (op setFieldOp) withCanonicalRef(ref NodeRef) editOp          { op.Ref = ref; return op }
func (op addEmbeddedNodeOp) withCanonicalRef(ref NodeRef) editOp   { op.ParentRef = ref; return op }
func (op addSectionFieldOp) withCanonicalRef(ref NodeRef) editOp   { op.Ref = ref; return op }
func (op deleteNodeOp) withCanonicalRef(ref NodeRef) editOp        { op.Ref = ref; return op }
func (op reorderCollectionOp) withCanonicalRef(ref NodeRef) editOp { op.ParentRef = ref; return op }
func (op setNarrativeOp) withCanonicalRef(ref NodeRef) editOp      { op.Ref = ref; return op }
func (op insertNarrativeOp) withCanonicalRef(ref NodeRef) editOp   { op.Ref = ref; return op }
func (op ensureBlockIDOp) withCanonicalRef(ref NodeRef) editOp     { op.Ref = ref; return op }
func (op setBlockIDOp) withCanonicalRef(ref NodeRef) editOp        { op.Ref = ref; return op }
func (op removeBlockIDOp) withCanonicalRef(ref NodeRef) editOp     { op.Ref = ref; return op }
func (op rewriteFileOp) withCanonicalRef(ref NodeRef) editOp {
	op.NotePathValue = ref.NotePath
	return op
}
func (op transformFileOp) withCanonicalRef(ref NodeRef) editOp {
	op.NotePathValue = ref.NotePath
	return op
}
