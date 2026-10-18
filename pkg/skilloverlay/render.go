package skilloverlay

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func Render(skills []Skill, overlays []LoadedOverlay, templates ...string) ([]Skill, Report, error) {
	report := Report{Templates: normalizeSkillOverlayReportTemplates(templates, overlays)}
	out := cloneSkillTemplates(skills)
	if len(out) == 0 {
		return out, report, nil
	}
	var issues []Issue
	overlayIDs := map[string]LoadedOverlay{}
	bySkill := map[string][]resolvedSkillOverlayFragment{}
	for _, loaded := range overlays {
		id := strings.TrimSpace(loaded.Overlay.ID)
		if prev, ok := overlayIDs[id]; ok {
			issues = append(issues, skillOverlayIssuef("duplicate_overlay_id", loaded.Overlay.TargetSkill, loaded.Path, "", id, "overlay id %q already declared at %s", id, prev.Path))
			continue
		}
		overlayIDs[id] = loaded
		for index, fragment := range loaded.Overlay.Fragments {
			bySkill[loaded.Overlay.TargetSkill] = append(bySkill[loaded.Overlay.TargetSkill], resolvedSkillOverlayFragment{
				Template:      loaded.Template,
				TemplateOrder: loaded.TemplateOrder,
				OverlayID:     id,
				SourcePath:    loaded.Path,
				FragmentIndex: index,
				File:          skillOverlayFragmentFile(fragment.File),
				Slot:          fragment.Slot,
				Op:            fragment.Op,
				Order:         fragment.Order,
				Content:       strings.TrimSpace(fragment.Content),
			})
		}
	}
	skillIndex := map[string]int{}
	for i, skill := range out {
		skillIndex[skill.Name] = i
	}
	for target := range bySkill {
		if _, ok := skillIndex[target]; !ok {
			for _, fragment := range bySkill[target] {
				issues = append(issues, skillOverlayIssuef("unknown_target_skill", target, fragment.SourcePath, fragment.Slot, fragment.OverlayID, "target skill %q is not installed", target))
			}
		}
	}
	if len(issues) > 0 {
		report.Issues = append(report.Issues, issues...)
		return nil, report, Error{Issues: issues}
	}
	for i, skill := range out {
		skillName := skill.Name
		fragments := bySkill[skillName]
		sortResolvedFragments(fragments)
		rendered, err := renderSkillTemplate(skillName, skill, fragments, &report)
		if err != nil {
			appendSkillOverlayReportError(&report, err)
			return nil, report, err
		}
		out[i] = rendered
	}
	return out, report, nil
}

// skillOverlayFragmentFile resolves the skill-relative Markdown file a fragment
// targets; SKILL.md remains the default so existing overlays keep working.
func skillOverlayFragmentFile(file string) string {
	file = strings.TrimSpace(filepath.ToSlash(file))
	if file == "" {
		return "SKILL.md"
	}
	return file
}

func cloneSkillTemplates(skills []Skill) []Skill {
	if len(skills) == 0 {
		return nil
	}
	out := make([]Skill, len(skills))
	for i, skill := range skills {
		out[i].Name = skill.Name
		out[i].Files = append([]File(nil), skill.Files...)
	}
	return out
}

func renderSkillTemplate(skillName string, skill Skill, fragments []resolvedSkillOverlayFragment, report *Report) (Skill, error) {
	known := map[string]bool{}
	for _, file := range skill.Files {
		if file.IsMarkdown {
			known[filepath.ToSlash(file.Path)] = true
		}
	}
	var issues []Issue
	for _, fragment := range fragments {
		if !known[fragment.File] {
			issues = append(issues, skillOverlayIssuef("unknown_target_file", skillName, fragment.SourcePath, fragment.Slot, fragment.OverlayID, "target file %q is not a Markdown file of skill %s", fragment.File, skillName))
		}
	}
	if len(issues) > 0 {
		return Skill{}, Error{Issues: issues}
	}
	for i, file := range skill.Files {
		if !file.IsMarkdown {
			if strings.Contains(string(file.Content), "rzm:skill-slot") {
				return Skill{}, skillOverlayErr(skillOverlayIssuef("invalid_slot_markup", skillName, file.Path, "", "", "skill slots are only supported in Markdown files"))
			}
			continue
		}
		var fileFragments []resolvedSkillOverlayFragment
		for _, fragment := range fragments {
			if fragment.File == filepath.ToSlash(file.Path) {
				fileFragments = append(fileFragments, fragment)
			}
		}
		rendered, entries, err := renderSkillTemplateMarkdown(skillName, file.Path, string(file.Content), fileFragments)
		if err != nil {
			return Skill{}, err
		}
		if report != nil {
			report.Entries = append(report.Entries, entries...)
		}
		skill.Files[i].Content = []byte(rendered)
	}
	return skill, nil
}

