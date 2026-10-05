package init

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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
func renderRhizomeMd(localCfg obsidian.LocalConfig) (string, error) {
	template, err := loadRhizomeMdTemplate("RHIZOME.md")
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(template, "{{REPO_CONFIG}}", renderRepoConfig(localCfg)), nil
}

func localHasCodeScanning(cfg obsidian.LocalConfig) bool {
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
func localHasEnabledAnchors(cfg obsidian.LocalConfig) bool {
	return obsidian.CodeAnchorRootsConfigured(cfg.Code)
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
func renderRepoConfig(localCfg obsidian.LocalConfig) string {
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
	if strings.Contains(s, startMarker) && strings.Contains(s, endMarker) {
		start := strings.Index(s, startMarker)
		end := strings.Index(s, endMarker)
		if start >= 0 && end > start {
			end += len(endMarker)
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
	}
	return s
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
		start := strings.Index(s, managedTemplateBlockStartPrefix)
		if start < 0 {
			return s
		}
		lineEnd := strings.Index(s[start:], "\n")
		if lineEnd < 0 {
			return strings.TrimRight(s[:start], "\n")
		}
		lineEnd += start
		startLine := strings.TrimSpace(s[start : lineEnd+1])
		template := strings.TrimSuffix(strings.TrimPrefix(startLine, managedTemplateBlockStartPrefix), managedTemplateBlockSuffix)
		if template == startLine {
			return s
		}
		endMarker := managedTemplateBlockEndPrefix + template + managedTemplateBlockSuffix
		end := strings.Index(s[lineEnd+1:], endMarker)
		if end < 0 {
			return strings.TrimRight(s[:start], "\n")
		}
		end += lineEnd + 1 + len(endMarker)
		if preserve != nil && preserve[template] {
			next := strings.Index(s[end:], managedTemplateBlockStartPrefix)
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
	start := strings.Index(s, startMarker)
	if start < 0 {
		return false
	}
	return strings.Index(s[start:], endMarker) >= 0
}

func replaceDelimitedBlock(s, startMarker, endMarker, replacement string) string {
	start := strings.Index(s, startMarker)
	if start < 0 {
		return s
	}
	end := strings.Index(s[start:], endMarker)
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
