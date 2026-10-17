package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
)

func editOpMatchesBase(state *documentState, op editOp) bool {
	switch typed := op.(type) {
	case setFieldOp:
		return setFieldOpMatchesBase(state, typed)
	case reorderCollectionOp:
		return reorderCollectionOpMatchesBase(state, typed)
	default:
		return false
	}
}

func setFieldOpMatchesBase(state *documentState, op setFieldOp) bool {
	if state == nil {
		return false
	}
	rootOnly, err := state.providerRootOnly()
	if err != nil {
		return false
	}
	if op.RawRootMetadata || (rootOnly && op.Ref.Kind == NodeKindNote && strings.TrimSpace(op.Ref.Fragment) == "" && strings.TrimSpace(op.Ref.NodeID) == "") {
		values, kind, err := fieldValueForReplay(state, op)
		if err != nil {
			return false
		}
		return equalEditFieldValue(kind, values, fieldValueKindForOp(op), op.Values)
	}
	_, projection, err := projectState(state, op.Ref)
	if err != nil || projection == nil {
		return false
	}
	if strings.EqualFold(op.Field, "title") {
		return len(op.Values) == 1 && oneLineTitleValue(op.Values[0]) == projectedNodeTitle(projection)
	}
	binding, ok := projection.Fields[op.Field]
	if !ok {
		return false
	}
	if op.Unset {
		return !binding.Present
	}
	if !binding.Present {
		return false
	}
	if len(op.Values) != len(binding.Values) {
		return false
	}
	for i := range op.Values {
		if op.Values[i] != binding.Values[i] {
			return false
		}
	}
	return true
}

func fieldChangedConflict(baseState, currentState *documentState, ops []editOp) *ConflictReport {
	if baseState == nil || currentState == nil || len(ops) == 0 {
		return nil
	}
	for _, rawOp := range ops {
		op, ok := rawOp.(setFieldOp)
		if !ok {
			continue
		}
		expected := append([]string(nil), op.ExpectedValues...)
		expectedKind := op.ExpectedValueKind
		if !op.ExpectedCaptured {
			var err error
			expected, expectedKind, err = fieldValueForReplay(baseState, op)
			if err != nil {
				continue
			}
		}
		if !op.ExpectedCaptured && equalEditFieldValue(expectedKind, expected, fieldValueKindForOp(op), op.Values) {
			continue
		}
		current, currentKind, err := fieldValueForReplay(currentState, op)
		if err != nil || equalEditFieldValue(expectedKind, expected, currentKind, current) {
			continue
		}
		return &ConflictReport{
			Kind:             ConflictKindFieldChanged,
			NotePath:         op.Ref.NotePath,
			NodeRef:          op.Ref.String(),
			Field:            op.Field,
			ExpectedValue:    formatEditValues(expected),
			CurrentValue:     formatEditValues(current),
			CurrentValues:    append([]string(nil), current...),
			CurrentValueKind: currentKind,
			ProposedValue:    formatEditValues(op.Values),
			Message: fmt.Sprintf(
				"field %s changed: expected %q, current %q, proposed %q",
				op.Field,
				formatEditValues(expected),
				formatEditValues(current),
				formatEditValues(op.Values),
			),
		}
	}
	return nil
}

func fieldValueForReplay(state *documentState, op setFieldOp) ([]string, string, error) {
	rootOnly, err := state.providerRootOnly()
	if err != nil {
		return nil, "", err
	}
	if op.RawRootMetadata || (rootOnly && op.Ref.Kind == NodeKindNote && strings.TrimSpace(op.Ref.Fragment) == "" && strings.TrimSpace(op.Ref.NodeID) == "") {
		return providerRootFieldValueForReplay(state, op)
	}
	_, projection, err := projectState(state, op.Ref)
	if err != nil {
		return nil, "", err
	}
	if strings.EqualFold(op.Field, "title") {
		return []string{projectedNodeTitle(projection)}, "scalar", nil
	}
	if projection == nil || projection.Type == nil {
		return nil, "", fmt.Errorf("%w: node type is required", errMissingNode)
	}
	binding, ok := projection.Fields[op.Field]
	if !ok {
		return nil, "", fmt.Errorf("%w: field %s not found", errMissingField, op.Field)
	}
	if !binding.Present {
		return nil, "unset", nil
	}
	kind := "scalar"
	if op.IsLinks || op.RequireScalarList {
		kind = "list"
	}
	values := append([]string(nil), binding.Values...)
	if kind == "scalar" && len(values) == 0 {
		values = []string{""}
	}
	return values, kind, nil
}

func fieldValueKindForOp(op setFieldOp) string {
	if op.Unset {
		return "unset"
	}
	if op.IsLinks || op.RequireScalarList {
		return "list"
	}
	return "scalar"
}

func equalEditFieldValue(leftKind string, left []string, rightKind string, right []string) bool {
	return leftKind == rightKind && equalEditValues(left, right)
}

func equalEditValues(left, right []string) bool {
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

func formatEditValues(values []string) string {
	return strings.Join(values, ", ")
}

func reorderCollectionOpMatchesBase(state *documentState, op reorderCollectionOp) bool {
	if state == nil {
		return false
	}
	_, projection, err := projectState(state, op.ParentRef)
	if err != nil || projection == nil {
		return false
	}
	collection, ok := projection.Collections[op.Field]
	if !ok || validateCollectionPermutation(collection.Items, op.OrderedFragments) != nil {
		return false
	}
	for i, fragment := range op.OrderedFragments {
		if strings.TrimSpace(strings.TrimPrefix(fragment, "#")) != strings.TrimSpace(strings.TrimPrefix(collection.Items[i].Ref.Fragment, "#")) {
			return false
		}
	}
	return true
}

func projectedNodeTitle(projection *NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if projection.Ref.Kind == NodeKindNote {
		if title, ok := projection.Snapshot.Frontmatter["title"].(string); ok && strings.TrimSpace(title) != "" {
			return title
		}
		for _, section := range projection.Snapshot.Sections {
			if section != nil && section.Level == ontoschema.SectionLevelH1 && strings.TrimSpace(section.Title) != "" {
				return section.Title
			}
		}
		return paths.NoteTitle(projection.Ref.NotePath)
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return span.Title
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return section.Title
	}
	return ""
}

func planHasMaterialChange(plan CommitPlan) bool {
	for _, file := range plan.Files {
		if file.HasMaterialChange {
			return true
		}
	}
	return false
}

func materialNotePathSet(plan CommitPlan) map[string]bool {
	out := make(map[string]bool, len(plan.Files))
	for _, file := range plan.Files {
		if file.HasMaterialChange {
			out[file.NotePath] = true
		}
	}
	return out
}

func planHasRebased(plan CommitPlan) bool {
	for _, file := range plan.Files {
		if file.Rebased {
			return true
		}
	}
	return false
}

type conflictListError struct {
	Conflicts []ConflictReport
}

func (e conflictListError) Error() string {
	if len(e.Conflicts) == 0 {
		return "edit conflict"
	}
	return e.Conflicts[0].Message
}

// AsConflictList unwraps a commit/preview conflict list error into a typed payload.
func AsConflictList(err error, target *conflictListError) bool {
	if err == nil || target == nil {
		return false
	}
	value, ok := err.(conflictListError)
	if !ok {
		return false
	}
	*target = value
	return true
}
