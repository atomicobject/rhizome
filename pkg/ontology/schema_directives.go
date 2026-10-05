package ontology

import (
	"fmt"
	"github.com/vektah/gqlparser/v2/ast"
	"regexp"
	"strings"
)

func fieldSourceArg(v any) FieldSource {
	s, ok := v.(string)
	if !ok {
		return FieldSourceFrontmatter
	}
	switch FieldSource(strings.ToUpper(strings.TrimSpace(s))) {
	case FieldSourceInline:
		return FieldSourceInline
	case FieldSourceCheckbox:
		return FieldSourceCheckbox
	case FieldSourceItemTitle:
		return FieldSourceItemTitle
	case FieldSourceItemSummary:
		return FieldSourceItemSummary
	case FieldSourceItemDetail:
		return FieldSourceItemDetail
	default:
		return FieldSourceFrontmatter
	}
}

func fieldAuthoringStyleArg(v any) FieldAuthoringStyle {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	switch FieldAuthoringStyle(strings.ToUpper(strings.TrimSpace(s))) {
	case FieldAuthoringStyleAny:
		return FieldAuthoringStyleAny
	case FieldAuthoringStyleFrontmatter:
		return FieldAuthoringStyleFrontmatter
	case FieldAuthoringStyleInlineProperty:
		return FieldAuthoringStyleInlineProperty
	case FieldAuthoringStyleListMetadata:
		return FieldAuthoringStyleListMetadata
	case FieldAuthoringStyleItemText:
		return FieldAuthoringStyleItemText
	case FieldAuthoringStyleCheckbox:
		return FieldAuthoringStyleCheckbox
	default:
		return ""
	}
}

func neighborDirectionArg(v any) NeighborDirection {
	s, ok := v.(string)
	if !ok {
		return NeighborDirectionBoth
	}
	switch NeighborDirection(strings.ToUpper(strings.TrimSpace(s))) {
	case NeighborDirectionOutbound:
		return NeighborDirectionOutbound
	case NeighborDirectionInbound:
		return NeighborDirectionInbound
	default:
		return NeighborDirectionBoth
	}
}

func neighborScopeArg(v any) NeighborScope {
	s, ok := v.(string)
	if !ok {
		return NeighborScopeNote
	}
	switch NeighborScope(strings.ToUpper(strings.TrimSpace(s))) {
	case NeighborScopeSubtree:
		return NeighborScopeSubtree
	default:
		return NeighborScopeNote
	}
}

func sectionLevelArg(v any) SectionLevel {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	switch SectionLevel(strings.ToUpper(strings.TrimSpace(s))) {
	case SectionLevelH1:
		return SectionLevelH1
	case SectionLevelH2:
		return SectionLevelH2
	case SectionLevelH3:
		return SectionLevelH3
	case SectionLevelH4:
		return SectionLevelH4
	case SectionLevelH5:
		return SectionLevelH5
	case SectionLevelH6:
		return SectionLevelH6
	default:
		return ""
	}
}

func embeddedSourceShapeArg(v any) EmbeddedSourceShape {
	s, ok := v.(string)
	if !ok {
		return EmbeddedSourceShapeSection
	}
	switch EmbeddedSourceShape(strings.ToUpper(strings.TrimSpace(s))) {
	case EmbeddedSourceShapeListItem:
		return EmbeddedSourceShapeListItem
	case EmbeddedSourceShapeCheckboxItem:
		return EmbeddedSourceShapeCheckboxItem
	default:
		return EmbeddedSourceShapeSection
	}
}

func sectionDisplayArg(v any) SectionDisplay {
	s, ok := v.(string)
	if !ok {
		return SectionDisplayPane
	}
	switch SectionDisplay(strings.ToUpper(strings.TrimSpace(s))) {
	case SectionDisplayInline:
		return SectionDisplayInline
	default:
		return SectionDisplayPane
	}
}

func semanticsKind(dir *ast.Directive) SemanticsKind {
	if dir == nil {
		return ""
	}
	args := dir.ArgumentMap(nil)
	s, _ := args["kind"].(string)
	switch SemanticsKind(strings.ToUpper(strings.TrimSpace(s))) {
	case SemanticsKindBehavioral:
		return SemanticsKindBehavioral
	case SemanticsKindDocumentary:
		return SemanticsKindDocumentary
	default:
		return ""
	}
}

