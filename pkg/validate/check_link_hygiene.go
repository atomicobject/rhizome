package validate

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type linkHygieneIndex struct {
	notes         []string
	byKey         map[string][]string
	aliasesByNote map[string][]string
	// files resolves wikilinks with Obsidian's rules, so a real note or
	// attachment always wins over an alias and only `.md` is ever implied.
	files func() *obsidian.VaultFileIndex
}

type linkHygieneIssueData struct {
	Target      string `json:"target,omitempty"`
	Replacement string `json:"replacement,omitempty"`
	LinkType    string `json:"linkType,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type linkHygieneEdit struct {
	issueCode string
	start     int
	end       int
	value     string
	issueKey  string
}

type scannedMdLink struct {
	target          string
	text            string
	start           int
	end             int
	line            int
	insideCodeBlock bool
}

var pseudoLinkPatterns = []*regexp.Regexp{
	regexp.MustCompile(`<[A-Za-z0-9_-]+(?:-[A-Za-z0-9_-]+)*#\^[^>]+>`),
	regexp.MustCompile(`\[\s+\[[^\]]+\]\s+\]`),
}

// RunLinkHygiene checks that author-facing internal links use Obsidian-compatible
// wikilinks or source-relative Markdown note links.
func RunLinkHygiene(runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckLinkHygiene, OK: true}
	allNotes, err := runCtx.NoteReader.GetNotesList(runCtx.VaultDef)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}
	idx := buildLinkHygieneIndex(runCtx, allNotes)
	issues := make([]Issue, 0)
	editsByPath := map[string][]linkHygieneEdit{}

	for _, notePath := range allNotes {
		content, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, notePath)
		if err != nil {
			continue
		}
		for _, link := range obsidian.ScanWikilinks(content, obsidian.DefaultWikilinkOptions) {
			if link.InsideCodeBlock {
				continue
			}
			replacement, code, reason, ok := idx.wikilinkReplacement(notePath, link.Raw, link.Target)
			if code == "" {
				continue
			}
			issue := linkHygieneIssue(notePath, link.Line, code, "wikilink", link.Target, replacement, reason)
			issues = append(issues, issue)
			if ok {
				issueKey, err := StableIssueKey(CheckLinkHygiene, issue)
				if err != nil {
					result.OK = false
					result.Error = fmt.Sprintf("identify link-hygiene issue: %v", err)
					return result
				}
				editsByPath[notePath] = append(editsByPath[notePath], linkHygieneEdit{
					issueCode: code,
					start:     link.Start,
					end:       link.End,
					value:     replacement,
					issueKey:  issueKey,
				})
			}
		}
		for _, link := range scanMarkdownLinksForHygiene(content) {
			if link.insideCodeBlock || obsidianLinkIsExternal(link.target) || !isMarkdownNoteLink(link.target) {
				continue
			}
			replacement, code, reason, ok := idx.markdownReplacement(notePath, link)
			if code == "" {
				continue
			}
			issue := linkHygieneIssue(notePath, link.line, code, "mdlink", link.target, replacement, reason)
			issues = append(issues, issue)
			if ok {
				issueKey, err := StableIssueKey(CheckLinkHygiene, issue)
				if err != nil {
					result.OK = false
					result.Error = fmt.Sprintf("identify link-hygiene issue: %v", err)
					return result
				}
				editsByPath[notePath] = append(editsByPath[notePath], linkHygieneEdit{
					issueCode: code,
					start:     link.start,
					end:       link.end,
					value:     replacement,
					issueKey:  issueKey,
				})
			}
		}
		for _, pseudo := range scanPseudoLinks(content) {
			issues = append(issues, linkHygieneIssue(notePath, pseudo.line, "pseudo_link_placeholder", "placeholder", pseudo.target, "", "placeholder is not a valid Markdown link or Obsidian wikilink"))
		}
	}

	result.IssueCount = len(issues)
	if len(issues) == 0 {
		result.Summary = "no link hygiene issues"
		return result
	}
	result.Issues = append(result.Issues, issues...)
	result.Fixes = buildLinkHygieneFixes(editsByPath)
	result.OK = false
	result.Summary = fmt.Sprintf("%d link hygiene issues", len(issues))
	return result
}

