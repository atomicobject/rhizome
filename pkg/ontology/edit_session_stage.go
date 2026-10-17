package ontology

import (
	"fmt"
	"strings"
)

// SetScalarField stages a scalar field replacement for the target node.
func (s *EditSession) SetScalarField(ref NodeRef, field string, value string) error {
	return s.stage(setFieldOp{Ref: ref, Field: field, Values: []string{value}})
}

// SetScalarListField stages a scalar-list field replacement for the target node.
func (s *EditSession) SetScalarListField(ref NodeRef, field string, values []string) error {
	return s.stage(setFieldOp{
		Ref: ref, Field: field, Values: append([]string(nil), values...), RequireScalarList: true,
	})
}

// SetLinkField stages a link field replacement for the target node.
func (s *EditSession) SetLinkField(ref NodeRef, field string, values []string) error {
	return s.stage(setFieldOp{Ref: ref, Field: field, Values: append([]string(nil), values...), IsLinks: true})
}

// UnsetField stages removal of a field value while preserving the distinction
// from an explicitly empty scalar or list.
func (s *EditSession) UnsetField(ref NodeRef, field string) error {
	return s.stage(setFieldOp{Ref: ref, Field: field, Unset: true})
}

// UnsetLinkField stages removal of a link field value while preserving the
// distinction from an explicitly empty link list.
func (s *EditSession) UnsetLinkField(ref NodeRef, field string) error {
	return s.stage(setFieldOp{Ref: ref, Field: field, IsLinks: true, Unset: true})
}

// SetRootMetadataList stages a provider-neutral raw list for canonical root
// metadata such as tags or aliases.
func (s *EditSession) SetRootMetadataList(ref NodeRef, field string, values []string) error {
	return s.stage(setFieldOp{
		Ref: ref, Field: strings.TrimSpace(field), Values: append([]string(nil), values...),
		RawRootMetadata: true, RequireScalarList: true,
	})
}

// UnsetRootMetadataList stages removal of provider-neutral canonical root
// metadata while preserving the distinction from a present empty list.
func (s *EditSession) UnsetRootMetadataList(ref NodeRef, field string) error {
	return s.stage(setFieldOp{
		Ref: ref, Field: strings.TrimSpace(field), RawRootMetadata: true,
		RequireScalarList: true, Unset: true,
	})
}

// SetRootMetadataListWithWitness stages a provider-neutral list while retaining
// the exact value observed by the editor so concurrent metadata changes conflict.
func (s *EditSession) SetRootMetadataListWithWitness(ref NodeRef, field string, values, expected []string, expectedKind string, unset bool) error {
	return s.stage(setFieldOp{
		Ref: ref, Field: strings.TrimSpace(field), Values: append([]string(nil), values...),
		RawRootMetadata: true, RequireScalarList: true, Unset: unset,
		ExpectedValues: append([]string(nil), expected...), ExpectedValueKind: expectedKind, ExpectedCaptured: true,
	})
}

// AddEmbeddedNode stages insertion of a new embedded node into a section collection.
func (s *EditSession) AddEmbeddedNode(parentRef NodeRef, collectionField string, heading string, body string, blockID string) error {
	return s.stage(addEmbeddedNodeOp{
		ParentRef: parentRef,
		Field:     collectionField,
		Heading:   heading,
		Body:      body,
		BlockID:   blockID,
	})
}

// DeleteNode stages removal of one section or embedded node.
func (s *EditSession) DeleteNode(ref NodeRef) error {
	return s.stage(deleteNodeOp{Ref: ref})
}

// ReorderCollection stages a new order for a section-backed collection.
func (s *EditSession) ReorderCollection(parentRef NodeRef, field string, orderedFragments []string) error {
	return s.stage(reorderCollectionOp{
		ParentRef:        parentRef,
		Field:            field,
		OrderedFragments: append([]string(nil), orderedFragments...),
	})
}

// RewriteFile stages a whole-file content replacement for the given note.
// This is the escape hatch for transformations that compute the full updated
// text outside the node-level op model.
func (s *EditSession) RewriteFile(notePath, newContent string) error {
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return fmt.Errorf("rewrite file: notePath is required")
	}
	return s.stage(rewriteFileOp{NotePathValue: notePath, NewContent: newContent})
}

// StageFileTransform stages a function that computes the new file content from
// the current in-plan content at apply time. This lets callers (like validate
// fix appliers) compose with earlier ops on the same file without freezing
// text at stage time. Transforms for different files may run concurrently while
// planning, so callers must make captured state concurrency-safe.
func (s *EditSession) StageFileTransform(notePath string, transform func(content string) (string, error)) error {
	notePath = strings.TrimSpace(notePath)
	if notePath == "" {
		return fmt.Errorf("transform file: notePath is required")
	}
	if transform == nil {
		return fmt.Errorf("transform file: transform is required")
	}
	return s.stage(transformFileOp{NotePathValue: notePath, Transform: transform})
}

// SetNarrative stages a source-preserving markdown replacement for one
// narrative body block within the target node.
func (s *EditSession) SetNarrative(ref NodeRef, rangeStart, rangeEnd int, previousMarkdown, markdown string) error {
	return s.stage(setNarrativeOp{
		Ref:              ref,
		Range:            ByteRange{Start: rangeStart, End: rangeEnd},
		PreviousMarkdown: previousMarkdown,
		Markdown:         markdown,
	})
}

// InsertNarrative inserts the first narrative block at the canonical body
// boundary owned by ref. It fails closed once that node has authored body
// content, allowing the caller to retry through SetNarrative instead.
func (s *EditSession) InsertNarrative(ref NodeRef, markdown string) error {
	if strings.TrimSpace(markdown) == "" {
		return nil
	}
	return s.stage(insertNarrativeOp{Ref: ref, Markdown: strings.TrimRight(markdown, " \t\n")})
}

// EnsureBlockID stages a source-preserving block ID insertion for an embedded node.
func (s *EditSession) EnsureBlockID(ref NodeRef, blockID string) error {
	op := ensureBlockIDOp{
		Ref:     ref,
		BlockID: strings.TrimSpace(strings.TrimPrefix(blockID, "^")),
	}
	for _, existing := range s.ops {
		typed, ok := existing.(ensureBlockIDOp)
		if !ok {
			continue
		}
		if typed.Ref.String() == op.Ref.String() && typed.Ref.NodeID == op.Ref.NodeID && typed.Ref.Structural == op.Ref.Structural && typed.BlockID == op.BlockID {
			return nil
		}
	}
	return s.stage(op)
}

// SetBlockID stages a source-preserving block ID replacement for an embedded node.
func (s *EditSession) SetBlockID(ref NodeRef, blockID string) error {
	return s.stage(setBlockIDOp{
		Ref:     ref,
		BlockID: strings.TrimSpace(strings.TrimPrefix(blockID, "^")),
	})
}

// RemoveBlockID stages a source-preserving deletion of an embedded node's
// `^block-id` anchor. Idempotent: if the node has no anchor the apply is a
// no-op. Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us4]]
func (s *EditSession) RemoveBlockID(ref NodeRef) error {
	return s.stage(removeBlockIDOp{Ref: ref})
}

func (s *EditSession) stage(op editOp) error {
	if s == nil {
		return fmt.Errorf("edit session is nil")
	}
	canonical, err := canonicalizeEditOp(op)
	if err != nil {
		return err
	}
	s.ops = append(s.ops, canonical)
	return nil
}