func directiveAnnotations(dirs ast.DirectiveList) map[string]map[string]any {
	if len(dirs) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(dirs))
	for _, dir := range dirs {
		if dir == nil || strings.TrimSpace(dir.Name) == "" {
			continue
		}
		out[dir.Name] = cloneAnnotationArgs(dir.ArgumentMap(nil))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func companionDocsArg(dirs ast.DirectiveList) ([]CompanionDocRef, error) {
	dir := dirs.ForName(companionDocsDirectiveName)
	if dir == nil {
		return nil, nil
	}
	args := dir.ArgumentMap(nil)
	paths, err := stringListArg(args["paths"])
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("paths must contain at least one note path")
	}
	purpose := stringArg(args["purpose"], "")
	out := make([]CompanionDocRef, 0, len(paths))
	for _, path := range paths {
		out = append(out, CompanionDocRef{
			Path:    path,
			Purpose: purpose,
		})
	}
	return out, nil
}

func guidanceFromDescriptionAndDirective(description string, dir *ast.Directive) *Guidance {
	summary := strings.TrimSpace(description)
	if dir == nil && summary == "" {
		return nil
	}
	guidance := &Guidance{Summary: summary}
	if dir == nil {
		return guidance
	}
	args := dir.ArgumentMap(nil)
	guidance.Meaning = strings.TrimSpace(stringArg(args["meaning"], ""))
	guidance.Authoring = strings.TrimSpace(stringArg(args["authoring"], ""))
	guidance.AgentImplications = strings.TrimSpace(stringArg(args["agentImplications"], ""))
	if guidance.Summary == "" && guidance.Meaning == "" && guidance.Authoring == "" && guidance.AgentImplications == "" {
		return nil
	}
	return guidance
}

func displayPresentationFromDirective(dir *ast.Directive) (singular, plural, group, parent string) {
	if dir == nil {
		return "", "", "", ""
	}
	args := dir.ArgumentMap(nil)
	return strings.TrimSpace(stringArg(args["singular"], "")),
		strings.TrimSpace(stringArg(args["plural"], "")),
		strings.TrimSpace(stringArg(args["group"], "")),
		strings.TrimSpace(stringArg(args["parent"], ""))
}

func fieldDisplayFromDirective(dir *ast.Directive) FieldDisplay {
	if dir == nil {
		return FieldDisplay{}
	}
	args := dir.ArgumentMap(nil)
	return FieldDisplay{
		Role:       FieldDisplayRole(strings.TrimSpace(stringArg(args["role"], ""))),
		Importance: FieldDisplayImportance(strings.TrimSpace(stringArg(args["importance"], ""))),
		HideHover:  !boolArg(args["hover"], true),
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func policyHintFromDirective(dir *ast.Directive) *PolicyHint {
	if dir == nil {
		return nil
	}
	args := dir.ArgumentMap(nil)
	policy := &PolicyHint{
		RequiresUserConfirmation:      boolArg(args["requiresUserConfirmation"], false),
		ForbidAutonomousSemanticEdits: boolArg(args["forbidAutonomousSemanticEdits"], false),
		EditScope:                     strings.TrimSpace(stringArg(args["editScope"], "")),
		Reason:                        strings.TrimSpace(stringArg(args["reason"], "")),
	}
	if !policy.RequiresUserConfirmation && !policy.ForbidAutonomousSemanticEdits && policy.EditScope == "" && policy.Reason == "" {
		return nil
	}
	return policy
}

func titleConstraintFromDirective(dir *ast.Directive) (*TitleConstraint, error) {
	if dir == nil {
		return nil, nil
	}
	constraint, err := fieldFormatConstraintFromArgs(dir.ArgumentMap(nil))
	return (*TitleConstraint)(constraint), err
}

func fieldFormatConstraintFromArgs(args map[string]any) (*FieldFormatConstraint, error) {
	pattern := strings.TrimSpace(stringArg(args["pattern"], ""))
	notPattern := strings.TrimSpace(stringArg(args["notPattern"], ""))
	if pattern == "" && notPattern == "" {
		return nil, fmt.Errorf("pattern or notPattern is required")
	}
	out := &FieldFormatConstraint{Pattern: pattern, NotPattern: notPattern}
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("pattern is not a valid regexp: %w", err)
		}
		out.patternRE = re
	}
	if notPattern != "" {
		re, err := regexp.Compile(notPattern)
		if err != nil {
			return nil, fmt.Errorf("notPattern is not a valid regexp: %w", err)
		}
		out.notPatternRE = re
	}
	return out, nil
}