func buildLinkHygieneIndex(runCtx RunContext, allNotes []string) linkHygieneIndex {
	idx := linkHygieneIndex{
		notes: append([]string(nil), allNotes...), byKey: map[string][]string{}, aliasesByNote: map[string][]string{},
		files: obsidian.LazyVaultFileIndex(runCtx.VaultDef, allNotes),
	}
	sort.Strings(idx.notes)
	for _, notePath := range idx.notes {
		idx.add(notePath, filepath.ToSlash(notePath))
		idx.add(notePath, path.Base(filepath.ToSlash(notePath)))
		if content, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, notePath); err == nil {
			if fm, err := obsidian.ExtractFrontmatter(content); err == nil {
				for _, alias := range obsidian.AliasListFromFrontmatter(fm) {
					idx.aliasesByNote[notePath] = append(idx.aliasesByNote[notePath], alias)
				}
			}
		}
		sort.Strings(idx.aliasesByNote[notePath])
	}
	return idx
}

func (i linkHygieneIndex) add(notePath, key string) {
	key = normalizeLinkHygieneKey(key)
	if key == "" {
		return
	}
	for _, existing := range i.byKey[key] {
		if existing == notePath {
			return
		}
	}
	i.byKey[key] = append(i.byKey[key], notePath)
}

func (i linkHygieneIndex) wikilinkReplacement(source, raw, target string) (string, string, string, bool) {
	if target == "" {
		return "", "wikilink_target_empty", "wikilink target is empty", false
	}
	if isFilesystemAbsoluteLink(target) {
		return "", "wikilink_target_absolute_path", "wikilink target uses a filesystem absolute path", false
	}
	if strings.HasPrefix(target, "/") {
		trimmed := strings.TrimPrefix(target, "/")
		if replacement, ok := i.rewriteWikilink(source, raw, target, trimmed); ok {
			return replacement, "wikilink_target_leading_slash", "wikilink target uses a leading vault-root slash", true
		}
		return "", "wikilink_target_leading_slash", "leading slash target cannot be rewritten unambiguously", false
	}
	base, fragment := splitLinkHygieneFragment(target)
	if strings.EqualFold(path.Ext(base), ".md") {
		next := strings.TrimSuffix(base, path.Ext(base)) + fragment
		if replacement, ok := i.rewriteWikilink(source, raw, target, next); ok {
			return replacement, "wikilink_target_has_md_extension", "wikilink target includes .md", true
		}
		return "", "wikilink_target_has_md_extension", "extensionless target cannot be proven equivalent", false
	}
	if _, ok := i.resolveWiki(source, target); ok {
		return "", "", "", false
	}
	if replacement, ok := i.aliasTargetReplacement(source, raw, base, fragment); ok {
		return replacement, "wikilink_target_is_alias", "wikilink target is a frontmatter alias; target the note and use the alias as display text", true
	}
	return "", "", "", false
}

func (i linkHygieneIndex) rewriteWikilink(source, raw, oldTarget, newTarget string) (string, bool) {
	oldResolved, oldOK := i.resolveWiki(source, oldTarget)
	newResolved, newOK := i.resolveWiki(source, newTarget)
	oldBase, _ := splitLinkHygieneFragment(oldTarget)
	newBase, _ := splitLinkHygieneFragment(newTarget)
	if !oldOK || !newOK || oldResolved != newResolved || i.files().Ambiguous(oldBase) || i.files().Ambiguous(newBase) {
		return "", false
	}
	open := strings.Index(raw, "[[")
	close := strings.LastIndex(raw, "]]")
	if open < 0 || close < open {
		return "", false
	}
	inner := raw[open+2 : close]
	alias := ""
	if pipe := strings.Index(inner, "|"); pipe >= 0 {
		alias = inner[pipe:]
	}
	return raw[:open+2] + newTarget + alias + raw[close:], true
}

