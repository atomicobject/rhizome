package validate

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func applyBlockFragmentRewrite(runCtx RunContext, sourceNote, content, oldTarget, newTarget string) (string, int, bool) {
	oldPath, oldFragment := splitFragment(oldTarget)
	newPath, newFragment := splitFragment(newTarget)
	if oldPath == "" || newPath == "" || oldFragment == "" || newFragment == "" {
		return content, 0, false
	}
	if oldPath != newPath || !strings.HasPrefix(oldFragment, "^") || !strings.HasPrefix(newFragment, "^") {
		return content, 0, false
	}
	allNotes, err := runCtx.NoteReader.GetNotesList(runCtx.VaultDef)
	if err != nil {
		return content, 0, false
	}
	contents := map[string]string{sourceNote: content}
	getContent := func(notePath string) string {
		if cached, ok := contents[notePath]; ok {
			return cached
		}
		body, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, notePath)
		if err != nil {
			contents[notePath] = ""
			return ""
		}
		contents[notePath] = body
		return body
	}
	cache := obsidian.BuildNotePathCacheWithAliases(allNotes, aliasMapFromNotes(allNotes, getContent))
	updated, count := rewriteResolvedBlockFragments(content, cache, sourceNote, oldPath, oldFragment, newFragment)
	return updated, count, true
}

// ApplyAliasAppendInMemory returns content with value appended to the aliases
// frontmatter list (sorted, deduped). Returns input unchanged when already present.
func ApplyAliasAppendInMemory(content, value string) (string, error) {
	fm, err := obsidian.ExtractFrontmatter(content)
	if err != nil {
		return "", err
	}
	aliases := obsidian.AliasListFromFrontmatter(fm)
	if containsExact(aliases, value) {
		return content, nil
	}
	aliases = append(aliases, value)
	sort.Strings(aliases)
	updated, changed, err := obsidian.SetFrontmatterProperty(content, "aliases", aliases, true)
	if err != nil || !changed {
		return content, err
	}
	return updated, nil
}

// ApplySetFrontmatterInMemory returns content with the given property set to
// the scalar value or values slice. Returns input unchanged when nothing changed.
func ApplySetFrontmatterInMemory(content, property, value string, values []string) (string, error) {
	var v any = value
	if values != nil {
		v = append([]string(nil), values...)
	}
	updated, changed, err := obsidian.SetFrontmatterProperty(content, property, v, true)
	if err != nil || !changed {
		return content, err
	}
	return updated, nil
}

// ApplyRewriteLinkInMemory returns content with oldTarget wikilinks rewritten
// to newTarget, plus the count of link instances rewritten.
func ApplyRewriteLinkInMemory(content, oldTarget, newTarget string) (string, int) {
	return obsidian.RewriteLinksInContentWithOptions(content, oldTarget, newTarget, true)
}

func ApplyLinkTargetRewriteInMemory(content string, edit FixEdit) (string, error) {
	if edit.StartByte < 0 || edit.EndByte < edit.StartByte || edit.EndByte > len(content) {
		return content, fmt.Errorf("invalid link rewrite range %d..%d", edit.StartByte, edit.EndByte)
	}
	return content[:edit.StartByte] + edit.Value + content[edit.EndByte:], nil
}

// ApplyFixEditInMemory applies a FixEdit to in-memory content and returns the
// result. Use this when staging validate fixes into an edit-session-style
// pipeline that writes content at commit time.
func ApplyFixEditInMemory(content string, edit FixEdit) (string, error) {
	switch edit.Kind {
	case FixKindAppendAlias:
		return ApplyAliasAppendInMemory(content, edit.Value)
	case FixKindSetFrontmatter:
		return ApplySetFrontmatterInMemory(content, edit.Property, edit.Value, edit.Values)
	case FixKindRewriteLinkGroup:
		if edit.PreserveDisplay {
			// A path-qualified target must not also match bare same-named links.
			updated, _ := obsidian.RewriteLinksPreservingDisplay(content, edit.OldTarget, edit.NewTarget, !strings.Contains(edit.OldTarget, "/"))
			return updated, nil
		}
		updated, _ := ApplyRewriteLinkInMemory(content, edit.OldTarget, edit.NewTarget)
		return updated, nil
	case FixKindRewriteLinkTarget:
		return ApplyLinkTargetRewriteInMemory(content, edit)
	case FixKindReplaceCodeAnchorTarget:
		updated, _, err := replaceCodeAnchorTargetInMemory(content, edit.Property, edit.OldTarget, edit.NewTarget)
		return updated, err
	case FixKindAddSectionScaffold:
		return AddSectionScaffold(content, edit.Property, edit.Value), nil
	default:
		return content, fmt.Errorf("unknown validation fix edit kind %q", edit.Kind)
	}
}

// AddSectionScaffold appends a markdown heading to the end of the content.
func AddSectionScaffold(content, level, heading string) string {
	level = strings.TrimSpace(level)
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return content
	}
	hashes := markdownHashesForLevel(level)
	if hashes == "" {
		return content
	}
	trimmedRight := strings.TrimRight(content, "\n")
	var b strings.Builder
	b.WriteString(trimmedRight)
	if trimmedRight != "" {
		b.WriteString("\n\n")
	}
	b.WriteString(hashes)
	b.WriteByte(' ')
	b.WriteString(heading)
	b.WriteString("\n")
	return b.String()
}

func markdownHashesForLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "H1":
		return "#"
	case "H2":
		return "##"
	case "H3":
		return "###"
	case "H4":
		return "####"
	case "H5":
		return "#####"
	case "H6":
		return "######"
	default:
		return ""
	}
}

func readNoteForFix(runCtx RunContext, notePath string) (string, string, error) {
	trimmed := strings.TrimSpace(notePath)
	content, err := runCtx.NoteReader.GetContents(runCtx.VaultDef, trimmed)
	if err != nil {
		return "", "", err
	}
	return content, filepath.Join(runCtx.VaultPath, filepath.FromSlash(trimmed)), nil
}

func containsExact(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}
