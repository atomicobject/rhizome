package web

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/validate"
	"strings"
)

func (s *Server) previewSessionEditor(ctx context.Context, session *ontologyEditSession, defs *ontologyDefinitions) (ontology.CommitPlan, []ontology.ConflictReport, error) {
	// Docs: [[ontology-edit-replay-conflict-contract]] owns rebase behavior.
	if session.Editor == nil {
		editor, err := s.replayOntologyEditOps(defs, session.Ops)
		if err != nil {
			return ontology.CommitPlan{}, nil, err
		}
		session.Editor = editor
	}
	plan, conflicts, err := session.Editor.PreviewCurrent(ctx)
	if err != nil {
		return ontology.CommitPlan{}, nil, err
	}
	session.Rebased = planHasRebased(plan)
	session.StalePaths = stalePathsFromPlan(plan)
	if len(conflicts) > 0 {
		return plan, conflicts, nil
	}
	relationConflicts, err := s.validateOntologyRelationOpsAgainstPlan(ctx, defs, session.Ops, plan)
	if err != nil {
		return ontology.CommitPlan{}, nil, err
	}
	if len(relationConflicts) > 0 {
		return plan, relationConflicts, nil
	}
	return plan, nil, nil
}

func (s *Server) validateOntologyRelationOpsAgainstPlan(ctx context.Context, defs *ontologyDefinitions, ops []OntologyEditOp, plan ontology.CommitPlan) ([]ontology.ConflictReport, error) {
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology schema is unavailable")
	}
	overlay := s.markdownReadOverlayFromCommitPlan(plan, OntologyEditSessionStatusDirty, false)
	targetScope := s.nodeReadScopeWithOverlay(ctx, defs, overlay)
	ownerScope := s.nodeReadScope(ctx, defs)
	conflicts := make([]ontology.ConflictReport, 0)
	for _, op := range ops {
		if strings.TrimSpace(op.Kind) != "setLinkField" {
			continue
		}
		ref, err := ontologyNodeRefFromOp(op)
		if err != nil {
			return nil, err
		}
		// The operation ref is canonical to the durable source. Resolve its
		// owning field there; earlier staged edits may have changed the
		// unanchored node's structural fingerprint in the overlay. Relation
		// targets still resolve against the completed plan so staged target
		// changes participate in validation.
		owner, err := ownerScope.Projection(ctx, ref)
		if err == nil {
			err = s.validateOntologyEditLinkTargets(ctx, defs, targetScope, owner, op)
		}
		if err == nil {
			continue
		}
		conflicts = append(conflicts, ontology.ConflictReport{
			Kind:        ontology.ConflictKindUnsupportedTarget,
			OperationID: op.ID,
			NotePath:    ref.NotePath,
			NodeRef:     ref.String(),
			Field:       op.Field,
			Message:     err.Error(),
		})
	}
	return conflicts, nil
}

func (s *Server) replayOntologyEditOps(defs *ontologyDefinitions, ops []OntologyEditOp) (*ontology.EditSession, error) {
	bases, err := editBaseDocumentsFromOps(ops)
	if err != nil {
		return nil, err
	}
	return s.replayOntologyEditOpsWithBases(defs, ops, bases)
}

func (s *Server) replayOntologyEditOpsWithBases(defs *ontologyDefinitions, ops []OntologyEditOp, bases []ontology.EditBaseDocument) (*ontology.EditSession, error) {
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology schema is unavailable")
	}
	editSession := s.newOntologyEditSession(defs.schema)
	for _, op := range ops {
		if err := applyOntologyEditOp(editSession, defs.schema, op); err != nil {
			return nil, err
		}
	}
	if len(bases) > 0 {
		if err := editSession.RestoreBases(bases); err != nil {
			return nil, err
		}
	}
	if len(ops) > 0 {
		if _, err := editSession.Preview(context.Background()); err != nil {
			return nil, err
		}
	}
	return editSession, nil
}

