package init

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const skillOverlayAPIVersion = "rhizome.skill-overlay.v1"

type skillOverlaySlotMode string

const (
	skillOverlaySlotModeExtension   skillOverlaySlotMode = "extension"
	skillOverlaySlotModeReplaceable skillOverlaySlotMode = "replaceable"
)

type skillOverlayOp string

const (
	skillOverlayOpPrepend  skillOverlayOp = "prepend"
	skillOverlayOpAppend   skillOverlayOp = "append"
	skillOverlayOpReplace  skillOverlayOp = "replace"
	skillOverlayOpSuppress skillOverlayOp = "suppress"
)

type skillOverlay struct {
	APIVersion  string                 `yaml:"apiVersion"`
	ID          string                 `yaml:"id"`
	TargetSkill string                 `yaml:"targetSkill"`
	Fragments   []skillOverlayFragment `yaml:"fragments"`
}

type skillOverlayFragment struct {
	File    string         `yaml:"file,omitempty"`
	Slot    string         `yaml:"slot"`
	Op      skillOverlayOp `yaml:"op"`
	Order   int            `yaml:"order,omitempty"`
	Content string         `yaml:"content,omitempty"`
}

type loadedSkillOverlay struct {
	Template      string
	TemplateOrder int
	Path          string
	Overlay       skillOverlay
}

type resolvedSkillOverlayFragment struct {
	Template      string
	TemplateOrder int
	OverlayID     string
	SourcePath    string
	FragmentIndex int
	File          string
	Slot          string
	Op            skillOverlayOp
	Order         int
	Content       string
}

type skillOverlaySlot struct {
	ID        string
	Mode      skillOverlaySlotMode
	Start     int
	End       int
	BodyStart int
	BodyEnd   int
	Body      string
}

type skillOverlayIssue struct {
	Code    string
	Message string
	Skill   string
	Path    string
	Slot    string
	Overlay string
}

type SkillOverlayIssue = skillOverlayIssue

type skillOverlayRenderReport struct {
	Templates []string
	Entries   []skillOverlayManifestEntry
	Issues    []skillOverlayIssue
}

type SkillOverlayRenderReport = skillOverlayRenderReport

type skillOverlayManifest struct {
	APIVersion string                      `json:"apiVersion"`
	Templates  []string                    `json:"templates"`
	Entries    []skillOverlayManifestEntry `json:"entries"`
	Issues     []skillOverlayIssue         `json:"issues,omitempty"`
}

type SkillOverlayManifest = skillOverlayManifest

type skillOverlayManifestEntry struct {
	TargetSkill    string `json:"targetSkill"`
	Slot           string `json:"slot"`
	Operation      string `json:"operation"`
	OverlayID      string `json:"overlayId"`
	SourceTemplate string `json:"sourceTemplate"`
	SourcePath     string `json:"sourcePath"`
	Order          int    `json:"order"`
	Outcome        string `json:"outcome"`
	ContentHash    string `json:"contentHash,omitempty"`
}

type SkillOverlayManifestEntry = skillOverlayManifestEntry

type skillOverlaySlotRender struct {
	Content string
	Entries []skillOverlayManifestEntry
}

type skillOverlayError struct {
	Issues []skillOverlayIssue
}

func (e skillOverlayError) Error() string {
	if len(e.Issues) == 0 {
		return "skill overlay error"
	}
	return fmt.Sprintf("skill overlay %s: %s", e.Issues[0].Code, e.Issues[0].Message)
}

func skillOverlayIssuef(code, skill, filePath, slot, overlay, format string, args ...any) skillOverlayIssue {
	return skillOverlayIssue{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Skill:   skill,
		Path:    filePath,
		Slot:    slot,
		Overlay: overlay,
	}
}

func skillOverlayErr(issues ...skillOverlayIssue) error {
	return skillOverlayError{Issues: issues}
}

func (r skillOverlayRenderReport) Manifest() skillOverlayManifest {
	return skillOverlayManifest{
		APIVersion: skillOverlayAPIVersion,
		Templates:  append([]string(nil), r.Templates...),
		Entries:    append([]skillOverlayManifestEntry(nil), r.Entries...),
		Issues:     append([]skillOverlayIssue(nil), r.Issues...),
	}
}

func ValidateBundledSkillOverlays() (SkillOverlayManifest, error) {
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return SkillOverlayManifest{}, err
	}
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	aggregate := skillOverlayRenderReport{Templates: ids}
	for _, id := range ids {
		effective, err := resolveEffectiveTemplatesWithOptions([]string{id}, nil, false)
		if err != nil {
			issue := skillOverlayIssuef("template_resolution_error", id, "", "", "", "%v", err)
			aggregate.Issues = append(aggregate.Issues, issue)
			continue
		}
		_, manifest, err := loadAllSkillTemplatesWithReport(effective)
		aggregate.Entries = append(aggregate.Entries, manifest.Entries...)
		aggregate.Issues = append(aggregate.Issues, manifest.Issues...)
		if err != nil {
			before := len(aggregate.Issues)
			appendSkillOverlayReportError(&aggregate, err)
			if len(aggregate.Issues) == before {
				aggregate.Issues = append(aggregate.Issues, skillOverlayIssuef("render_error", strings.Join(effective, ","), "", "", "", "%v", err))
			}
		}
	}
	return aggregate.Manifest(), nil
}

