package validate

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func applyBlockFragmentRewrite(runCtx RunContext, sourceNote, content, oldTarget, newTarget string) (string, int, bool, error) {
	oldPath, oldFragment := splitFragment(oldTarget)
	newPath, newFragment := splitFragment(newTarget)
	if oldPath == "" || oldPath != newPath || !strings.HasPrefix(oldFragment, "^") || !strings.HasPrefix(newFragment, "^") {
		return content, 0, false, nil
	}
	inventory, err := validationLinksFor(context.Background(), runCtx)
	if err != nil {
		return content, 0, true, err
	}
	source, err := runCtx.NoteMetadata.ProjectNoteSource(sourceNote, content, 0)
	if err != nil {
		return content, 0, true, err
	}
	var spans []noteformat.SourceRange
	for _, fact := range source.Projection.Facts.Links {
		if fact.Fragment != oldFragment || !validationBlockFragmentAuthored(source, fact) || validationLinkExternal(source, fact) || !validationLinkSyntaxEnabled(runCtx.VaultDef, fact) {
			continue
		}
		if target, exists := inventory.resolve(source, fact); exists && target == oldPath {
			spans = append(spans, fact.FragmentRange.Range)
		}
	}
	for index := len(spans) - 1; index >= 0; index-- {
		span := spans[index]
		content = content[:span.StartByte] + newFragment + content[span.EndByte:]
	}
	return content, len(spans), true, nil
}

// ApplyAliasAppendInMemory returns content with value appended to the aliases
// frontmatter list (sorted, deduped). Returns input unchanged when already present.
func ApplyAliasAppendInMemory(content, value string) (string, error) {
	fm, err := markdownsyntax.ExtractFrontmatter(content)
	if err != nil {
		return "", err
	}
	aliases := obsidian.AliasListFromFrontmatter(fm)
	if containsExact(aliases, value) {
		return content, nil
	}
	aliases = append(aliases, value)
	sort.Strings(aliases)
	return ontology.PatchRootFrontmatter(content, "aliases", "", aliases)
}

// ApplySetFrontmatterInMemory returns content with the given property set to
// the scalar value or values slice. Returns input unchanged when nothing changed.
func ApplySetFrontmatterInMemory(content, property, value string, values []string) (string, error) {
	return ontology.PatchRootFrontmatter(content, property, value, values)
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
