package skilloverlay

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
)

func parseSkillOverlaySlots(skillName, filePath, body string) ([]skillOverlaySlot, error) {
	var slots []skillOverlaySlot
	seen := map[string]struct{}{}
	inFence := false
	var fenceChar byte
	var fenceLen int
	var open *skillOverlaySlot
	for i := 0; i < len(body); {
		atLineStart := i == 0 || body[i-1] == '\n'
		if atLineStart {
			if ok, fc, fl, next := markdownsyntax.FenceLine(body, i); ok {
				if !inFence {
					inFence = true
					fenceChar = fc
					fenceLen = fl
				} else if fc == fenceChar && fl >= fenceLen {
					inFence = false
					fenceChar = 0
					fenceLen = 0
				}
				i = next
				continue
			}
			if inFence {
				i = markdownsyntax.NextLineStart(body, i)
				continue
			}
			lineEnd := markdownsyntax.NextLineStart(body, i)
			line := strings.TrimSpace(strings.TrimSuffix(body[i:lineEnd], "\n"))
			if strings.HasPrefix(line, "<!-- rzm:skill-slot ") && strings.HasSuffix(line, "-->") {
				if open != nil {
					return nil, skillOverlayErr(skillOverlayIssuef("nested_slot", skillName, filePath, open.ID, "", "slot %q is nested", open.ID))
				}
				slot, err := parseSkillOverlaySlotStart(skillName, filePath, line)
				if err != nil {
					return nil, err
				}
				if _, ok := seen[slot.ID]; ok {
					return nil, skillOverlayErr(skillOverlayIssuef("duplicate_slot_id", skillName, filePath, slot.ID, "", "slot id %q is declared more than once", slot.ID))
				}
				seen[slot.ID] = struct{}{}
				slot.Start = i
				slot.BodyStart = lineEnd
				open = &slot
				i = lineEnd
				continue
			}
			if line == "<!-- /rzm:skill-slot -->" {
				if open == nil {
					return nil, skillOverlayErr(skillOverlayIssuef("invalid_slot_markup", skillName, filePath, "", "", "closing slot marker without an open slot"))
				}
				open.BodyEnd = i
				open.End = lineEnd
				open.Body = body[open.BodyStart:open.BodyEnd]
				slots = append(slots, *open)
				open = nil
				i = lineEnd
				continue
			}
		}
		i = markdownsyntax.NextLineStart(body, i)
	}
	if open != nil {
		return nil, skillOverlayErr(skillOverlayIssuef("unterminated_slot", skillName, filePath, open.ID, "", "slot %q is missing a closing marker", open.ID))
	}
	return slots, nil
}

func hasUnparsedSkillSlotMarker(body string) bool {
	inFence := false
	var fenceChar byte
	var fenceLen int
	for i := 0; i < len(body); {
		if ok, fc, fl, next := markdownsyntax.FenceLine(body, i); ok {
			if !inFence {
				inFence = true
				fenceChar = fc
				fenceLen = fl
			} else if fc == fenceChar && fl >= fenceLen {
				inFence = false
				fenceChar = 0
				fenceLen = 0
			}
			i = next
			continue
		}
		if !inFence {
			lineEnd := markdownsyntax.NextLineStart(body, i)
			line := strings.TrimSpace(strings.TrimSuffix(body[i:lineEnd], "\n"))
			if strings.HasPrefix(line, "<!-- rzm:skill-slot ") || line == "<!-- /rzm:skill-slot -->" {
				return true
			}
		}
		i = markdownsyntax.NextLineStart(body, i)
	}
	return false
}

func parseSkillOverlaySlotStart(skillName, filePath, line string) (skillOverlaySlot, error) {
	inside := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "<!-- rzm:skill-slot "), "-->"))
	attrs, err := parseSkillOverlayAttrs(inside)
	if err != nil {
		return skillOverlaySlot{}, skillOverlayErr(skillOverlayIssuef("invalid_slot_markup", skillName, filePath, "", "", "%v", err))
	}
	if len(attrs) != 2 {
		return skillOverlaySlot{}, skillOverlayErr(skillOverlayIssuef("invalid_slot_markup", skillName, filePath, "", "", "slot marker must declare only id and mode"))
	}
	id := strings.TrimSpace(attrs["id"])
	mode := skillOverlaySlotMode(strings.TrimSpace(attrs["mode"]))
	if id == "" {
		return skillOverlaySlot{}, skillOverlayErr(skillOverlayIssuef("invalid_slot_markup", skillName, filePath, "", "", "slot id is required"))
	}
	switch mode {
	case skillOverlaySlotModeExtension, skillOverlaySlotModeReplaceable:
	default:
		return skillOverlaySlot{}, skillOverlayErr(skillOverlayIssuef("invalid_slot_mode", skillName, filePath, id, "", "unsupported slot mode %q", mode))
	}
	return skillOverlaySlot{ID: id, Mode: mode}, nil
}

func parseSkillOverlayAttrs(input string) (map[string]string, error) {
	attrs := map[string]string{}
	for strings.TrimSpace(input) != "" {
		input = strings.TrimLeft(input, " \t")
		eq := strings.IndexByte(input, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("expected key=value attribute")
		}
		key := strings.TrimSpace(input[:eq])
		if key != "id" && key != "mode" {
			return nil, fmt.Errorf("unknown attribute %q", key)
		}
		rest := strings.TrimLeft(input[eq+1:], " \t")
		if !strings.HasPrefix(rest, `"`) {
			return nil, fmt.Errorf("attribute %q must use quoted value", key)
		}
		rest = rest[1:]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			return nil, fmt.Errorf("unterminated quoted value for %q", key)
		}
		if _, exists := attrs[key]; exists {
			return nil, fmt.Errorf("duplicate attribute %q", key)
		}
		attrs[key] = rest[:end]
		input = rest[end+1:]
	}
	return attrs, nil
}

func sortResolvedFragments(fragments []resolvedSkillOverlayFragment) {
	sort.SliceStable(fragments, func(i, j int) bool {
		a, b := fragments[i], fragments[j]
		if a.TemplateOrder != b.TemplateOrder {
			return a.TemplateOrder < b.TemplateOrder
		}
		if a.SourcePath != b.SourcePath {
			return a.SourcePath < b.SourcePath
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.FragmentIndex < b.FragmentIndex
	})
}

func cleanSkillOverlayWhitespace(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
