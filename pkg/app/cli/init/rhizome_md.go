package init

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

const (
	rhizomeStart                    = "<!--- RHIZOME START -->"
	rhizomeEnd                      = "<!--- RHIZOME END -->"
	legacyManagedAgentBlockStart    = "<!-- BEGIN RZM INIT MANAGED AGENT BLOCK -->"
	legacyManagedAgentBlockEnd      = "<!-- END RZM INIT MANAGED AGENT BLOCK -->"
	managedRhizomeBlockStart        = "<!-- BEGIN RZM INIT RHIZOME BLOCK -->"
	managedRhizomeBlockEnd          = "<!-- END RZM INIT RHIZOME BLOCK -->"
	managedTemplateBlockStartPrefix = "<!-- BEGIN RZM INIT TEMPLATE BLOCK: "
	managedTemplateBlockEndPrefix   = "<!-- END RZM INIT TEMPLATE BLOCK: "
	managedTemplateBlockSuffix      = " -->"
)

// Docs:
// - [RHIZOME.md templates + rzm init](docs/reference/guides/RHIZOME.md templates + rzm init.md)
// - [[init-template-architecture]]
// renderRhizomeMd renders the core Rhizome guidance block from canonical
// repo-local config.
func renderRhizomeMd(localCfg repoconfig.LocalConfig) (string, error) {
	template, err := loadRhizomeMdTemplate("RHIZOME.md")
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(template, "{{REPO_CONFIG}}", renderRepoConfig(localCfg)), nil
}

func localHasCodeScanning(cfg repoconfig.LocalConfig) bool {
	if cfg.Code.Enabled {
		return true
	}
	if len(cfg.Code.Scan) > 0 {
		return true
	}
	if cfg.Code.Python != nil || cfg.Code.Go != nil || cfg.Code.TypeScript != nil || cfg.Code.JavaScript != nil || cfg.Code.CSharp != nil || cfg.Code.PHP != nil {
		return true
	}
	return false
}

// localHasEnabledAnchors checks if canonical config enables code anchors.
func localHasEnabledAnchors(cfg repoconfig.LocalConfig) bool {
	return repoconfig.CodeAnchorRootsConfigured(cfg.Code)
}

func renderDocPatternGuidance(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	joined := fmt.Sprintf("`%s`", strings.Join(patterns, "`, `"))

	var out []string

	hasReadme := false
	hasContext := false
	for _, p := range patterns {
		if strings.EqualFold(strings.TrimSpace(p), "README.md") {
			hasReadme = true
		}
		if strings.EqualFold(strings.TrimSpace(p), "CONTEXT.md") {
			hasContext = true
		}
	}

	switch {
	case hasReadme && hasContext:
		out = append(out, fmt.Sprintf("- **Module docs convention**: keep `%s` / `%s` near each major directory to explain scope, invariants, and entry points, and link/`![[embed]]` deeper design/runbook notes.", "README.md", "CONTEXT.md"))
	case hasReadme:
		out = append(out, fmt.Sprintf("- **Module docs convention**: use `%s` as directory-level context: what this module does, invariants/sharp edges, key entry points, and links/`![[embed]]`s to deeper docs.", "README.md"))
	case hasContext:
		out = append(out, fmt.Sprintf("- **Module docs convention**: use `%s` as module context near code: what this module does, invariants/sharp edges, key entry points, and links/`![[embed]]`s to deeper docs.", "CONTEXT.md"))
	default:
		out = append(out, fmt.Sprintf("- **Module docs convention**: use %s near code to provide module context (scope, invariants, entry points) and links/`![[embed]]`s to deeper design/runbook docs.", joined))
	}

	return out
}

// renderRepoConfig renders the repo config summary from canonical LocalConfig.
func renderRepoConfig(localCfg repoconfig.LocalConfig) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("- **Rhizome config**: `%s`", ".rhizome/config.yml"))
	lines = append(lines, fmt.Sprintf("- **Ignore rules**: `%s` (preferred)", ".rhizome/ignore"))

	// Notes
	incs := localCfg.Notes.Includes
	if len(incs) == 0 {
		incs = []string{"**/*.md"}
	}
	lines = append(lines, fmt.Sprintf("- **Notes**: includes `%s`", strings.Join(incs, "`, `")))
	if localCfg.Notes.Links != "" && localCfg.Notes.Links != obsidian.LinkTypeBoth {
		lines = append(lines, fmt.Sprintf("- **Links mode**: `%s`", localCfg.Notes.Links))
	}

	// file_context docs
	if len(localCfg.FileCtx.DocPatterns) > 0 {
		lines = append(lines, fmt.Sprintf("- **`file_context` doc patterns**: `%s`", strings.Join(localCfg.FileCtx.DocPatterns, "`, `")))
		lines = append(lines, renderDocPatternGuidance(localCfg.FileCtx.DocPatterns)...)
	}

	// Code scope
	if localHasCodeScanning(localCfg) {
		if limits := codeFolderLimits(localCfg.Code); len(limits) > 0 {
			lines = append(lines, fmt.Sprintf("- **Code indexing**: limited to `%s`", strings.Join(limits, "`, `")))
		} else {
			lines = append(lines, "- **Code indexing**: whole repository; language from each file's extension")
		}
		if len(localCfg.Code.Scan) > 0 {
			lines = append(lines, fmt.Sprintf("- **Code scanning globs**: `%s`", strings.Join(localCfg.Code.Scan, "`, `")))
		}
	}

	// Disabled languages
	if len(localCfg.Code.DisabledLanguages) > 0 {
		lines = append(lines, fmt.Sprintf("- **Code anchors (never prompt)**: `%s`", strings.Join(localCfg.Code.DisabledLanguages, "`, `")))
	}

	// Anchors
	if localHasEnabledAnchors(localCfg) {
		lines = append(lines, "- **Code anchors**: enabled")
	}

	return strings.Join(lines, "\n")
}