func renderSkillTemplateMarkdown(skillName, filePath, body string, fragments []resolvedSkillOverlayFragment) (string, []Entry, error) {
	slots, err := parseSkillOverlaySlots(skillName, filePath, body)
	if err != nil {
		return "", nil, err
	}
	if len(slots) == 0 {
		if hasUnparsedSkillSlotMarker(body) {
			return "", nil, skillOverlayErr(skillOverlayIssuef("leftover_marker", skillName, filePath, "", "", "unparsed skill slot marker remains"))
		}
		return strings.TrimSpace(body), nil, nil
	}
	bySlot := map[string][]resolvedSkillOverlayFragment{}
	for _, fragment := range fragments {
		bySlot[fragment.Slot] = append(bySlot[fragment.Slot], fragment)
	}
	slotIDs := map[string]struct{}{}
	for _, slot := range slots {
		slotIDs[slot.ID] = struct{}{}
	}
	var issues []Issue
	for _, fragment := range fragments {
		if _, ok := slotIDs[fragment.Slot]; !ok {
			issues = append(issues, skillOverlayIssuef("unknown_slot", skillName, filePath, fragment.Slot, fragment.OverlayID, "slot %q is not declared by %s", fragment.Slot, skillName))
		}
	}
	if len(issues) > 0 {
		return "", nil, Error{Issues: issues}
	}
	var b strings.Builder
	var entries []Entry
	last := 0
	for _, slot := range slots {
		b.WriteString(body[last:slot.Start])
		rendered, err := renderSkillOverlaySlot(skillName, filePath, slot, bySlot[slot.ID])
		if err != nil {
			return "", nil, err
		}
		entries = append(entries, rendered.Entries...)
		b.WriteString(rendered.Content)
		if rendered.Content != "" && slot.End > slot.Start && slot.End <= len(body) && body[slot.End-1] == '\n' && slot.End < len(body) {
			b.WriteByte('\n')
		}
		last = slot.End
	}
	b.WriteString(body[last:])
	out := cleanSkillOverlayWhitespace(b.String())
	if hasUnparsedSkillSlotMarker(out) {
		return "", nil, skillOverlayErr(skillOverlayIssuef("leftover_marker", skillName, filePath, "", "", "rendered skill still contains slot markers"))
	}
	return out, entries, nil
}

