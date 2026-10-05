package validate

import (
	"fmt"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func buildCodeAnchorFix(runCtx RunContext, item codeanchor.ValidationResult, notePaths []string, issueKey string) FixAction {
	paths := sortedUnique(append([]string(nil), notePaths...))
	status := string(item.Status)
	actionID := "repair-code-anchor:" + status + ":" + item.Anchor.Label
	if suffix := strings.TrimPrefix(issueKey, "issue:v1:"); suffix != "" {
		actionID += ":" + suffix
	}
	action := FixAction{
		ID:    actionID,
		Check: CheckCodeAnchors, IssueCode: status,
		Kind: "repair_code_anchor", Safety: FixSafetyAgent,
		Title:         "Repair code anchor " + item.Anchor.Label,
		Summary:       item.Message,
		InstanceCount: 1, IssueKeys: []string{issueKey}, AffectedPaths: paths,
	}
	if item.Status != codeanchor.ValidationSuffix || len(item.SuffixMatches) != 1 || len(paths) != 1 {
		return action
	}
	oldTarget := codeAnchorTarget(item.Anchor)
	newTarget := item.SuffixMatches[0]
	reader := runCtx.NoteReader
	if reader == nil {
		reader = &obsidian.Note{}
	}
	vaultDef := runCtx.VaultDef
	if strings.TrimSpace(vaultDef.BasePath()) == "" {
		vaultDef.Path = runCtx.VaultPath
	}
	content, err := reader.GetContents(vaultDef, paths[0])
	if err != nil {
		action.Summary = fmt.Sprintf("%s; source preview failed: %v", item.Message, err)
		return action
	}
	_, changed, err := replaceCodeAnchorTargetInMemory(content, item.Anchor.Label, oldTarget, newTarget)
	if err != nil || !changed {
		action.Summary = fmt.Sprintf("%s; unique source edit could not be materialized", item.Message)
		return action
	}
	action.Kind = FixKindReplaceCodeAnchorTarget
	action.Safety = FixSafetyConfirm
	action.Title = "Confirm unique code-anchor suffix replacement"
	action.Summary = fmt.Sprintf("replace %s with the unique indexed suffix match %s", oldTarget, newTarget)
	action.Question = fmt.Sprintf("Replace code anchor %q target %q with %q in %s?", item.Anchor.Label, oldTarget, newTarget, paths[0])
	action.Edits = []FixEdit{{
		Kind: FixKindReplaceCodeAnchorTarget, NotePath: paths[0], Property: item.Anchor.Label,
		OldTarget: oldTarget, NewTarget: newTarget,
	}}
	return action
}

func codeAnchorTarget(anchor codeanchor.Anchor) string {
	if anchor.BaseSym == nil {
		return ""
	}
	if anchor.BaseSym.Pkg == "" {
		return anchor.BaseSym.Name
	}
	return anchor.BaseSym.Pkg + "." + anchor.BaseSym.Name
}

func replaceCodeAnchorTargetInMemory(content, label, oldTarget, newTarget string) (string, bool, error) {
	if strings.TrimSpace(oldTarget) == "" || strings.TrimSpace(newTarget) == "" || oldTarget == newTarget {
		return content, false, fmt.Errorf("code-anchor replacement requires distinct non-empty targets")
	}
	frontmatter, err := obsidian.ExtractFrontmatter(content)
	if err != nil {
		return content, false, err
	}
	if frontmatter == nil {
		return content, false, fmt.Errorf("code-anchor source has no frontmatter")
	}
	key, anchorsValue := caseInsensitiveMapValue(frontmatter, "code-anchors")
	if key == "" {
		return content, false, fmt.Errorf("code-anchor source uses no supported code-anchors property")
	}
	updated, replacements := replaceCodeAnchorValue(anchorsValue, label, oldTarget, newTarget)
	if replacements != 1 {
		return content, false, fmt.Errorf("code-anchor target replacement matched %d selectors; expected exactly one", replacements)
	}
	result, changed, err := obsidian.SetFrontmatterProperty(content, key, updated, true)
	if err != nil {
		return content, false, err
	}
	if !changed || result == content {
		return content, false, fmt.Errorf("code-anchor target replacement produced no source change")
	}
	return result, true, nil
}

func replaceCodeAnchorValue(value any, label, oldTarget, newTarget string) (any, int) {
	switch current := value.(type) {
	case map[string]any:
		replacements := 0
		for key, nested := range current {
			updated, count := replaceCodeAnchorValue(nested, label, oldTarget, newTarget)
			current[key] = updated
			replacements += count
		}
		return current, replacements
	case []any:
		replacements := 0
		for index, entry := range current {
			updated, count := replaceCodeAnchorEntry(entry, label, oldTarget, newTarget)
			current[index] = updated
			replacements += count
		}
		return current, replacements
	default:
		return value, 0
	}
}

func replaceCodeAnchorEntry(value any, label, oldTarget, newTarget string) (any, int) {
	switch current := value.(type) {
	case string:
		updated, matched := replaceCodeAnchorSelector(current, oldTarget, newTarget)
		if !matched || !codeAnchorLabelMatches(label, "", oldTarget) {
			return value, 0
		}
		return updated, 1
	case map[string]any:
		explicitLabel := ""
		if _, raw := caseInsensitiveMapValue(current, "label"); raw != nil {
			explicitLabel, _ = raw.(string)
		}
		selectorKeys := []string{"ref", "symbol", "calls", "baseClass"}
		for _, selectorKey := range selectorKeys {
			key, raw := caseInsensitiveMapValue(current, selectorKey)
			target, ok := raw.(string)
			if key == "" || !ok || !codeAnchorLabelMatches(label, explicitLabel, oldTarget) {
				continue
			}
			updated, matched := replaceCodeAnchorSelector(target, oldTarget, newTarget)
			if !matched {
				continue
			}
			current[key] = updated
			return current, 1
		}
		return current, 0
	default:
		return value, 0
	}
}

func replaceCodeAnchorSelector(value, oldTarget, newTarget string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == oldTarget {
		return strings.Replace(value, oldTarget, newTarget, 1), true
	}
	if index := strings.Index(trimmed, ":"); index > 0 && strings.TrimSpace(trimmed[index+1:]) == oldTarget {
		prefix := trimmed[:index+1]
		return prefix + newTarget, true
	}
	return value, false
}

func codeAnchorLabelMatches(want, explicit, target string) bool {
	if strings.TrimSpace(explicit) != "" {
		return explicit == want
	}
	target = strings.TrimSpace(target)
	for _, separator := range []string{"::", "\\", "."} {
		if index := strings.LastIndex(target, separator); index >= 0 {
			target = target[index+len(separator):]
			break
		}
	}
	return target == want
}

func caseInsensitiveMapValue(values map[string]any, target string) (string, any) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.EqualFold(key, target) {
			return key, values[key]
		}
	}
	return "", nil
}