func loadStarterSkillOverlayTemplates(template string, templateOrder ...int) ([]loadedSkillOverlay, error) {
	if template == "" {
		return nil, nil
	}
	order := 0
	if len(templateOrder) > 0 {
		order = templateOrder[0]
	}
	root := path.Join("templates", "starters", template, "agents", "skill-overlays")
	if _, err := fs.Stat(agentHelperTemplates, root); err != nil {
		if errorsIsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat skill overlay templates %q: %w", template, err)
	}
	var out []loadedSkillOverlay
	if err := fs.WalkDir(agentHelperTemplates, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(p), ".yaml") && !strings.HasSuffix(strings.ToLower(p), ".yml") {
			return skillOverlayErr(skillOverlayIssuef("overlay_yaml_decode_error", "", p, "", "", "skill overlay file must be yaml"))
		}
		prefix := root + "/"
		if !strings.HasPrefix(p, prefix) {
			return fmt.Errorf("skill overlay template file %q is not under %q", p, root)
		}
		content, err := fs.ReadFile(agentHelperTemplates, p)
		if err != nil {
			return err
		}
		overlay, err := decodeSkillOverlayFile(p, content)
		if err != nil {
			return err
		}
		out = append(out, loadedSkillOverlay{
			Template:      template,
			TemplateOrder: order,
			Path:          strings.TrimPrefix(p, prefix),
			Overlay:       overlay,
		})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read skill overlay template files %q: %w", template, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func decodeSkillOverlayFile(filePath string, content []byte) (skillOverlay, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var overlay skillOverlay
	if err := decoder.Decode(&overlay); err != nil {
		return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("overlay_yaml_decode_error", "", filePath, "", "", "%v", err))
	}
	if strings.TrimSpace(overlay.APIVersion) != skillOverlayAPIVersion {
		return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("unsupported_api_version", overlay.TargetSkill, filePath, "", overlay.ID, "apiVersion must be %s", skillOverlayAPIVersion))
	}
	if strings.TrimSpace(overlay.ID) == "" {
		return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "id is required"))
	}
	if strings.TrimSpace(overlay.TargetSkill) == "" {
		return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "targetSkill is required"))
	}
	if len(overlay.Fragments) == 0 {
		return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "fragments are required"))
	}
	for i, fragment := range overlay.Fragments {
		if strings.TrimSpace(fragment.Slot) == "" {
			return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, "", overlay.ID, "fragments[%d].slot is required", i))
		}
		if !validSkillOverlayOp(fragment.Op) {
			return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", overlay.TargetSkill, filePath, fragment.Slot, overlay.ID, "unsupported operation %q", fragment.Op))
		}
		if fragment.Order == 0 {
			overlay.Fragments[i].Order = 100
		}
		if fragment.Op != skillOverlayOpSuppress && strings.TrimSpace(fragment.Content) == "" {
			return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("missing_required_field", overlay.TargetSkill, filePath, fragment.Slot, overlay.ID, "content is required for %s", fragment.Op))
		}
		if fragment.Op == skillOverlayOpSuppress && strings.TrimSpace(fragment.Content) != "" {
			return skillOverlay{}, skillOverlayErr(skillOverlayIssuef("invalid_operation", overlay.TargetSkill, filePath, fragment.Slot, overlay.ID, "suppress fragments must not include content"))
		}
	}
	return overlay, nil
}

func validSkillOverlayOp(op skillOverlayOp) bool {
	switch op {
	case skillOverlayOpPrepend, skillOverlayOpAppend, skillOverlayOpReplace, skillOverlayOpSuppress:
		return true
	default:
		return false
	}
}

