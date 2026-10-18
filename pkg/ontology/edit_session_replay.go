package ontology

import (
	"fmt"
	"strings"
)

func applyEditOps(state *documentState, ops []editOp) *editApplyError {
	for i := 0; i < len(ops); {
		if _, ok := ops[i].(setFieldOp); ok {
			start := i
			for i < len(ops) {
				if _, ok := ops[i].(setFieldOp); !ok {
					break
				}
				i++
			}
			if err := applySetFieldOps(state, ops[start:i]); err != nil {
				return err
			}
			continue
		}
		if rootOnly, err := state.providerRootOnly(); err != nil {
			return &editApplyError{op: ops[i], err: err}
		} else if rootOnly {
			return &editApplyError{op: ops[i], err: fmt.Errorf("%w: source format permits root metadata edits only", errUnsupportedTarget)}
		}
		if state.captureRefLineage && len(state.refLineage) > 0 {
			// Structural edits after a captured field target can delete, replace,
			// or manufacture an indistinguishable node. Refuse to carry lineage
			// across that boundary without stronger operation-specific evidence.
			state.refLineage = nil
		}
		var beforeStructural map[string]int
		if state.captureRefLineage {
			beforeStructural = documentStructuralCounts(state)
		}
		if err := ops[i].apply(state); err != nil {
			return &editApplyError{op: ops[i], err: err}
		}
		if state.captureRefLineage {
			state.recordStructuralInvalidations(beforeStructural, documentStructuralCounts(state), ops[i])
		}
		i++
	}
	return nil
}

func applySetFieldOps(state *documentState, ops []editOp) *editApplyError {
	if rootOnly, err := state.providerRootOnly(); err != nil {
		return &editApplyError{op: ops[0], err: err}
	} else if rootOnly {
		typed := make([]setFieldOp, len(ops))
		for index, op := range ops {
			typed[index] = op.(setFieldOp)
		}
		failed, applyErr := applyProviderRootFieldOps(state, typed)
		if applyErr != nil {
			if failed == nil {
				failed = ops[0]
			}
			return &editApplyError{op: failed, err: applyErr}
		}
		return nil
	}
	if len(ops) == 1 && !state.captureRefLineage {
		if err := ops[0].apply(state); err != nil {
			return &editApplyError{op: ops[0], err: err}
		}
		return nil
	}
	typed := make([]setFieldOp, 0, len(ops))
	for _, op := range ops {
		typed = append(typed, op.(setFieldOp))
	}
	updated, targets, edits, ok, failedOp, err := tryApplySetFieldOpsBatch(state, typed)
	if err != nil {
		return &editApplyError{op: failedOp, err: err}
	}
	if ok {
		state.setContent(updated)
		if state.captureRefLineage {
			captureFieldRefLineage(state, targets, edits)
		}
		return nil
	}
	bindings := make(map[NodeRef]NodeRef)
	for _, rawOp := range ops {
		original := rawOp.(setFieldOp)
		effective := original
		if rebound, exists := bindings[original.Ref]; exists {
			effective.Ref = rebound
		}
		_, projection, projectionErr := projectState(state, effective.Ref)
		if projectionErr != nil {
			return &editApplyError{op: rawOp, err: projectionErr}
		}
		before := projection.Ref
		if err := effective.apply(state); err != nil {
			return &editApplyError{op: rawOp, err: err}
		}
		preview, ok := fieldReplaySuccessor(state, before)
		if !ok {
			delete(bindings, original.Ref)
			continue
		}
		bindings[original.Ref] = preview
		recordFieldRefLineage(state, original.Ref, preview)
	}
	return nil
}

// fieldReplaySuccessor binds a node only after its original identity resolved
// strictly and this replay changed it. The pre-edit byte position is evidence
// local to that mutation; it is never used to recover an initially stale ref.
func fieldReplaySuccessor(state *documentState, before NodeRef) (NodeRef, bool) {
	snapshot, err := state.documentSnapshot()
	if err != nil {
		return NodeRef{}, false
	}
	resolver, err := newProjectionResolver(snapshot, state.schema)
	if err != nil {
		return NodeRef{}, false
	}
	preview, ok := replayRefAtStart(replayRefsByStart(resolver)[before.StartByte], before.Kind, before.TypeName)
	if !ok || (strings.TrimSpace(preview.Structural) == "" && strings.TrimSpace(preview.NodeID) == "") {
		return NodeRef{}, false
	}
	return preview, true
}

func recordFieldRefLineage(state *documentState, original, preview NodeRef) {
	if state == nil || !state.captureRefLineage || strings.TrimSpace(preview.Structural) == "" {
		return
	}
	if state.invalidRefStructural[strings.TrimSpace(original.Structural)] {
		return
	}
	for i := range state.refLineage {
		if state.refLineage[i].Original == original {
			state.refLineage[i].Preview = preview
			return
		}
	}
	state.refLineage = append(state.refLineage, PreviewRefLineage{Original: original, Preview: preview})
}

type contentEdit struct {
	r           ByteRange
	replacement string
	order       int
}

type fieldReplayTarget struct {
	Original NodeRef
	Before   NodeRef
}
