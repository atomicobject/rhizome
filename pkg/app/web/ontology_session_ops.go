package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
	"time"
)

func cloneOntologyEditOps(in []OntologyEditOp) []OntologyEditOp {
	if len(in) == 0 {
		return nil
	}
	out := make([]OntologyEditOp, 0, len(in))
	for _, op := range in {
		cloned := op
		cloned.Values = append([]string(nil), op.Values...)
		cloned.OrderedFragments = append([]string(nil), op.OrderedFragments...)
		if op.FieldValue != nil {
			fieldValue := *op.FieldValue
			fieldValue.Items = append([]string(nil), op.FieldValue.Items...)
			cloned.FieldValue = &fieldValue
		}
		if op.Expected != nil {
			expected := *op.Expected
			if op.Expected.Field != nil {
				fieldValue := *op.Expected.Field
				fieldValue.Items = append([]string(nil), op.Expected.Field.Items...)
				expected.Field = &fieldValue
			}
			cloned.Expected = &expected
		}
		out = append(out, cloned)
	}
	return out
}

func validateOntologyEditSnapshotBases(snapshot *OntologyEditSessionSnapshot) error {
	if snapshot == nil {
		return nil
	}
	wanted := make(map[string]struct{})
	for _, op := range snapshot.Ops {
		raw := strings.TrimSpace(strings.SplitN(op.Path, "#", 2)[0])
		clean, err := paths.CleanNotePath(raw)
		if err != nil {
			return fmt.Errorf("invalid snapshot operation path %q: %w", raw, err)
		}
		wanted[clean.String()] = struct{}{}
	}
	seen := make(map[string]struct{}, len(snapshot.BaseDocuments))
	for _, base := range snapshot.BaseDocuments {
		clean, err := paths.CleanNotePath(base.NotePath)
		if err != nil {
			return fmt.Errorf("invalid snapshot base path %q: %w", base.NotePath, err)
		}
		if _, duplicate := seen[clean.String()]; duplicate {
			return fmt.Errorf("snapshot contains multiple bases for %s", clean.String())
		}
		verified, err := buildMarkdownDocumentSnapshotCompat(clean.String(), base.Content, time.Time{})
		if err != nil || verified.ContentFingerprint != base.Fingerprint {
			return fmt.Errorf("snapshot base fingerprint mismatch for %s", clean.String())
		}
		seen[clean.String()] = struct{}{}
	}
	for notePath := range wanted {
		if _, ok := seen[notePath]; !ok {
			return fmt.Errorf("snapshot is missing the verified base for %s", notePath)
		}
	}
	return nil
}

func (s *Server) canonicalizeOntologyEditOps(ctx context.Context, defs *ontologyDefinitions, ops []OntologyEditOp) ([]OntologyEditOp, error) {
	// Docs: [[ontology-edit-replay-conflict-contract]]
	// requires web sessions to persist canonical node identity in addition to
	// the author-facing locator that came from the UI.
	out := cloneOntologyEditOps(ops)
	nodeScope := s.nodeReadScope(ctx, defs)
	for i := range out {
		ref, err := ontologyNodeRefFromOp(out[i])
		if err != nil {
			return nil, err
		}
		cleanPath, err := paths.CleanNotePath(ref.NotePath)
		if err != nil {
			return nil, err
		}
		ref.NotePath = cleanPath.String()
		out[i].Path = ref.NotePath
		if ref.Fragment != "" {
			out[i].Path += "#" + strings.TrimPrefix(ref.Fragment, "#")
		}
		needsProjection := ref.Kind != ontology.NodeKindNote || strings.TrimSpace(ref.Fragment) != "" || strings.TrimSpace(out[i].Kind) == "setLinkField"
		if !needsProjection {
			continue
		}
		projection, err := s.resolveNodeProjection(ctx, defs, nodeScope, ref)
		if err != nil {
			return nil, err
		}
		out[i].NodeID = projection.Ref.NodeID
		out[i].Structural = projection.Ref.Structural
		if strings.TrimSpace(out[i].Kind) == "setLinkField" {
			if err := s.validateOntologyEditLinkTargets(ctx, defs, nodeScope, projection, out[i]); err != nil {
				return nil, err
			}
		}
	}
	for i := range out {
		ensureOntologyEditOpID(&out[i])
	}
	return out, nil
}

