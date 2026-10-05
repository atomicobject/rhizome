package ontology

import (
	"fmt"
	"strings"

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
	canonicalRef := func(ref NodeRef) (NodeRef, error) { return CanonicalEditRef(ref) }
	switch typed := op.(type) {
	case setFieldOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case setRawFrontmatterListOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case addEmbeddedNodeOp:
		ref, err := canonicalRef(typed.ParentRef)
		typed.ParentRef = ref
		return typed, err
	case addSectionFieldOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case deleteNodeOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case reorderCollectionOp:
		ref, err := canonicalRef(typed.ParentRef)
		typed.ParentRef = ref
		return typed, err
	case setNarrativeOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case insertNarrativeOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case ensureBlockIDOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case setBlockIDOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case removeBlockIDOp:
		ref, err := canonicalRef(typed.Ref)
		typed.Ref = ref
		return typed, err
	case rewriteFileOp:
		path, err := CanonicalEditPath(typed.NotePathValue)
		typed.NotePathValue = path.String()
		return typed, err
	case transformFileOp:
		path, err := CanonicalEditPath(typed.NotePathValue)
		typed.NotePathValue = path.String()
		return typed, err
	default:
		if strings.TrimSpace(op.notePath()) == "" {
			return nil, fmt.Errorf("edit operation note path is required")
		}
		return nil, fmt.Errorf("canonical edit identity is not implemented for %T", op)
	}
}
