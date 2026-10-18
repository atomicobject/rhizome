package init

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/skilloverlay"
)

// rhizomePrefix is the prefix for all rhizome-managed artifacts (commands, prompts, skills).
// All rhizome artifacts use hyphenated names: rhizome-onboard.md, rhizome-notes/, etc.
const rhizomePrefix = "rhizome-"
const coreRhizomeSkillName = "rhizome"

var retiredCoreSkillNames = []string{
	"rhizome-note-authoring",
	"rhizome-onboard",
	"rhizome-ontology",
	"rhizome-skill-creator",
}

// legacyNameOwnedSkills predate ownership markers; their names alone mark them
// as Rhizome's.
var legacyNameOwnedSkills = []string{"rhizome-hub-notes", "rhizome-old-core-skill"}

// AgentSurfaceOptions carries starter-management state into agent surface
// planning. Ejected starters keep their managed blocks and skills as they are.
type AgentSurfaceOptions struct {
	PreserveTemplateBlocks       []string
	PreserveLegacyTemplateBlocks bool
	PreserveSkillTemplates       []string
}

// SkippablePathError indicates a path-related issue that can be warned about
// and skipped rather than causing init to fail entirely.
type SkippablePathError struct {
	Path    string
	Message string
}

func (e *SkippablePathError) Error() string {
	return e.Message
}

// IsSkippablePathError checks if an error is a SkippablePathError.
func IsSkippablePathError(err error) bool {
	var spe *SkippablePathError
	return errors.As(err, &spe)
}

// skillSurface is one agent folder that receives skills.
type skillSurface struct {
	rel string // project-relative skills folder, e.g. ".agents/skills"
}

func enabledSkillSurfaces(harnesses AgentHarnesses) []skillSurface {
	var surfaces []skillSurface
	if harnesses.HasAgentSkills {
		surfaces = append(surfaces, skillSurface{rel: ".agents/skills"})
	}
	if harnesses.HasClaude {
		surfaces = append(surfaces, skillSurface{rel: ".claude/skills"})
	}
	return surfaces
}