func renderSkillOverlaySlot(skillName, filePath string, slot skillOverlaySlot, fragments []resolvedSkillOverlayFragment) (skillOverlaySlotRender, error) {
	if len(fragments) == 0 {
		if slot.Mode == skillOverlaySlotModeExtension {
			return skillOverlaySlotRender{Entries: []Entry{skillOverlaySlotEntry(skillName, slot.ID, "", "stripped_empty_slot", "", "", "", 0, "")}}, nil
		}
		body := strings.TrimSpace(slot.Body)
		return skillOverlaySlotRender{
			Content: body,
			Entries: []Entry{skillOverlaySlotEntry(skillName, slot.ID, "", "preserved_default", "", "", "", 0, body)},
		}, nil
	}
	var prepend, appendItems []string
	var entries []Entry
	body := strings.TrimSpace(slot.Body)
	replaced := false
	suppressed := false
	var replaceOverlay, suppressOverlay string
	for _, fragment := range fragments {
		switch fragment.Op {
		case Prepend:
			prepend = append(prepend, strings.TrimSpace(fragment.Content))
			entries = append(entries, skillOverlayFragmentEntry(skillName, fragment, "applied"))
		case Append:
			appendItems = append(appendItems, strings.TrimSpace(fragment.Content))
			entries = append(entries, skillOverlayFragmentEntry(skillName, fragment, "applied"))
		case Replace:
			if slot.Mode != skillOverlaySlotModeReplaceable {
				return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", skillName, filePath, slot.ID, fragment.OverlayID, "replace requires a replaceable slot"))
			}
			if replaced {
				return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("conflicting_replace", skillName, filePath, slot.ID, fragment.OverlayID, "slot already replaced by %s", replaceOverlay))
			}
			replaced = true
			replaceOverlay = fragment.OverlayID
			body = strings.TrimSpace(fragment.Content)
			entries = append(entries, skillOverlayFragmentEntry(skillName, fragment, "applied"))
		case Suppress:
			if slot.Mode != skillOverlaySlotModeReplaceable {
				return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", skillName, filePath, slot.ID, fragment.OverlayID, "suppress requires a replaceable slot"))
			}
			if suppressed {
				return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("conflicting_suppress", skillName, filePath, slot.ID, fragment.OverlayID, "slot already suppressed by %s", suppressOverlay))
			}
			suppressed = true
			suppressOverlay = fragment.OverlayID
			entries = append(entries, skillOverlayFragmentEntry(skillName, fragment, "suppressed_default"))
		default:
			return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", skillName, filePath, slot.ID, fragment.OverlayID, "unsupported operation %q", fragment.Op))
		}
	}
	if replaced && suppressed {
		return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("conflicting_replace_suppress", skillName, filePath, slot.ID, suppressOverlay, "slot cannot be both replaced and suppressed"))
	}
	if suppressed {
		if len(prepend) > 0 || len(appendItems) > 0 {
			return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("conflicting_suppress", skillName, filePath, slot.ID, suppressOverlay, "suppressed slot cannot also prepend or append"))
		}
		return skillOverlaySlotRender{Entries: entries}, nil
	}
	if slot.Mode == skillOverlaySlotModeExtension {
		for _, fragment := range fragments {
			if fragment.Op != Prepend && fragment.Op != Append {
				return skillOverlaySlotRender{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", skillName, filePath, slot.ID, fragment.OverlayID, "%s requires a replaceable slot", fragment.Op))
			}
		}
		body = ""
	}
	parts := make([]string, 0, len(prepend)+1+len(appendItems))
	for _, item := range prepend {
		if item != "" {
			parts = append(parts, item)
		}
	}
	if body != "" {
		parts = append(parts, body)
	}
	for _, item := range appendItems {
		if item != "" {
			parts = append(parts, item)
		}
	}
	return skillOverlaySlotRender{Content: strings.Join(parts, "\n\n"), Entries: entries}, nil
}

func normalizeSkillOverlayReportTemplates(templates []string, overlays []LoadedOverlay) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(template string) {
		template = strings.TrimSpace(template)
		if template == "" {
			return
		}
		if _, ok := seen[template]; ok {
			return
		}
		seen[template] = struct{}{}
		out = append(out, template)
	}
	for _, template := range templates {
		add(template)
	}
	if len(out) > 0 {
		return out
	}
	for _, overlay := range overlays {
		add(overlay.Template)
	}
	return out
}

func appendSkillOverlayReportError(report *Report, err error) {
	if report == nil || err == nil {
		return
	}
	var overlayErr Error
	if errors.As(err, &overlayErr) {
		report.Issues = append(report.Issues, overlayErr.Issues...)
	}
}

func skillOverlayFragmentEntry(skillName string, fragment resolvedSkillOverlayFragment, outcome string) Entry {
	return skillOverlaySlotEntry(
		skillName,
		fragment.Slot,
		string(fragment.Op),
		outcome,
		fragment.OverlayID,
		fragment.Template,
		fragment.SourcePath,
		fragment.Order,
		fragment.Content,
	)
}

func skillOverlaySlotEntry(skillName, slot, op, outcome, overlayID, sourceTemplate, sourcePath string, order int, content string) Entry {
	entry := Entry{
		TargetSkill:    skillName,
		Slot:           slot,
		Operation:      op,
		OverlayID:      overlayID,
		SourceTemplate: sourceTemplate,
		SourcePath:     sourcePath,
		Order:          order,
		Outcome:        outcome,
	}
	if strings.TrimSpace(content) != "" {
		entry.ContentHash = skillOverlayContentHash(content)
	}
	return entry
}

func skillOverlayContentHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return fmt.Sprintf("sha256:%x", sum)
}
