package init

import (
	"fmt"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/skilloverlay"
)

func renderStarterManagedAgentDoc(template string) (managedDocBlock, error) {
	body, err := loadStarterManagedDocTemplate(template, "AGENTS.md")
	if err != nil || strings.TrimSpace(body) == "" {
		return managedDocBlock{}, err
	}
	skills, err := loadStarterSkillTemplates(template)
	if err != nil {
		return managedDocBlock{}, err
	}
	rendered := applyPlaceholders(body, map[string]string{
		"{{TEAM_SKILLS}}": renderTeamSkillLinks(skills),
	})
	return managedDocBlock{key: template, content: rendered}, nil
}

// Docs:
// - [[init-starter-workflow#^spec-0038-us2-ac2]]
// - [[init-template-architecture#^spec-0039-us1-ac1]]
func renderTeamSkillLinks(skills []skilloverlay.Skill) string {
	if len(skills) == 0 {
		return "- None bundled.\n"
	}
	lines := make([]string, 0, len(skills))
	for _, skill := range skills {
		lines = append(lines, fmt.Sprintf("- `%s`: `.agents/skills/%s/SKILL.md`", skill.Name, skill.Name))
	}
	return strings.Join(lines, "\n")
}

// claudeAgentsMdPointer replaces the full managed block in CLAUDE.md when that
// file already includes AGENTS.md.
const claudeAgentsMdPointer = "Rhizome guidance for this repository is the managed block in `AGENTS.md`, which this file includes with `@AGENTS.md`. `rzm init` keeps it there; do not copy it here."

// claudeDocIncludesAgentsMd reports whether CLAUDE.md pulls AGENTS.md in through
// a Claude Code `@` include line, so its guidance is already in context. Lines
// inside fenced code or HTML comments are examples, not includes.
func claudeDocIncludesAgentsMd(claudePath string) bool {
	content, err := os.ReadFile(claudePath)
	if err != nil {
		return false
	}
	fence, inComment := "", false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}
		if fence != "" {
			// Only a run of the opening character at least as long as the opener
			// closes the block; a different delimiter inside it is content.
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if opener := fenceOpener(trimmed); opener != "" {
			fence = opener
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			inComment = !strings.Contains(trimmed, "-->")
			continue
		}
		switch trimmed {
		case "@AGENTS.md", "@./AGENTS.md":
			return true
		}
	}
	return false
}

// fenceOpener returns the code-fence delimiter run that opens on this line, or
// an empty string when the line does not open a fence.
func fenceOpener(trimmed string) string {
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return ""
	}
	marker := trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == marker {
		n++
	}
	if n < 3 {
		return ""
	}
	return trimmed[:n]
}

func preservedTemplateBlockIDs(templates []string) []string {
	preserved := normalizeMetadataIDsWithCanonicalIDs(templates)
	for _, template := range preserved {
		if template == templateAgenticEngineering {
			return append(preserved, legacyTemplateSpecDriven)
		}
	}
	return preserved
}

func renderManagedDocBlocks(rhizomeContent string, templates []string) ([]managedDocBlock, error) {
	blocks := make([]managedDocBlock, 0, len(templates)+1)
	if trimmed := strings.TrimSpace(rhizomeContent); trimmed != "" {
		blocks = append(blocks, managedDocBlock{key: "rhizome", content: trimmed})
	}
	for _, template := range templates {
		block, err := renderStarterManagedAgentDoc(template)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(block.content) != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func managedBlockContent(content, start, end string) string {
	startAt := strings.Index(content, start)
	if startAt < 0 {
		return ""
	}
	bodyAt := startAt + len(start)
	endAt := strings.Index(content[bodyAt:], end)
	if endAt < 0 {
		return ""
	}
	return strings.TrimSpace(content[bodyAt : bodyAt+endAt])
}

func addPreservedSkillNames(keep map[string]bool, templates []string) error {
	for _, template := range templates {
		skillTemplates, err := loadStarterSkillTemplates(template)
		if err != nil {
			return err
		}
		for _, tmpl := range skillTemplates {
			keep[tmpl.Name] = true
		}
	}
	return nil
}

func buildCursorRhizomeRule() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("alwaysApply: true\n")
	b.WriteString("---\n\n")
	b.WriteString("STOP! IMPORTANT! You MUST read and follow `AGENTS.md` RIGHT NOW! Those rules are important to follow in this repository.\n\n")
	b.WriteString("If `CLAUDE.md` exists, it either carries the same managed Rhizome guidance or includes `AGENTS.md` with `@AGENTS.md` and holds only a pointer, plus any Claude-specific notes.\n")
	return b.String()
}