// planAgentSurfaces adds every agent-facing file to the plan: managed blocks
// in AGENTS.md and CLAUDE.md, harness rules and commands, and skills in each
// enabled skill folder, plus removals for skills Rhizome no longer ships.
//
// Docs:
// - [[init-starter-workflow#^spec-0038-us2-ac2]]
// - [[agent-surface-integration-modes]]
// - [Init - Agent surfaces](docs/reference/guides/Init - Agent surfaces (prompts, commands, skills).md)
func planAgentSurfaces(projectRoot string, harnesses AgentHarnesses, section string, templates []string, opts AgentSurfaceOptions, record *generatedFiles, plan *filePlan) error {
	if !agentSurfacesEnabled(harnesses) {
		return nil
	}
	if err := checkCoreRhizomeSkillOwnership(projectRoot, harnesses, record); err != nil {
		return err
	}

	preserved := preservedTemplateBlockIDs(opts.PreserveTemplateBlocks)
	if opts.PreserveLegacyTemplateBlocks && !stringSet(preserved)[legacyTemplateSpecDriven] {
		preserved = append(preserved, legacyTemplateSpecDriven)
	}
	blocks, err := renderManagedDocBlocks(section, templates)
	if err != nil {
		return err
	}

	// AGENTS.md is the shared instruction file unless explicitly disabled.
	if !harnesses.AgentsMdDisabled {
		plan.docs = append(plan.docs, generatedDoc{rel: "AGENTS.md", header: "# Repository Guidelines", blocks: blocks, preserve: preserved})
	}
	if harnesses.HasClaude {
		claudeBlocks := blocks
		// WHY: Claude Code inlines an `@AGENTS.md` include, so a full managed
		// block in both files puts the same guidance in context twice per turn.
		if !harnesses.AgentsMdDisabled && claudeDocIncludesAgentsMd(filepath.Join(projectRoot, "CLAUDE.md")) {
			claudeBlocks = []managedDocBlock{{key: "rhizome", content: claudeAgentsMdPointer}}
		}
		plan.docs = append(plan.docs, generatedDoc{rel: "CLAUDE.md", header: "# Claude", blocks: claudeBlocks, preserve: preserved})
	}

	commands, err := renderCommandTemplates()
	if err != nil {
		return err
	}
	var commandDirs []string
	if harnesses.HasCursor {
		plan.addFile(generatedFile{rel: ".cursor/rules/rhizome.mdc", content: []byte(buildCursorRhizomeRule())})
		commandDirs = append(commandDirs, ".cursor/commands")
	}
	if harnesses.HasCodex {
		// RATIONALE: prompt targets mirror command helpers until prompt
		// artifacts need distinct source ownership or refresh semantics.
		// Docs: [[Keep prompt targets command-backed until prompt semantics diverge]]
		commandDirs = append(commandDirs, ".codex/prompts", ".codex/commands")
	}
	if harnesses.HasClaude {
		commandDirs = append(commandDirs, ".claude/commands", ".claude/prompts")
	}
	currentCommands := map[string]struct{}{}
	for _, cmd := range commands {
		currentCommands[cmd.name] = struct{}{}
	}
	for _, dir := range commandDirs {
		plan.commandDirs = append(plan.commandDirs, dir)
		plan.currentCommands = currentCommands
		for _, cmd := range commands {
			plan.addFile(generatedFile{rel: dir + "/" + cmd.name, content: cmd.content, group: "command:" + cmd.name})
		}
	}

	skills, err := loadAllSkillTemplates(templates)
	if err != nil {
		return err
	}
	rendered, err := renderSkillTemplates(skills)
	if err != nil {
		return err
	}
	for _, surface := range enabledSkillSurfaces(harnesses) {
		if err := planSkillSurface(projectRoot, surface, rendered, opts, record, plan); err != nil {
			return err
		}
	}
	return nil
}

type renderedCommand struct {
	name    string
	content []byte
}

func renderCommandTemplates() ([]renderedCommand, error) {
	templates, err := loadCommandTemplates()
	if err != nil {
		return nil, err
	}
	out := make([]renderedCommand, 0, len(templates))
	for _, tmpl := range templates {
		content, err := expandAgentTemplateEmbeds(applyPlaceholders(tmpl.Content, agentTemplateReplacements))
		if err != nil {
			return nil, err
		}
		if content == "" {
			return nil, fmt.Errorf("agent template %q produced empty output", tmpl.Name)
		}
		out = append(out, renderedCommand{name: tmpl.Name, content: []byte(content + "\n")})
	}
	return out, nil
}

var agentTemplateReplacements = map[string]string{"{{RHI_ZOME_CHUNK}}": "`AGENTS.md`"}

type renderedSkill struct {
	name  string
	files []renderedSkillFile
}

type renderedSkillFile struct {
	path    string // slash path inside the skill folder
	content []byte
	mode    os.FileMode
}

// Docs:
// - [[init-starter-workflow#^spec-0038-us2-ac3]]
// - [[init-template-architecture#^spec-0039-us2-ac1]]
func renderSkillTemplates(templates []skilloverlay.Skill) ([]renderedSkill, error) {
	out := make([]renderedSkill, 0, len(templates))
	for _, tmpl := range templates {
		if strings.TrimSpace(tmpl.Name) == "" {
			return nil, fmt.Errorf("skill template has empty name")
		}
		skill := renderedSkill{name: tmpl.Name}
		for _, file := range tmpl.Files {
			path := filepath.ToSlash(file.Path)
			content, _, err := renderSkillTemplateFile(tmpl, path)
			if err != nil {
				return nil, err
			}
			if file.IsMarkdown && strings.TrimSpace(string(content)) == "" {
				return nil, fmt.Errorf("skill template %q produced empty output for %q", tmpl.Name, file.Path)
			}
			mode := os.FileMode(0o644)
			if strings.HasPrefix(path, "scripts/") {
				mode = 0o755
			}
			skill.files = append(skill.files, renderedSkillFile{path: path, content: content, mode: mode})
		}
		out = append(out, skill)
	}
	return out, nil
}

