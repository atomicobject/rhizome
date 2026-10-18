package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
	"time"
)

func projectState(state *documentState, ref NodeRef) (*DocumentSnapshot, *NodeProjection, error) {
	if rootOnly, err := state.providerRootOnly(); err != nil {
		return nil, nil, err
	} else if rootOnly {
		if ref.Kind != NodeKindNote || strings.TrimSpace(ref.Fragment) != "" || strings.TrimSpace(ref.NodeID) != "" {
			return nil, nil, fmt.Errorf("%w: provider exposes only the note root", errUnsupportedTarget)
		}
		_, _, projection, err := state.projectProviderRoot(state.content)
		if err != nil {
			return nil, nil, err
		}
		return &DocumentSnapshot{
			NotePath: ref.NotePath, Content: state.content, ContentFingerprint: hashText(state.content),
		}, projection, nil
	}
	snapshot, err := state.documentSnapshot()
	if err != nil {
		return nil, nil, err
	}
	projection, err := projectSnapshot(snapshot, state.schema, ref, state.allowSelectorRecovery)
	if err != nil {
		return nil, nil, err
	}
	return snapshot, projection, nil
}

func (s *documentState) documentSnapshot() (*DocumentSnapshot, error) {
	if s.snapshot != nil && s.snapshotFor == s.content {
		return s.snapshot, nil
	}
	snapshot, err := BuildDocumentSnapshot(s.notePath, s.content, time.Time{})
	if err != nil {
		return nil, err
	}
	s.snapshot = snapshot
	s.snapshotFor = s.content
	return snapshot, nil
}

func projectSnapshot(snapshot *DocumentSnapshot, schema *ontoschema.Schema, ref NodeRef, allowSelectorRecovery bool) (*NodeProjection, error) {
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, ref)
	if err != nil {
		return nil, err
	}
	if allowSelectorRecovery && projection.Type == nil && strings.TrimSpace(ref.TypeName) != "" {
		recovered, recoveryErr := ProjectNodeFromSnapshotWithSelectorRecovery(snapshot, schema, ref)
		if recoveryErr != nil {
			return nil, recoveryErr
		}
		if recovered.Type == nil {
			return nil, fmt.Errorf("missing selector-matched note projection for %s", ref.String())
		}
		projection = recovered
	}
	if err := validateResolvedProjectionRef(ref, projection.Ref); err != nil {
		return nil, err
	}
	return projection, nil
}

func validateResolvedProjectionRef(requested NodeRef, resolved NodeRef) error {
	// WHY: author-facing locators may survive unrelated insertions, but replay
	// must stop when the resolved canonical node identity no longer matches the
	// captured operation. See [[ontology-edit-replay-conflict-contract]].
	// A note root is identified by its path; its structure fingerprint hashes
	// section byte offsets, so any earlier staged edit would move it.
	if requested.Kind == NodeKindNote && resolved.Kind == NodeKindNote {
		return nil
	}
	if requested.Structural != "" &&
		strings.TrimSpace(requested.Structural) != strings.TrimSpace(resolved.Structural) {
		if requested.Kind == NodeKindEmbedded && strings.TrimSpace(resolved.TypeName) != "" {
			if start, ok := sourceSpanStartFromFragment(normalizeSectionFragment(requested.Fragment, requested.NotePath)); ok && start == resolved.StartByte {
				return nil
			}
		}
		return fmt.Errorf("%w: structure changed for %s", errMissingNode, requested.String())
	}
	if requested.Structural == "" && requested.NodeID != "" &&
		strings.TrimSpace(requested.NodeID) != strings.TrimSpace(resolved.NodeID) {
		return fmt.Errorf("%w: node id changed for %s", errMissingNode, requested.String())
	}
	return nil
}