func applyOntologyEditOp(editSession *ontology.EditSession, schema *ontoschema.Schema, op OntologyEditOp) error {
	switch strings.TrimSpace(op.Kind) {
	case "setSource":
		notePath := strings.TrimSpace(op.Path)
		witness := op.PreviousMarkdown
		if op.Expected != nil {
			witness = op.Expected.SourceContent
		}
		if op.Expected == nil || strings.TrimSpace(op.Expected.SourceHash) == "" {
			return fmt.Errorf("setSource requires a verified full-file source witness")
		}
		if err := validateBoundedSourceEdit(witness, op.Markdown, schema); err != nil {
			return err
		}
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			if content != witness {
				return "", fmt.Errorf("source changed while applying setSource for %s", notePath)
			}
			return op.Markdown, nil
		})
	case "appendAlias":
		notePath := strings.TrimSpace(op.Path)
		value := op.Value
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			return validate.ApplyAliasAppendInMemory(content, value)
		})
	case "setFrontmatter":
		notePath := strings.TrimSpace(op.Path)
		property := strings.TrimSpace(op.Property)
		if property == "" {
			property = strings.TrimSpace(op.Field)
		}
		value := op.Value
		values := append([]string(nil), op.Values...)
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			return validate.ApplySetFrontmatterInMemory(content, property, value, values)
		})
	case "rewriteBrokenLink":
		notePath := strings.TrimSpace(op.Path)
		oldTarget := op.OldTarget
		newTarget := op.NewTarget
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			updated, _ := validate.ApplyRewriteLinkInMemory(content, oldTarget, newTarget)
			return updated, nil
		})
	case "addSectionScaffold":
		notePath := strings.TrimSpace(op.Path)
		level := strings.TrimSpace(op.Level)
		if level == "" {
			level = strings.TrimSpace(op.Property)
		}
		heading := op.Value
		if strings.TrimSpace(heading) == "" {
			heading = op.Heading
		}
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			return validate.AddSectionScaffold(content, level, heading), nil
		})
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(op.Kind) {
	case "setField":
		values, mode, err := ontologyEditFieldValues(op)
		if err != nil {
			return err
		}
		field := strings.TrimSpace(op.Field)
		if op.FieldValue != nil && op.Expected != nil && op.Expected.Field != nil {
			expected, expectedMode, err := ontologyEditFieldValues(OntologyEditOp{FieldValue: op.Expected.Field})
			if err != nil {
				return fmt.Errorf("invalid expected field witness: %w", err)
			}
			return editSession.SetFieldValueWithWitness(ref, field, values, expected, expectedMode, mode == "list", false, mode == "unset")
		}
		switch mode {
		case "unset":
			return editSession.UnsetField(ref, field)
		case "list":
			return editSession.SetScalarListField(ref, field, values)
		default:
			return editSession.SetScalarField(ref, field, values[0])
		}
	case "setLinkField":
		values, mode, err := ontologyEditFieldValues(op)
		if err != nil {
			return err
		}
		field := strings.TrimSpace(op.Field)
		if op.FieldValue != nil && op.Expected != nil && op.Expected.Field != nil {
			expected, expectedMode, err := ontologyEditFieldValues(OntologyEditOp{FieldValue: op.Expected.Field})
			if err != nil {
				return fmt.Errorf("invalid expected field witness: %w", err)
			}
			return editSession.SetFieldValueWithWitness(ref, field, values, expected, expectedMode, true, true, mode == "unset")
		}
		if mode == "unset" {
			return editSession.UnsetLinkField(ref, field)
		}
		return editSession.SetLinkField(ref, field, values)
	case "setRootMetadataList":
		values, mode, err := ontologyEditFieldValues(op)
		if err != nil {
			return err
		}
		if op.FieldValue != nil && op.Expected != nil && op.Expected.Field != nil {
			expected, expectedMode, err := ontologyEditFieldValues(OntologyEditOp{FieldValue: op.Expected.Field})
			if err != nil {
				return fmt.Errorf("invalid expected field witness: %w", err)
			}
			return editSession.SetRootMetadataListWithWitness(ref, strings.TrimSpace(op.Field), values, expected, expectedMode, mode == "unset")
		}
		if mode == "unset" {
			return editSession.UnsetRootMetadataList(ref, strings.TrimSpace(op.Field))
		}
		return editSession.SetRootMetadataList(ref, strings.TrimSpace(op.Field), values)
	case "setNarrative":
		return editSession.SetNarrative(ref, op.RangeStart, op.RangeEnd, op.PreviousMarkdown, op.Markdown)
	case "insertNarrative":
		return editSession.InsertNarrative(ref, op.Markdown)
	case "ensureBlockID":
		ref.Kind = ontology.NodeKindEmbedded
		return editSession.EnsureBlockID(ref, strings.TrimSpace(op.BlockID))
	case "setBlockID":
		ref.Kind = ontology.NodeKindEmbedded
		return editSession.SetBlockID(ref, strings.TrimSpace(op.BlockID))
	case "addEmbeddedNode":
		return editSession.AddEmbeddedNode(ref, strings.TrimSpace(op.Collection), strings.TrimSpace(op.Heading), op.Body, strings.TrimSpace(op.BlockID))
	case "deleteNode":
		return editSession.DeleteNode(ref)
	case "reorderCollection":
		return editSession.ReorderCollection(ref, strings.TrimSpace(op.Collection), append([]string(nil), op.OrderedFragments...))
	default:
		return fmt.Errorf("unsupported ontology edit op kind %q", op.Kind)
	}
}