// renderSkillTemplateFile returns the bytes rzm init writes for one template file.
func renderSkillTemplateFile(tmpl skilloverlay.Skill, path string) ([]byte, bool, error) {
	for _, file := range tmpl.Files {
		if filepath.ToSlash(file.Path) != path {
			continue
		}
		if !file.IsMarkdown {
			return file.Content, true, nil
		}
		content, err := expandAgentTemplateEmbeds(applyPlaceholders(string(file.Content), agentTemplateReplacements))
		if err != nil {
			return nil, true, err
		}
		return []byte(content + "\n"), true, nil
	}
	return nil, false, nil
}

// planSkillSurface plans one skills folder: every shipped skill file, files a
// newer version dropped from a shipped skill, and whole skills Rhizome no
// longer ships.
func planSkillSurface(projectRoot string, surface skillSurface, skills []renderedSkill, opts AgentSurfaceOptions, record *generatedFiles, plan *filePlan) error {
	shipped := map[string]renderedSkill{}
	for _, skill := range skills {
		shipped[skill.name] = skill
	}
	keep := map[string]bool{}
	for name := range shipped {
		keep[name] = true
	}
	if err := addPreservedSkillNames(keep, opts.PreserveSkillTemplates); err != nil {
		return err
	}

	for _, skill := range skills {
		dir := surface.rel + "/" + skill.name
		absDir := filepath.Join(projectRoot, filepath.FromSlash(dir))
		if hasLegacySkillMarker(absDir) {
			record.foldLegacySkillFolder(projectRoot, dir)
			plan.legacyDirs = append(plan.legacyDirs, dir)
		}
		files := map[string]bool{}
		for _, file := range skill.files {
			files[file.path] = true
			plan.addFile(generatedFile{
				rel:     dir + "/" + file.path,
				content: file.content,
				mode:    file.mode,
				group:   "skill:" + skill.name + "/" + file.path,
			})
		}
		// A file Rhizome wrote that the new version no longer ships is removed
		// once SKILL.md stops pointing at it.
		for _, rel := range recordedFilesUnder(record, dir) {
			if files[strings.TrimPrefix(rel, dir+"/")] {
				continue
			}
			plan.removals = append(plan.removals, generatedRemoval{
				rel:      rel,
				group:    "skill:" + skill.name + "/" + strings.TrimPrefix(rel, dir+"/"),
				requires: dir + "/SKILL.md",
			})
		}
	}

	return planStaleSkills(projectRoot, surface, keep, shipped, record, plan)
}