func (i linkHygieneIndex) aliasTargetReplacement(source, raw, aliasTarget, fragment string) (string, bool) {
	inner, ok := wikilinkInner(raw)
	if !ok {
		return "", false
	}
	aliasTarget = strings.TrimSpace(aliasTarget)
	if aliasTarget == "" || strings.Contains(aliasTarget, "/") {
		return "", false
	}
	matches := i.notesWithAlias(aliasTarget)
	if len(matches) != 1 {
		return "", false
	}
	display := aliasTarget
	if pipe := strings.Index(inner, "|"); pipe >= 0 {
		if candidate := strings.TrimSpace(inner[pipe+1:]); candidate != "" {
			display = candidate
		}
	}
	target := i.preferredWikilinkTarget(matches[0])
	if fragment != "" {
		target += fragment
	}
	recheck, ok := i.resolveWiki(source, target)
	if !ok || recheck.notePath != matches[0] {
		return "", false
	}
	return "[[" + target + "|" + display + "]]", true
}

func wikilinkInner(raw string) (string, bool) {
	open := strings.Index(raw, "[[")
	close := strings.LastIndex(raw, "]]")
	if open < 0 || close < open {
		return "", false
	}
	return raw[open+2 : close], true
}

func (i linkHygieneIndex) notesWithAlias(alias string) []string {
	want := normalizeLinkHygieneKey(alias)
	var matches []string
	for _, notePath := range i.notes {
		for _, candidate := range i.aliasesByNote[notePath] {
			if normalizeLinkHygieneKey(candidate) == want {
				matches = append(matches, notePath)
				break
			}
		}
	}
	return matches
}

func (i linkHygieneIndex) preferredWikilinkTarget(notePath string) string {
	base := obsidian.RemoveMdSuffix(filepath.ToSlash(notePath))
	name := path.Base(base)
	if candidates := i.byKey[normalizeLinkHygieneKey(name)]; len(candidates) == 1 && candidates[0] == notePath {
		return name
	}
	return base
}

func (i linkHygieneIndex) markdownReplacement(sourceNote string, link scannedMdLink) (string, string, string, bool) {
	if isFilesystemAbsoluteLink(link.target) {
		return "", "markdown_internal_link_absolute_path", "Markdown note link is absolute instead of source-relative", false
	}
	targetPath, fragment := splitLinkHygieneFragment(link.target)
	if targetPath == "" {
		return "", "", "", false
	}
	if markdownEscapesVault(sourceNote, targetPath) {
		return "", "markdown_internal_link_outside_vault", "Markdown note link escapes the vault", false
	}
	resolved, ok := i.resolveMarkdown(sourceNote, link.target)
	if !ok {
		if strings.HasPrefix(link.target, "/") {
			return "", "markdown_internal_link_absolute_path", "Markdown note link is absolute instead of source-relative", false
		}
		return "", "", "", false
	}
	want := relativeMarkdownTarget(sourceNote, resolved.notePath)
	if resolved.fragment != "" {
		want += "#" + resolved.fragment
	} else if fragment != "" {
		want += fragment
	}
	if filepath.ToSlash(link.target) == want {
		return "", "", "", false
	}
	recheck, ok := i.resolveMarkdown(sourceNote, want)
	if !ok || recheck != resolved {
		return "", "markdown_internal_link_not_relative", "relative Markdown replacement cannot be proven equivalent", false
	}
	return "[" + link.text + "](" + want + ")", "markdown_internal_link_not_relative", "Markdown note link should be source-relative", true
}

type resolvedHygieneTarget struct {
	notePath string
	fragment string
}

// resolveWiki reports the real file a wikilink opens, ignoring aliases.
func (i linkHygieneIndex) resolveWiki(source, target string) (resolvedHygieneTarget, bool) {
	base, fragment := splitLinkHygieneFragment(target)
	notePath, ok := i.files().Resolve(base, source)
	if !ok {
		return resolvedHygieneTarget{}, false
	}
	return resolvedHygieneTarget{notePath: notePath, fragment: strings.TrimPrefix(fragment, "#")}, true
}