func ontologyEditFieldValues(op OntologyEditOp) ([]string, string, error) {
	if op.FieldValue != nil {
		switch strings.ToLower(strings.TrimSpace(op.FieldValue.Kind)) {
		case "unset":
			return nil, "unset", nil
		case "scalar":
			return []string{op.FieldValue.Scalar}, "scalar", nil
		case "list":
			return append([]string(nil), op.FieldValue.Items...), "list", nil
		default:
			return nil, "", fmt.Errorf("unsupported ontology edit field value kind %q", op.FieldValue.Kind)
		}
	}
	if strings.TrimSpace(op.Kind) == "setLinkField" || strings.TrimSpace(op.Kind) == "setRootMetadataList" {
		return append([]string(nil), op.Values...), "list", nil
	}
	if op.Values != nil {
		return append([]string(nil), op.Values...), "list", nil
	}
	return []string{op.Value}, "scalar", nil
}

func ontologyNodeRefFromOp(op OntologyEditOp) (ontology.NodeRef, error) {
	path := strings.TrimSpace(op.Path)
	if path == "" {
		return ontology.NodeRef{}, fmt.Errorf("op path is required")
	}
	notePath, fragment, fragmentType := splitLinkFragment(path)
	if notePath == "" {
		notePath = path
	}
	ref := ontology.NodeRef{
		NotePath:   notePath,
		NodeID:     strings.TrimSpace(op.NodeID),
		Structural: strings.TrimSpace(op.Structural),
		Kind:       ontology.NodeKindNote,
	}
	if strings.TrimSpace(fragment) == "" {
		if nodeIDPath, nodeIDFragment, nodeIDFragmentType := splitLinkFragment(ref.NodeID); strings.TrimSpace(nodeIDFragment) != "" {
			if strings.TrimSpace(nodeIDPath) != "" {
				ref.NotePath = nodeIDPath
			}
			ref.Fragment = joinAnchorFragment(strings.TrimSpace(nodeIDFragment), nodeIDFragmentType)
			ref.Kind = ontology.NodeKindSection
			if nodeIDFragmentType == "block" || isItemFragment(nodeIDFragment) {
				ref.Kind = ontology.NodeKindEmbedded
			}
		}
		return ref, nil
	}
	ref.Fragment = joinAnchorFragment(strings.TrimSpace(fragment), fragmentType)
	ref.Kind = ontology.NodeKindSection
	if fragmentType == "block" || isItemFragment(fragment) {
		ref.Kind = ontology.NodeKindEmbedded
	}
	return ref, nil
}

func isItemFragment(fragment string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(fragment, "#")), "item-")
}