// planStaleSkills removes skills Rhizome used to ship: retired core and
// starter skills, and starter skills whose starter is no longer active. Only
// folders Rhizome owns are touched.
func planStaleSkills(projectRoot string, surface skillSurface, keep map[string]bool, shipped map[string]renderedSkill, record *generatedFiles, plan *filePlan) error {
	starterSkillNames, err := knownStarterSkillNames()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(projectRoot, filepath.FromSlash(surface.rel)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || keep[name] {
			continue
		}
		_, starterOwned := starterSkillNames[name]
		retiredCore := isRetiredCoreSkillName(name) || contains(legacyNameOwnedSkills, name)
		retiredStarter := isRetiredStarterSkillName(name)
		if !retiredCore && !retiredStarter && !starterOwned {
			continue
		}
		dir := surface.rel + "/" + name
		absDir := filepath.Join(projectRoot, filepath.FromSlash(dir))
		legacyMarker := hasLegacySkillMarker(absDir)
		// Retired rhizome-* names are Rhizome's by name. Other folders need the
		// record or an older marker, because names like "plan" may be a team's
		// own skill.
		if !retiredCore && !legacyMarker && !record.ownsUnder(dir) {
			continue
		}
		// Rhizome remembers the skills it retired and cleans them up without
		// asking; only a recorded file someone edited still gets the edit check.
		retired := retiredCore || retiredStarter
		if legacyMarker {
			record.foldLegacySkillFolder(projectRoot, dir)
			plan.legacyDirs = append(plan.legacyDirs, dir)
		}
		// With a record, only files Rhizome wrote are candidates; anything
		// someone added to the folder stays. Older folders without a record
		// offer every file for a decision.
		recordedOnly := record.ownsUnder(dir) && !legacyMarker && !retired
		// Retired skills go only once their replacement is in place.
		requires := ""
		switch {
		case retiredCore:
			requires = surface.rel + "/" + coreRhizomeSkillName + "/SKILL.md"
		case retiredStarter:
			if _, ok := shipped[templateAgenticEngineering]; ok {
				requires = surface.rel + "/" + templateAgenticEngineering + "/SKILL.md"
			}
		}
		err := filepath.WalkDir(absDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if filepath.Dir(path) == absDir && contains(legacyManagedSkillFiles, d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(projectRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if _, recorded := record.Written[rel]; recordedOnly && !recorded {
				return nil
			}
			plan.removals = append(plan.removals, generatedRemoval{
				rel:      rel,
				group:    "skill:" + name + "/" + strings.TrimPrefix(rel, dir+"/"),
				requires: requires,
				pruneDir: dir,
				retired:  retired,
			})
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func recordedFilesUnder(record *generatedFiles, dir string) []string {
	prefix := dir + "/"
	var out []string
	for key := range record.Written {
		if strings.HasPrefix(key, prefix) && !strings.Contains(key, "#") {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// checkCoreRhizomeSkillOwnership refuses to overwrite a skill folder named
// "rhizome" that someone else created. An unreadable record proves nothing
// either way, so its files go through the unknown-files question instead.
func checkCoreRhizomeSkillOwnership(projectRoot string, harnesses AgentHarnesses, record *generatedFiles) error {
	if record.unreadable {
		return nil
	}
	var collisions []string
	for _, surface := range enabledSkillSurfaces(harnesses) {
		dir := surface.rel + "/" + coreRhizomeSkillName
		absDir := filepath.Join(projectRoot, filepath.FromSlash(dir))
		info, err := os.Stat(absDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect Rhizome skill ownership at %s: %w", absDir, err)
		}
		if info.IsDir() && !record.ownsUnder(dir) && !hasLegacySkillMarker(absDir) && !coreRouterIsCurrent(absDir) {
			collisions = append(collisions, filepath.ToSlash(absDir))
		}
	}
	if len(collisions) == 0 {
		return nil
	}
	return fmt.Errorf("Rhizome skill collision: refusing to overwrite unmarked user-owned skill directory(s): %s; rename or remove the user skill, or run rzm init --agents none to stop managing agent files", strings.Join(collisions, ", "))
}

// coreRouterIsCurrent reports whether the folder's SKILL.md is exactly what
// this version ships, which proves Rhizome wrote it even when a clone lacks
// the record.
func coreRouterIsCurrent(skillDir string) bool {
	got, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return false
	}
	skills, err := loadAllSkillTemplates(nil)
	if err != nil {
		return false
	}
	for _, tmpl := range skills {
		if tmpl.Name != coreRhizomeSkillName {
			continue
		}
		want, shipped, err := renderSkillTemplateFile(tmpl, "SKILL.md")
		return err == nil && shipped && contentFingerprint(got) == contentFingerprint(want)
	}
	return false
}

func agentSurfacesEnabled(harnesses AgentHarnesses) bool {
	return harnesses.HasAgents ||
		harnesses.HasAgentSkills ||
		harnesses.HasClaude ||
		harnesses.HasCursor ||
		harnesses.HasCodex
}
