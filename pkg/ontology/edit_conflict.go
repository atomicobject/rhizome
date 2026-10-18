package ontology

import "errors"

// ErrEditSourceChanged reports that an edit transform's captured source is stale.
var ErrEditSourceChanged = errors.New("source changed")

func (op setFieldOp) conflictTarget() (NodeRef, string)          { return op.Ref, op.Field }
func (op addEmbeddedNodeOp) conflictTarget() (NodeRef, string)   { return op.ParentRef, op.Field }
func (op addSectionFieldOp) conflictTarget() (NodeRef, string)   { return op.Ref, op.FieldPath }
func (op reorderCollectionOp) conflictTarget() (NodeRef, string) { return op.ParentRef, op.Field }
func (op deleteNodeOp) conflictTarget() (NodeRef, string)        { return op.Ref, "" }
func (op setNarrativeOp) conflictTarget() (NodeRef, string)      { return op.Ref, "" }
func (op insertNarrativeOp) conflictTarget() (NodeRef, string)   { return op.Ref, "" }
func (op ensureBlockIDOp) conflictTarget() (NodeRef, string)     { return op.Ref, "" }
func (op setBlockIDOp) conflictTarget() (NodeRef, string)        { return op.Ref, "" }
func (op removeBlockIDOp) conflictTarget() (NodeRef, string)     { return op.Ref, "" }
func (op rewriteFileOp) conflictTarget() (NodeRef, string) {
	return NodeRef{NotePath: op.NotePathValue, Kind: NodeKindNote}, ""
}
func (op transformFileOp) conflictTarget() (NodeRef, string) {
	return NodeRef{NotePath: op.NotePathValue, Kind: NodeKindNote}, ""
}
