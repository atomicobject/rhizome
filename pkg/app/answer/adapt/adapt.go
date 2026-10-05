package adapt

import (
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// NodeRef converts canonical search provenance into the answer DTO shape.
// It intentionally copies fields only; link repair and node hydration are caller responsibilities.
func NodeRef(ref *ontology.NodeRef) *answer.NodeRef {
	if ref == nil {
		return nil
	}
	return &answer.NodeRef{
		NotePath:              ref.NotePath,
		Fragment:              ref.Fragment,
		NodeID:                ref.NodeID,
		TypeName:              ref.TypeName,
		Kind:                  string(ref.Kind),
		StartByte:             ref.StartByte,
		EndByte:               ref.EndByte,
		ParentID:              ref.ParentID,
		StructuralFingerprint: ref.Structural,
	}
}

// LinkTarget converts ontology linkability metadata into the answer DTO shape.
// The metadata is already resolved; this adapter must not inspect markdown to fill gaps.
func LinkTarget(target *ontology.NodeLinkTarget) *answer.LinkTarget {
	if target == nil {
		return nil
	}
	return &answer.LinkTarget{
		Markdown:     target.Markdown,
		Wikilink:     target.Wikilink,
		DisplayLabel: target.DisplayLabel,
		Exists:       target.Exists,
		RequiresFix:  target.RequiresFix,
		BlockID:      target.BlockID,
	}
}