func applySkillOverlays(skills []skillTemplate, overlays []loadedSkillOverlay, templates ...string) ([]skillTemplate, skillOverlayRenderReport, error) {
	report := skillOverlayRenderReport{Templates: normalizeSkillOverlayReportTemplates(templates, overlays)}
	out := cloneSkillTemplates(skills)
	if len(out) == 0 {
		return out, report, nil
	}
	var issues []skillOverlayIssue
	overlayIDs := map[string]loadedSkillOverlay{}
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
		return nil, report, skillOverlayError{Issues: issues}
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

func cloneSkillTemplates(skills []skillTemplate) []skillTemplate {
	if len(skills) == 0 {
		return nil
	}
	out := make([]skillTemplate, len(skills))
	for i, skill := range skills {
		out[i].Name = skill.Name
		out[i].Files = append([]skillTemplateFile(nil), skill.Files...)
	}
	return out
}

func renderSkillTemplate(skillName string, skill skillTemplate, fragments []resolvedSkillOverlayFragment, report *skillOverlayRenderReport) (skillTemplate, error) {
	known := map[string]bool{}
	for _, file := range skill.Files {
		if file.IsMarkdown {
			known[filepath.ToSlash(file.Path)] = true
		}
	}
	var issues []skillOverlayIssue
	for _, fragment := range fragments {
		if !known[fragment.File] {
			issues = append(issues, skillOverlayIssuef("unknown_target_file", skillName, fragment.SourcePath, fragment.Slot, fragment.OverlayID, "target file %q is not a Markdown file of skill %s", fragment.File, skillName))
		}
	}
	if len(issues) > 0 {
		return skillTemplate{}, skillOverlayError{Issues: issues}
	}
	for i, file := range skill.Files {
		if !file.IsMarkdown {
			if strings.Contains(string(file.Content), "rzm:skill-slot") {
				return skillTemplate{}, skillOverlayErr(skillOverlayIssuef("invalid_slot_markup", skillName, file.Path, "", "", "skill slots are only supported in Markdown files"))
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
			return skillTemplate{}, err
		}
		if report != nil {
			report.Entries = append(report.Entries, entries...)
		}
		skill.Files[i].Content = []byte(rendered)
	}
	return skill, nil
}

func renderSkillTemplateMarkdown(skillName, filePath, body string, fragments []resolvedSkillOverlayFragment) (string, []skillOverlayManifestEntry, error) {
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
	var issues []skillOverlayIssue
	for _, fragment := range fragments {
		if _, ok := slotIDs[fragment.Slot]; !ok {
			issues = append(issues, skillOverlayIssuef("unknown_slot", skillName, filePath, fragment.Slot, fragment.OverlayID, "slot %q is not declared by %s", fragment.Slot, skillName))
		}
	}
	if len(issues) > 0 {
		return "", nil, skillOverlayError{Issues: issues}
	}
	var b strings.Builder
	var entries []skillOverlayManifestEntry
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
			return skillOverlaySlotRender{Entries: []skillOverlayManifestEntry{skillOverlaySlotEntry(skillName, slot.ID, "", "stripped_empty_slot", "", "", "", 0, "")}}, nil
		}
		body := strings.TrimSpace(slot.Body)
		return skillOverlaySlotRender{
			Content: body,
			Entries: []skillOverlayManifestEntry{skillOverlaySlotEntry(skillName, slot.ID, "", "preserved_default", "", "", "", 0, body)},
		}, nil
	}
	var prepend, appendItems []string
	var entries []skillOverlayManifestEntry
	body := strings.TrimSpace(slot.Body)
	replaced := false
	suppressed := false
	var replaceOverlay, suppressOverlay string
	for _, fragment := range fragments {
		switch fragment.Op {
		case skillOverlayOpPrepend:
			prepend = append(prepend, strings.TrimSpace(fragment.Content))
			entries = append(entries, skillOverlayFragmentEntry(skillName, fragment, "applied"))
		case skillOverlayOpAppend:
			appendItems = append(appendItems, strings.TrimSpace(fragment.Content))
			entries = append(entries, skillOverlayFragmentEntry(skillName, fragment, "applied"))
		case skillOverlayOpReplace:
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
		case skillOverlayOpSuppress:
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
			if fragment.Op != skillOverlayOpPrepend && fragment.Op != skillOverlayOpAppend {
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

func normalizeSkillOverlayReportTemplates(templates []string, overlays []loadedSkillOverlay) []string {
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

func appendSkillOverlayReportError(report *skillOverlayRenderReport, err error) {
	if report == nil || err == nil {
		return
	}
	var overlayErr skillOverlayError
	if errors.As(err, &overlayErr) {
		report.Issues = append(report.Issues, overlayErr.Issues...)
	}
}

func skillOverlayFragmentEntry(skillName string, fragment resolvedSkillOverlayFragment, outcome string) skillOverlayManifestEntry {
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

func skillOverlaySlotEntry(skillName, slot, op, outcome, overlayID, sourceTemplate, sourcePath string, order int, content string) skillOverlayManifestEntry {
	entry := skillOverlayManifestEntry{
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
			if ok, fc, fl, next := parseMarkdownFenceLine(body, i); ok {
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
				i = nextLineStart(body, i)
				continue
			}
			lineEnd := nextLineStart(body, i)
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
		i = nextLineStart(body, i)
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
		if ok, fc, fl, next := parseMarkdownFenceLine(body, i); ok {
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
			lineEnd := nextLineStart(body, i)
			line := strings.TrimSpace(strings.TrimSuffix(body[i:lineEnd], "\n"))
			if strings.HasPrefix(line, "<!-- rzm:skill-slot ") || line == "<!-- /rzm:skill-slot -->" {
				return true
			}
		}
		i = nextLineStart(body, i)
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