type managedDocBlock struct {
	key     string
	content string
}

func buildManagedRhizomeBlock(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	return managedRhizomeBlockStart + "\n" + trimmed + "\n" + managedRhizomeBlockEnd
}

func buildManagedTemplateBlock(template string, content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.TrimSpace(template) == "" || trimmed == "" {
		return ""
	}
	start := managedTemplateBlockStartPrefix + template + managedTemplateBlockSuffix
	end := managedTemplateBlockEndPrefix + template + managedTemplateBlockSuffix
	return start + "\n" + trimmed + "\n" + end
}

func stripDelimitedBlock(s, startMarker, endMarker string) string {
	start := markerIndex(s, startMarker)
	if start < 0 {
		return s
	}
	end := markerIndex(s[start:], endMarker)
	if end < 0 {
		return s
	}
	end += start + len(endMarker)
	before := strings.TrimRight(s[:start], "\n")
	after := strings.TrimLeft(s[end:], "\n")
	if before != "" && after != "" {
		return before + "\n\n" + after
	}
	if before != "" {
		return before
	}
	return after
}

// markerLine returns the offset and text of the first marker in s that
// stands alone on its line and satisfies match, or -1. A marker quoted
// mid-line, in prose or inline code, is not a fence.
func markerLine(s string, match func(string) bool) (int, string) {
	for at := 0; at <= len(s); {
		lineEnd, next := strings.IndexByte(s[at:], '\n'), len(s)+1
		if lineEnd < 0 {
			lineEnd = len(s)
		} else {
			lineEnd += at
			next = lineEnd + 1
		}
		line := s[at:lineEnd]
		if trimmed := strings.TrimSpace(line); trimmed != "" && match(trimmed) {
			return at + strings.Index(line, trimmed), trimmed
		}
		at = next
	}
	return -1, ""
}

func markerIndex(s, marker string) int {
	at, _ := markerLine(s, func(line string) bool { return line == marker })
	return at
}

func isTemplateBlockStart(line string) bool {
	return len(line) > len(managedTemplateBlockStartPrefix)+len(managedTemplateBlockSuffix) &&
		strings.HasPrefix(line, managedTemplateBlockStartPrefix) && strings.HasSuffix(line, managedTemplateBlockSuffix)
}

// templateBlockStart returns the offset and marker of the first template
// BEGIN line in s, or -1.
func templateBlockStart(s string) (int, string) {
	return markerLine(s, isTemplateBlockStart)
}

func templateBlockKey(marker string) string {
	return strings.TrimSuffix(strings.TrimPrefix(marker, managedTemplateBlockStartPrefix), managedTemplateBlockSuffix)
}

// checkManagedFences refuses a doc in which a managed BEGIN line has no
// matching END line before the next BEGIN, so init stops before any write
// instead of dropping the text after the broken marker.
func checkManagedFences(rel, content string) error {
	open, openLine := "", 0
	for n, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != managedRhizomeBlockStart && !isTemplateBlockStart(trimmed) {
			if open != "" && trimmed == managedFenceEnd(open) {
				open = ""
			}
			continue
		}
		if open != "" {
			break
		}
		open, openLine = trimmed, n+1
	}
	if open == "" {
		return nil
	}
	return fmt.Errorf("malformed managed block in %s: %q on line %d has no matching %q line; restore the END line or delete the BEGIN line, then rerun rzm init", rel, open, openLine, managedFenceEnd(open))
}

func managedFenceEnd(begin string) string {
	return strings.Replace(begin, "<!-- BEGIN ", "<!-- END ", 1)
}

func stripLegacyRhizomeRedirect(s string) string {
	legacyRedirect := "STOP! IMPORTANT! You MUST read and follow @RHIZOME.md RIGHT NOW! Those rules are important to follow in this repository!\n\n@RHIZOME.md"
	if strings.TrimSpace(s) == legacyRedirect {
		return ""
	}
	s = strings.ReplaceAll(s, legacyRedirect, "")
	oldPhrasing := "If this repository contains a RHIZOME.md file, you MUST read and follow it."
	s = strings.ReplaceAll(s, oldPhrasing, "")
	return strings.TrimRight(s, "\n")
}