func (i linkHygieneIndex) resolveMarkdown(sourceNote, target string) (resolvedHygieneTarget, bool) {
	base, fragment := splitLinkHygieneFragment(filepath.ToSlash(target))
	if base == "" {
		return resolvedHygieneTarget{notePath: sourceNote, fragment: strings.TrimPrefix(fragment, "#")}, true
	}
	joined := base
	if strings.HasPrefix(base, "/") {
		joined = strings.TrimPrefix(base, "/")
	} else {
		dir := path.Dir(filepath.ToSlash(sourceNote))
		if dir != "." {
			joined = path.Clean(path.Join(dir, base))
		}
	}
	joined = obsidian.RemoveMdSuffix(joined)
	if notePath, ok := i.exactPath(joined); ok {
		return resolvedHygieneTarget{notePath: notePath, fragment: strings.TrimPrefix(fragment, "#")}, true
	}
	candidates := i.byKey[normalizeLinkHygieneKey(joined)]
	if len(candidates) != 1 {
		return resolvedHygieneTarget{}, false
	}
	return resolvedHygieneTarget{notePath: candidates[0], fragment: strings.TrimPrefix(fragment, "#")}, true
}

func (i linkHygieneIndex) exactPath(targetNoExt string) (string, bool) {
	normalized := normalizeLinkHygieneKey(targetNoExt)
	for _, notePath := range i.notes {
		noteNoExt := normalizeLinkHygieneKey(filepath.ToSlash(notePath))
		if noteNoExt == normalized {
			return notePath, true
		}
	}
	return "", false
}

func linkHygieneIssue(notePath string, line int, code, linkType, target, replacement, reason string) Issue {
	return Issue{
		Code:    code,
		Path:    notePath,
		Source:  notePath,
		Target:  target,
		Line:    line,
		Message: reason,
		Data: mustMarshal(linkHygieneIssueData{
			Target:      target,
			Replacement: replacement,
			LinkType:    linkType,
			Reason:      reason,
		}),
	}
}

func buildLinkHygieneFixes(byPath map[string][]linkHygieneEdit) []FixAction {
	paths := make([]string, 0, len(byPath))
	for notePath := range byPath {
		paths = append(paths, notePath)
	}
	sort.Strings(paths)
	actions := make([]FixAction, 0, len(paths))
	for _, notePath := range paths {
		edits := byPath[notePath]
		sort.SliceStable(edits, func(a, b int) bool {
			return edits[a].start > edits[b].start
		})
		fixEdits := make([]FixEdit, 0, len(edits))
		issueKeys := make([]string, 0, len(edits))
		issueCodes := make([]string, 0, len(edits))
		for _, edit := range edits {
			fixEdits = append(fixEdits, FixEdit{
				Kind:      FixKindRewriteLinkTarget,
				NotePath:  notePath,
				StartByte: edit.start,
				EndByte:   edit.end,
				Value:     edit.value,
			})
			issueKeys = append(issueKeys, edit.issueKey)
			issueCodes = append(issueCodes, edit.issueCode)
		}
		issueCodes = sortedUnique(issueCodes)
		issueCode := ""
		if len(issueCodes) == 1 {
			issueCode = issueCodes[0]
		}
		actions = append(actions, FixAction{
			ID:            "link-hygiene:" + notePath,
			Check:         CheckLinkHygiene,
			IssueCode:     issueCode,
			Kind:          FixKindRewriteLinkTarget,
			Safety:        FixSafetySafe,
			Title:         "Normalize internal link targets",
			Summary:       fmt.Sprintf("rewrite %d internal link target(s) in %s", len(fixEdits), notePath),
			InstanceCount: len(fixEdits),
			IssueKeys:     sortedUnique(issueKeys),
			AffectedPaths: []string{notePath},
			Edits:         fixEdits,
		})
	}
	return actions
}