func (s *Server) validateOntologyEditLinkTargets(ctx context.Context, defs *ontologyDefinitions, scope *noderead.Scope, owner *ontology.NodeProjection, op OntologyEditOp) error {
	if owner == nil || owner.Type == nil {
		return fmt.Errorf("relation owner has no resolved ontology type")
	}
	field := owner.Type.ByName[strings.TrimSpace(op.Field)]
	if field == nil || field.Kind != ontoschema.FieldKindLink {
		return fmt.Errorf("field %s is not a relation on %s", op.Field, owner.Ref.String())
	}
	values, mode, err := ontologyEditFieldValues(op)
	if err != nil || mode == "unset" {
		return err
	}
	for _, value := range values {
		target := strings.TrimSpace(value)
		resolverInput := target
		if !strings.HasPrefix(resolverInput, "[[") || !strings.HasSuffix(resolverInput, "]]") {
			resolverInput = "[[" + resolverInput + "]]"
		}
		resolved, err := scope.Resolve(ctx, noderead.ResolveRequest{
			Targets:  []noderead.NodeTarget{{Input: resolverInput}},
			FromPath: owner.Ref.NotePath,
			Hydrate:  noderead.HydrateOptions{Profile: noderead.HydrateSummary},
		})
		if err != nil {
			return fmt.Errorf("resolve relation target %q: %w", value, err)
		}
		if len(resolved.Resolved) != 1 {
			return fmt.Errorf("relation target %q did not resolve uniquely", value)
		}
		canonical := resolved.Resolved[0]
		projection, err := s.resolveNodeProjection(ctx, defs, scope, canonical.Ref)
		if err != nil {
			return fmt.Errorf("resolve relation target %q: %w", value, err)
		}
		targetType := projection.Type
		if targetType == nil {
			targetType = defs.schema.Types[canonical.Record.TypeName]
		}
		actualType := canonical.Record.TypeName
		if targetType != nil {
			actualType = targetType.Name
		}
		if !ontoschema.TypeMatchesOrImplements(defs.schema, actualType, field.TypeName) {
			resolvedType := canonical.Record.TypeName
			if targetType != nil {
				resolvedType = targetType.Name
			}
			return fmt.Errorf("relation field %s requires %s, but %q resolves to %s", field.Name, field.TypeName, value, resolvedType)
		}
	}
	return nil
}

func ensureOntologyEditOpID(op *OntologyEditOp) {
	if op == nil || strings.TrimSpace(op.ID) != "" {
		return
	}
	identity := ontologyEditOperationTarget(*op)
	switch strings.TrimSpace(op.Kind) {
	case "setField", "setLinkField", "setNarrative", "setSource":
	default:
		encoded, _ := json.Marshal(op)
		identity = string(encoded)
	}
	sum := sha256.Sum256([]byte(identity))
	op.ID = "op-" + hex.EncodeToString(sum[:8])
}

// canonicalizeOntologyEditOpsForSessionLocked requires session.mu. The caller
// keeps that lock through appending the canonical operations so preview lineage
// cannot be applied to a different session state.
func (s *Server) canonicalizeOntologyEditOpsForSessionLocked(ctx context.Context, defs *ontologyDefinitions, session *ontologyEditSession, ops []OntologyEditOp) ([]OntologyEditOp, error) {
	out := make([]OntologyEditOp, 0, len(ops))
	for _, op := range ops {
		canonical, err := s.canonicalizeOntologyEditOps(ctx, defs, []OntologyEditOp{op})
		if err == nil {
			out = append(out, canonical[0])
			continue
		}
		kind := strings.TrimSpace(op.Kind)
		if kind != "setField" && kind != "setLinkField" && kind != "setRootMetadataList" {
			return nil, err
		}
		projection, lineage, previewScope, previewErr := s.projectOntologyEditOpFromSessionLocked(ctx, defs, session, op)
		if previewErr != nil {
			return nil, err
		}
		if kind == "setLinkField" {
			if validationErr := s.validateOntologyEditLinkTargets(ctx, defs, previewScope, projection, op); validationErr != nil {
				return nil, validationErr
			}
		}
		baseRef, ok := matchingSessionBaseRef(lineage, projection.Ref)
		if !ok {
			return nil, err
		}
		canonicalOp := op
		canonicalOp.Path = baseRef.NotePath
		if baseRef.Fragment != "" {
			canonicalOp.Path += "#" + baseRef.Fragment
		}
		canonicalOp.NodeID = baseRef.NodeID
		canonicalOp.Structural = baseRef.Structural
		ensureOntologyEditOpID(&canonicalOp)
		out = append(out, canonicalOp)
	}
	return out, nil
}