func conditionalRequirementsFromDirectives(dirs ast.DirectiveList, noteType *NoteType, schema *Schema) ([]ConditionalRequirement, error) {
	if len(dirs) == 0 || noteType == nil {
		return nil, nil
	}
	out := make([]ConditionalRequirement, 0)
	for _, dir := range dirs {
		if dir == nil || dir.Name != "requiresWhen" {
			continue
		}
		args := dir.ArgumentMap(nil)
		fieldName := strings.TrimSpace(stringArg(args["field"], ""))
		equals := strings.TrimSpace(stringArg(args["equals"], ""))
		if fieldName == "" {
			return nil, fmt.Errorf("field is required")
		}
		if equals == "" {
			return nil, fmt.Errorf("equals is required for %s", fieldName)
		}
		trigger := noteType.ByName[fieldName]
		if trigger == nil || (trigger.Kind != FieldKindScalar && trigger.Kind != FieldKindEnum) {
			return nil, fmt.Errorf("trigger field %q must be a declared scalar or enum field", fieldName)
		}
		if trigger.Kind == FieldKindEnum {
			values := EnumValuesSet(schema, trigger.TypeName)
			if _, ok := values[equals]; !ok {
				return nil, fmt.Errorf("trigger field %q equals value %q is not a member of enum %s", fieldName, equals, trigger.TypeName)
			}
		}
		requireItems, err := requiredFieldConditionsArg(args["require"])
		if err != nil {
			return nil, err
		}
		if len(requireItems) == 0 {
			return nil, fmt.Errorf("require must contain at least one field condition")
		}
		for _, item := range requireItems {
			field := noteType.ByName[item.Field]
			if field == nil {
				return nil, fmt.Errorf("required field %q is not declared on %s", item.Field, noteType.Name)
			}
			if item.Equals != "" && field.Kind != FieldKindScalar && field.Kind != FieldKindEnum {
				return nil, fmt.Errorf("required field %q can declare equals only for scalar or enum fields", item.Field)
			}
			if item.Equals != "" && field.Kind == FieldKindEnum {
				values := EnumValuesSet(schema, field.TypeName)
				if _, ok := values[item.Equals]; !ok {
					return nil, fmt.Errorf("required field %q equals value %q is not a member of enum %s", item.Field, item.Equals, field.TypeName)
				}
			}
		}
		out = append(out, ConditionalRequirement{
			Field:   fieldName,
			Equals:  equals,
			Require: requireItems,
		})
	}
	return out, nil
}

func requiredFieldConditionsArg(v any) ([]RequiredFieldCondition, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("require must be a list")
	}
	out := make([]RequiredFieldCondition, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("require entries must be input objects")
		}
		field := strings.TrimSpace(stringArg(m["field"], ""))
		if field == "" {
			return nil, fmt.Errorf("require entry field is required")
		}
		out = append(out, RequiredFieldCondition{
			Field:  field,
			Equals: strings.TrimSpace(stringArg(m["equals"], "")),
		})
	}
	return out, nil
}

func enumValueViewFromDirective(dir *ast.Directive) (EnumValueView, error) {
	if dir == nil {
		return EnumValueView{}, nil
	}
	args := dir.ArgumentMap(nil)
	view := EnumValueView{
		Label: strings.TrimSpace(stringArg(args["label"], "")),
		Order: intArg(args["order"], 0),
	}
	_, view.orderSet = args["order"]
	if raw, ok := args["collapsed"]; ok {
		if collapsed, ok := raw.(bool); ok {
			view.Collapsed = &collapsed
		}
	}
	if _, ok := args["tone"]; ok {
		view.Tone = strings.TrimSpace(stringArg(args["tone"], ""))
		switch view.Tone {
		case "neutral", "info", "progress", "success", "warning", "risk", "muted":
		default:
			return EnumValueView{}, fmt.Errorf("tone must be one of neutral, info, progress, success, warning, risk, or muted")
		}
	}
	if _, ok := args["stage"]; ok {
		view.Stage = LifecycleStage(strings.TrimSpace(stringArg(args["stage"], "")))
		if _, ok := stageDefaultTones[view.Stage]; !ok {
			return EnumValueView{}, fmt.Errorf("stage must be one of open, active, done, or dropped")
		}
	}
	return view, nil
}

func cloneAnnotationArgs(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAnnotationValue(value)
	}
	return out
}

func cloneAnnotationValue(value any) any {
	switch current := value.(type) {
	case []any:
		out := make([]any, 0, len(current))
		for _, item := range current {
			out = append(out, cloneAnnotationValue(item))
		}
		return out
	case map[string]any:
		return cloneAnnotationArgs(current)
	default:
		return current
	}
}
