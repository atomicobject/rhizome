package query

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

func (e *executor) resolveNodeRefTarget(ctx context.Context, target string) (ontology.NodeRef, bool, error) {
	ref, _, ok, err := e.resolveNodeRefTargetCandidates(ctx, target)
	return ref, ok, err
}

func (e *executor) resolveNodeRefTargetCandidates(ctx context.Context, target string) (ontology.NodeRef, []ontology.NodeRef, bool, error) {
	target = strings.TrimSpace(target)
	if target == "" || e.loaders == nil || e.loaders.scope == nil {
		return ontology.NodeRef{}, nil, false, nil
	}
	result, err := e.resolveTargetIdentity(ctx, target)
	if err != nil {
		return ontology.NodeRef{}, nil, false, err
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "target_ambiguous" && len(diagnostic.Candidates) > 1 {
			return ontology.NodeRef{}, sortedUniqueNodeRefs(diagnostic.Candidates), false, nil
		}
	}
	if len(result.Resolved) == 0 {
		return e.resolvePreferredIdentifierTarget(ctx, target)
	}
	ref := result.Resolved[0].Ref
	// A blank hydrated record has no catalog-backed note payload. Let the
	// identifier resolver decide whether this is a preferred ID; otherwise the
	// caller can try an indexed code path. Do not infer either result from an
	// extension.
	if strings.TrimSpace(result.Resolved[0].Record.Path) == "" && strings.TrimSpace(ref.TypeName) == "" {
		return e.resolvePreferredIdentifierTarget(ctx, target)
	}
	if !ref.IsZero() {
		claimsPreferredIdentifier, err := e.scopeResolvedNoteClaimsPreferredIdentifier(ctx, result.Resolved[0], target)
		if err != nil {
			return ontology.NodeRef{}, nil, false, err
		}
		if !claimsPreferredIdentifier {
			return ref, nil, true, nil
		}
		preferredRef, preferredCandidates, preferredOK, err := e.resolvePreferredIdentifierTarget(ctx, target)
		if err != nil {
			return ontology.NodeRef{}, nil, false, err
		}
		if preferredOK {
			preferredCandidates = append(preferredCandidates, preferredRef)
		}
		effectiveRef := ref
		if effectiveRef.TypeName == "" {
			effectiveRef.TypeName = strings.TrimSpace(result.Resolved[0].Record.TypeName)
		}
		if len(preferredCandidates) > 0 && nodeRefCandidatesRepresent(preferredCandidates, effectiveRef) {
			candidates := sortedUniqueNodeRefs(preferredCandidates)
			if len(candidates) > 1 {
				return ontology.NodeRef{}, candidates, false, nil
			}
			return candidates[0], nil, true, nil
		}
		return ref, nil, true, nil
	}
	return e.resolvePreferredIdentifierTarget(ctx, target)
}

// Query consumers need canonical identity and records, not link spelling or fix
// plans. Keep catalog resolution and ambiguity handling inside the shared scope.
func (e *executor) resolveTargetIdentity(ctx context.Context, target string) (noderead.ResolveResult, error) {
	return e.loaders.scope.Resolve(ctx, noderead.ResolveRequest{
		Targets:      []noderead.NodeTarget{{Input: target}},
		Hydrate:      noderead.HydrateOptions{Profile: noderead.HydrateSummary},
		OmitLocators: true,
	})
}

func (e *executor) scopeResolvedNoteClaimsPreferredIdentifier(ctx context.Context, resolved noderead.ResolvedNode, target string) (bool, error) {
	if e.schema == nil || resolved.Ref.Kind != ontology.NodeKindNote {
		return false, nil
	}
	var record *noteRecord
	if strings.TrimSpace(resolved.Record.Path) != "" {
		record = &noteRecord{
			Frontmatter: resolved.Record.Frontmatter,
			InlineProps: resolved.Record.InlineProps,
			TypeName:    resolved.Record.TypeName,
		}
	} else {
		var err error
		record, err = e.rootNote(ctx, resolved.Ref.NotePath)
		if err != nil {
			return false, err
		}
	}
	typeName := strings.TrimSpace(resolved.Record.TypeName)
	if typeName == "" {
		typeName = strings.TrimSpace(resolved.Ref.TypeName)
	}
	if typeName == "" && record != nil {
		typeName = strings.TrimSpace(record.TypeName)
	}
	noteType := e.schema.Types[typeName]
	if noteType == nil || noteType.Role != ontology.TypeRoleNote {
		return false, nil
	}
	field := preferredIdentifierField(noteType)
	if field == nil {
		return false, nil
	}
	return identifierValuesContain(record.fieldValues(field), target), nil
}