func stripManagedTemplateBlocksExcept(s string, preserve map[string]bool) string {
	for {
		start, marker := templateBlockStart(s)
		if start < 0 {
			return s
		}
		template := templateBlockKey(marker)
		bodyAt := start + len(marker)
		endMarker := managedTemplateBlockEndPrefix + template + managedTemplateBlockSuffix
		end := markerIndex(s[bodyAt:], endMarker)
		if end < 0 {
			// checkManagedFences refuses this before rendering; never truncate.
			return s
		}
		end += bodyAt + len(endMarker)
		if preserve != nil && preserve[template] {
			next, _ := templateBlockStart(s[end:])
			if next < 0 {
				return s
			}
			prefix := s[:end+next]
			suffix := stripManagedTemplateBlocksExcept(s[end+next:], preserve)
			return prefix + suffix
		}
		before := strings.TrimRight(s[:start], "\n")
		after := strings.TrimLeft(s[end:], "\n")
		if before != "" && after != "" {
			s = before + "\n\n" + after
			continue
		}
		if before != "" {
			s = before
			continue
		}
		s = after
	}
}

func renderAgentHarnessDocWithOptions(existing string, createHeader string, blocks []managedDocBlock, preserveTemplateBlocks []string) string {
	s := existing
	s = stripDelimitedBlock(s, rhizomeStart, rhizomeEnd)

	// WHY: old init embedded the same guidance without modern managed fences;
	// strip it so reruns converge to one refreshable block while preserving
	// unrelated repo instructions.
	// Docs: [[init-starter-workflow#^spec-0038-us3-ac1]]
	// Legacy embedded RHIZOME.md docs sometimes started with this heading.
	if idx := strings.Index(s, "## Rhizome (vault + agent CLI) guidelines"); idx >= 0 {
		afterHeader := s[idx+len("## Rhizome (vault + agent CLI) guidelines"):]
		nextHeading := strings.Index(afterHeader, "\n## ")
		if nextHeading >= 0 {
			before := strings.TrimRight(s[:idx], "\n")
			after := strings.TrimLeft(afterHeader[nextHeading+1:], "\n")
			if before != "" && after != "" {
				s = before + "\n\n" + after
			} else if before != "" {
				s = before
			} else {
				s = after
			}
		} else {
			s = strings.TrimRight(s[:idx], "\n")
		}
	}

	s = stripDelimitedBlock(s, legacyManagedAgentBlockStart, legacyManagedAgentBlockEnd)

	// WHY: preserved template blocks stay in place, so the refreshed core block
	// must keep its existing position instead of being re-appended after them.
	rhizomeBlock := ""
	for _, block := range blocks {
		if block.key == "rhizome" && strings.TrimSpace(block.content) != "" {
			rhizomeBlock = buildManagedRhizomeBlock(strings.TrimSpace(block.content))
		}
	}
	rhizomeInPlace := rhizomeBlock != "" && hasDelimitedBlock(s, managedRhizomeBlockStart, managedRhizomeBlockEnd)
	if rhizomeInPlace {
		s = replaceDelimitedBlock(s, managedRhizomeBlockStart, managedRhizomeBlockEnd, rhizomeBlockPlaceholder)
	} else {
		s = stripDelimitedBlock(s, managedRhizomeBlockStart, managedRhizomeBlockEnd)
	}
	s = stripManagedTemplateBlocksExcept(s, stringSet(normalizePreservedTemplateBlockIDs(preserveTemplateBlocks)))
	s = stripLegacyRhizomeRedirect(s)
	s = strings.TrimSpace(s)

	parts := make([]string, 0, 2+len(blocks))
	if s != "" {
		parts = append(parts, s)
	} else if createHeader != "" {
		parts = append(parts, createHeader)
	}
	for _, block := range blocks {
		trimmed := strings.TrimSpace(block.content)
		if trimmed == "" {
			continue
		}
		switch block.key {
		case "rhizome":
			if !rhizomeInPlace {
				parts = append(parts, rhizomeBlock)
			}
		default:
			parts = append(parts, buildManagedTemplateBlock(block.key, trimmed))
		}
	}
	out := strings.Join(parts, "\n\n") + "\n"
	if rhizomeInPlace {
		out = strings.Replace(out, rhizomeBlockPlaceholder, rhizomeBlock, 1)
	}
	return out
}

const rhizomeBlockPlaceholder = "<!-- RZM RHIZOME BLOCK PLACEHOLDER -->"

func hasDelimitedBlock(s, startMarker, endMarker string) bool {
	start := markerIndex(s, startMarker)
	if start < 0 {
		return false
	}
	return markerIndex(s[start:], endMarker) >= 0
}

func replaceDelimitedBlock(s, startMarker, endMarker, replacement string) string {
	start := markerIndex(s, startMarker)
	if start < 0 {
		return s
	}
	end := markerIndex(s[start:], endMarker)
	if end < 0 {
		return s
	}
	end += start + len(endMarker)
	return s[:start] + replacement + s[end:]
}

func normalizePreservedTemplateBlockIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	var out []string
	for _, raw := range ids {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
