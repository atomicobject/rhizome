package validate

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	issueCodeFrontmatterYAML     = "code_frontmatter_yaml_error"
	issueCodeAnchorDefinition    = "code_anchor_definition_error"
	fixKindRepairCodeFrontmatter = "repair_code_frontmatter"
)

// RunCodeFrontmatter validates that every note's frontmatter parses (code-anchor
// definitions live there) and that code-anchor definitions are valid, preserving
// the exact source path plus parser diagnostic needed to repair each finding.
// Obsidian template files whose unparseable frontmatter holds Templater or core
// template placeholders are skipped and reported in Notes.
func RunCodeFrontmatter(ctx context.Context, runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckCodeFrontmatter, OK: true}
	reader := runCtx.NoteReader
	if reader == nil {
		reader = &obsidian.Note{}
	}
	vaultDef := runCtx.VaultDef
	if strings.TrimSpace(vaultDef.BasePath()) == "" {
		vaultDef.Path = runCtx.VaultPath
	}
	runCtx.NoteReader = reader
	runCtx.VaultDef = vaultDef
	sources, err := markdownValidationSources(ctx, runCtx)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	var skippedTemplates []string
	for _, source := range sources {
		notePath := source.Path.String()
		content := source.Content
		if _, yamlErr := obsidian.ExtractFrontmatter(content); yamlErr != nil {
			// Skip a template only when its template syntax is what keeps the
			// frontmatter from parsing.
			if neutral, ok := neutralizeTemplateFrontmatter(content); ok {
				if _, err := obsidian.ExtractFrontmatter(neutral); err == nil {
					skippedTemplates = append(skippedTemplates, notePath)
					continue
				}
			}
			result.Issues = append(result.Issues, codeFrontmatterIssue(notePath, issueCodeFrontmatterYAML, yamlErr))
			continue
		}
		if _, anchorErr := codeanchor.ParseNote(notePath, content); anchorErr != nil {
			result.Issues = append(result.Issues, codeFrontmatterIssue(notePath, issueCodeAnchorDefinition, anchorErr))
		}
	}
	if len(skippedTemplates) > 0 {
		sort.Strings(skippedTemplates)
		result.Notes = append(result.Notes, fmt.Sprintf(
			"skipped %d template note(s) with Templater or {{...}} placeholders in unparseable frontmatter: %s",
			len(skippedTemplates), strings.Join(skippedTemplates, ", "),
		))
	}
	result.IssueCount = len(result.Issues)
	result.Fixes, err = buildCodeFrontmatterFixes(result.Issues)
	if err != nil {
		result.OK = false
		result.Error = fmt.Sprintf("identify code-frontmatter issue: %v", err)
		result.IssueCount = 0
		result.Issues = nil
		result.Fixes = nil
		return result
	}
	if len(sources) == 0 {
		result.Skipped = true
		result.Summary = "no notes to validate"
	} else if result.IssueCount == 0 {
		result.Summary = fmt.Sprintf("%d notes checked", len(sources))
	} else {
		result.Summary = fmt.Sprintf("%d invalid notes (of %d)", result.IssueCount, len(sources))
	}
	return result
}

var frontmatterTemplateSyntax = regexp.MustCompile(`(?s)<%.*?%>|\{\{.*?\}\}`)

// neutralizeTemplateFrontmatter returns content with each Templater
// `<% ... %>` or core `{{...}}` span in its frontmatter neutralized, and
// whether there was any.
func neutralizeTemplateFrontmatter(content string) (string, bool) {
	if !strings.HasPrefix(content, "---") {
		return "", false
	}
	end := strings.Index(content[3:], "\n---")
	if end == -1 {
		return "", false
	}
	block := content[3 : 3+end]
	if !frontmatterTemplateSyntax.MatchString(block) {
		return "", false
	}
	return "---" + neutralizeTemplateSyntax(block) + content[3+end:], true
}

// neutralizeTemplateSyntax replaces an inline template span with a plain
// scalar and drops a span that fills its own lines (a Templater statement).
func neutralizeTemplateSyntax(block string) string {
	var b strings.Builder
	cursor := 0
	for _, loc := range frontmatterTemplateSyntax.FindAllStringIndex(block, -1) {
		lineStart := strings.LastIndexByte(block[:loc[0]], '\n') + 1
		lineEnd := len(block)
		if next := strings.IndexByte(block[loc[1]:], '\n'); next >= 0 {
			lineEnd = loc[1] + next
		}
		standalone := strings.TrimSpace(block[lineStart:loc[0]]) == "" && strings.TrimSpace(block[loc[1]:lineEnd]) == ""
		b.WriteString(block[cursor:loc[0]])
		if !standalone {
			b.WriteString("template")
		}
		cursor = loc[1]
	}
	b.WriteString(block[cursor:])
	return b.String()
}

func codeFrontmatterIssue(notePath, code string, diagnostic error) Issue {
	guidance := fmt.Sprintf("Edit code-anchors frontmatter in %s, then run `rzm validate code-frontmatter`.", notePath)
	return Issue{
		Code: code, Path: notePath, Field: "code-anchors",
		Message: fmt.Sprintf("invalid code frontmatter: %v", diagnostic),
		Data: mustMarshal(CodeFrontmatterData{
			Diagnostic: diagnostic.Error(), Guidance: guidance,
		}),
	}
}

func buildCodeFrontmatterFixes(issues []Issue) ([]FixAction, error) {
	actions := make([]FixAction, 0, len(issues))
	for _, issue := range issues {
		issueKey, err := StableIssueKey(CheckCodeFrontmatter, issue)
		if err != nil {
			return nil, err
		}
		actions = append(actions, FixAction{
			ID:    "repair-code-frontmatter:" + issue.Code + ":" + issue.Path,
			Check: CheckCodeFrontmatter, IssueCode: issue.Code,
			Kind: fixKindRepairCodeFrontmatter, Safety: FixSafetyAgent,
			Title:         "Repair code-anchor frontmatter",
			Summary:       issue.Message,
			InstanceCount: 1, IssueKeys: []string{issueKey}, AffectedPaths: []string{issue.Path},
		})
	}
	return actions, nil
}