func (s *Server) projectOntologyEditOpFromSessionLocked(ctx context.Context, defs *ontologyDefinitions, session *ontologyEditSession, op OntologyEditOp) (*ontology.NodeProjection, []ontology.PreviewRefLineage, *noderead.Scope, error) {
	if session == nil || defs == nil || defs.schema == nil {
		return nil, nil, nil, fmt.Errorf("edit session preview is unavailable")
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return nil, nil, nil, err
	}
	if session.Editor == nil {
		return nil, nil, nil, fmt.Errorf("edit session preview is unavailable")
	}
	plan, lineage, err := session.Editor.PreviewWithRefLineage(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	overlay := s.markdownReadOverlayFromCommitPlan(plan, OntologyEditSessionStatusDirty, false)
	if overlay == nil {
		return nil, nil, nil, fmt.Errorf("edit session preview does not include %s", ref.NotePath)
	}
	scope := s.nodeReadScopeWithOverlay(ctx, defs, overlay)
	projection, err := scope.Projection(ctx, ref)
	if err == nil && (projection == nil || projection.Ref.IsZero()) {
		err = fmt.Errorf("edit session preview did not resolve %s", ref.String())
	}
	return projection, lineage, scope, err
}

func matchingSessionBaseRef(lineage []ontology.PreviewRefLineage, current ontology.NodeRef) (ontology.NodeRef, bool) {
	var match ontology.NodeRef
	for _, entry := range lineage {
		candidate := entry.Original
		preview := entry.Preview
		if candidate.NotePath != current.NotePath || candidate.Kind != current.Kind ||
			preview.NotePath != current.NotePath || preview.Kind != current.Kind ||
			strings.TrimSpace(preview.Structural) == "" || preview.Structural != current.Structural {
			continue
		}
		if match.NotePath != "" && (match.NodeID != candidate.NodeID || match.Structural != candidate.Structural || match.Fragment != candidate.Fragment) {
			return ontology.NodeRef{}, false
		}
		match = candidate
	}
	return match, match.NotePath != ""
}

func (s *Server) reduceOntologyEditOps(ctx context.Context, defs *ontologyDefinitions, ops []OntologyEditOp) ([]OntologyEditOp, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	nodeScope := s.nodeReadScope(ctx, defs)
	out := make([]OntologyEditOp, 0, len(ops))
	replaceable := map[string]int{}
	for _, op := range ops {
		op = cloneOntologyEditOps([]OntologyEditOp{op})[0]
		key, ok := replaceableOntologyEditOpKey(op)
		if !ok {
			out = append(out, op)
			continue
		}
		if index, exists := replaceable[key]; exists {
			previous := out[index]
			if strings.TrimSpace(op.ID) != "" && ontologyEditOperationTarget(previous) != ontologyEditOperationTarget(op) {
				return nil, fmt.Errorf("operation id %q was reused for a different edit target", op.ID)
			}
			if previous.PreviousMarkdown != "" {
				op.PreviousMarkdown = previous.PreviousMarkdown
			}
			if previous.Expected != nil {
				op.Expected = previous.Expected
			}
			out[index] = op
			continue
		}
		replaceable[key] = len(out)
		out = append(out, op)
	}
	pruned := out[:0]
	for _, op := range out {
		if ok, err := s.ontologyEditOpMatchesCurrentValue(ctx, defs, nodeScope, op); err != nil {
			return nil, err
		} else if ok {
			continue
		}
		pruned = append(pruned, op)
	}
	if len(pruned) == 0 {
		return nil, nil
	}
	return cloneOntologyEditOps(pruned), nil
}

func ontologyEditOperationTarget(op OntologyEditOp) string {
	ref, _ := ontologyNodeRefFromOp(op)
	parts := []string{
		strings.TrimSpace(op.Kind), canonicalNodeRefKey(ref), strings.TrimSpace(op.Field),
		strings.TrimSpace(op.Collection),
	}
	if strings.TrimSpace(op.Kind) != "setNarrative" {
		parts = append(parts, fmt.Sprintf("%d:%d", op.RangeStart, op.RangeEnd))
	}
	return strings.Join(parts, "|")
}

func attachOntologyConflictOperationIDs(conflicts []ontology.ConflictReport, ops []OntologyEditOp) []ontology.ConflictReport {
	attached := append([]ontology.ConflictReport(nil), conflicts...)
	for index := range attached {
		if attached[index].OperationID != "" {
			continue
		}
		for _, op := range ops {
			if strings.TrimSpace(op.ID) == "" || modifiedOpNotePath(op) != attached[index].NotePath {
				continue
			}
			if attached[index].Field != "" && strings.TrimSpace(op.Field) != attached[index].Field {
				continue
			}
			if attached[index].NodeRef != "" {
				ref, err := ontologyNodeRefFromOp(op)
				if err != nil || ref.String() != attached[index].NodeRef {
					continue
				}
			}
			attached[index].OperationID = op.ID
			break
		}
	}
	return attached
}

func replaceableOntologyEditOpKey(op OntologyEditOp) (string, bool) {
	if id := strings.TrimSpace(op.ID); id != "" {
		return "id|" + id, true
	}
	kind := strings.TrimSpace(op.Kind)
	switch kind {
	case "setField", "setLinkField", "setRootMetadataList":
	default:
		return "", false
	}
	field := strings.TrimSpace(op.Field)
	if field == "" {
		return "", false
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return "", false
	}
	return strings.Join([]string{kind, canonicalNodeRefKey(ref), field}, "|"), true
}

func (s *Server) ontologyEditOpMatchesCurrentValue(ctx context.Context, defs *ontologyDefinitions, nodeScope *noderead.Scope, op OntologyEditOp) (bool, error) {
	kind := strings.TrimSpace(op.Kind)
	if kind != "setField" && kind != "setLinkField" && kind != "setRootMetadataList" {
		return false, nil
	}
	field := strings.TrimSpace(op.Field)
	if field == "" {
		return false, nil
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return false, nil
	}
	projection, err := s.resolveNodeProjection(ctx, defs, nodeScope, ref)
	if err != nil {
		return false, nil
	}
	binding, ok := projection.Fields[field]
	if !ok {
		if strings.EqualFold(field, "title") {
			return len(ontologyEditOpValues(op)) == 1 && oneLineOntologyEditTitleValue(ontologyEditOpValues(op)[0]) == projectedOntologyEditTitle(projection), nil
		}
		return false, nil
	}
	desiredKind := ontologyEditOpFieldValueKind(op)
	if desiredKind == "unset" {
		return !binding.Present, nil
	}
	currentValues := binding.Values
	if desiredKind == "scalar" && binding.Present && len(currentValues) == 0 {
		currentValues = []string{""}
	}
	return binding.Present && stringSlicesEqual(currentValues, ontologyEditOpValues(op)), nil
}

func ontologyEditOpFieldValueKind(op OntologyEditOp) string {
	if op.FieldValue != nil {
		switch strings.ToLower(strings.TrimSpace(op.FieldValue.Kind)) {
		case "unset", "scalar", "list":
			return strings.ToLower(strings.TrimSpace(op.FieldValue.Kind))
		}
	}
	if strings.TrimSpace(op.Kind) == "setLinkField" || strings.TrimSpace(op.Kind) == "setRootMetadataList" || op.Values != nil {
		return "list"
	}
	return "scalar"
}

func ontologyEditOpValues(op OntologyEditOp) []string {
	if op.FieldValue != nil {
		switch strings.ToLower(strings.TrimSpace(op.FieldValue.Kind)) {
		case "unset":
			return nil
		case "scalar":
			return []string{op.FieldValue.Scalar}
		case "list":
			return append([]string(nil), op.FieldValue.Items...)
		}
	}
	if strings.TrimSpace(op.Kind) == "setLinkField" || strings.TrimSpace(op.Kind) == "setRootMetadataList" || op.Values != nil {
		return append([]string(nil), op.Values...)
	}
	return []string{op.Value}
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