func scanMarkdownLinksForHygiene(content string) []scannedMdLink {
	var links []scannedMdLink
	inCode := obsidian.MarkdownCodeMask(content)
	for i := 0; i < len(content); {
		idx := strings.Index(content[i:], "[")
		if idx == -1 {
			break
		}
		idx += i
		if idx > 0 && content[idx-1] == '!' || idx+1 < len(content) && content[idx+1] == '[' {
			i = idx + 1
			continue
		}
		closeText := strings.Index(content[idx+1:], "]")
		if closeText == -1 {
			break
		}
		closeText += idx + 1
		if closeText+1 >= len(content) || content[closeText+1] != '(' {
			i = closeText + 1
			continue
		}
		closeTarget := strings.Index(content[closeText+2:], ")")
		if closeTarget == -1 {
			break
		}
		closeTarget += closeText + 2
		target := content[closeText+2 : closeTarget]
		links = append(links, scannedMdLink{
			target:          filepath.ToSlash(strings.TrimSpace(target)),
			text:            content[idx+1 : closeText],
			start:           idx,
			end:             closeTarget + 1,
			line:            1 + strings.Count(content[:idx], "\n"),
			insideCodeBlock: inCode(idx),
		})
		i = closeTarget + 1
	}
	return links
}

type pseudoLink struct {
	target string
	line   int
}

func scanPseudoLinks(content string) []pseudoLink {
	var out []pseudoLink
	inCode := obsidian.MarkdownCodeMask(content)
	for _, pattern := range pseudoLinkPatterns {
		for _, loc := range pattern.FindAllStringIndex(content, -1) {
			if inCode(loc[0]) {
				continue
			}
			out = append(out, pseudoLink{target: content[loc[0]:loc[1]], line: 1 + strings.Count(content[:loc[0]], "\n")})
		}
	}
	return out
}

// normalizeLinkHygieneKey strips only `.md`: a dot in a note name
// (`fetch.ai call`, `v1.2 plan`) is part of the name, not an extension.
func normalizeLinkHygieneKey(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	value = obsidian.RemoveMdSuffix(value)
	return strings.ToLower(value)
}

func splitLinkHygieneFragment(target string) (string, string) {
	if hash := strings.Index(target, "#"); hash >= 0 {
		return target[:hash], target[hash:]
	}
	return target, ""
}

func relativeMarkdownTarget(sourceNote, targetNote string) string {
	dir := path.Dir(filepath.ToSlash(sourceNote))
	if dir == "." {
		return filepath.ToSlash(targetNote)
	}
	rel, err := filepath.Rel(filepath.FromSlash(dir), filepath.FromSlash(targetNote))
	if err != nil {
		return filepath.ToSlash(targetNote)
	}
	return filepath.ToSlash(rel)
}

func markdownEscapesVault(sourceNote, target string) bool {
	if target == "" {
		return false
	}
	clean := filepath.ToSlash(target)
	if strings.HasPrefix(clean, "/") && isFilesystemAbsoluteLink(clean) {
		return true
	}
	clean = strings.TrimPrefix(clean, "/")
	dir := path.Dir(filepath.ToSlash(sourceNote))
	if dir == "." {
		dir = ""
	}
	joined := path.Clean(path.Join(dir, clean))
	return strings.HasPrefix(joined, "../") || joined == ".."
}

func isMarkdownNoteLink(target string) bool {
	base, _ := splitLinkHygieneFragment(target)
	return strings.EqualFold(path.Ext(base), ".md") || base == ""
}

func isFilesystemAbsoluteLink(target string) bool {
	target = strings.TrimSpace(filepath.ToSlash(target))
	if strings.HasPrefix(target, "~/") {
		return true
	}
	if len(target) >= 3 && target[1] == ':' && (target[2] == '/' || target[2] == '\\') {
		return true
	}
	return strings.HasPrefix(target, "/Users/") || strings.HasPrefix(target, "/home/") || strings.HasPrefix(target, "/var/") || strings.HasPrefix(target, "/tmp/")
}

func obsidianLinkIsExternal(target string) bool {
	lower := strings.ToLower(strings.TrimSpace(target))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "tel:") ||
		strings.HasPrefix(lower, "ftp://") ||
		strings.HasPrefix(lower, "file://")
}
