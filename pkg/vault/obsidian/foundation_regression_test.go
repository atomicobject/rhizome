package obsidian

import (
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestHashtagsExcludeProtectedSourceAndNumericNames(t *testing.T) {
	source := "---\nstatus: draft #yaml\n---\n#café #123 #_ok\n~~~\n#fenced\n~~~\n\n    #indented\n\n`#inline` #visible\n"
	require.Equal(t, []string{"#café", "#_ok", "#visible"}, markdownsyntax.ExtractHashtags(source))
}

func TestLinkNameFoldingRetainsCollisionAmbiguity(t *testing.T) {
	cache := BuildNotePathCacheWithAliases([]string{"Notes/Cafe\u0301.md", "My Note.md"}, map[string][]string{"My Note.md": {"Alpha"}})
	for _, link := range []string{"notes/CAFÉ.md", "café"} {
		got, ok := cache.ResolveNote(link)
		require.True(t, ok, link)
		require.Equal(t, "Notes/Cafe\u0301.md", got)
	}
	got, ok := cache.ResolveNote("alpha")
	require.True(t, ok)
	require.Equal(t, "My Note.md", got)
	cache.AddOrUpdate("other.md", []string{"ALPHA"})
	_, ok = cache.ResolveNote("alpha")
	require.False(t, ok)
	cache.Remove("other.md")
	got, ok = cache.ResolveNote("ALPHA")
	require.True(t, ok)
	require.Equal(t, "My Note.md", got)
	cache.AddOrUpdate("notes/CAFÉ.md", nil)
	_, ok = cache.ResolveNote("café")
	require.False(t, ok)
	cache.Remove("notes/CAFÉ.md")
	got, ok = cache.ResolveNote("café")
	require.True(t, ok)
	require.Equal(t, "Notes/Cafe\u0301.md", got)
}

func TestLegacyLinkViewsExcludeAllProtectedSource(t *testing.T) {
	source := "---\nexample: '[[YAML]]'\n---\n[[Live]]\n~~~\n[[Fence]]\n~~~\n\n    [[Indented]]\n\n`[[Inline]]`"
	links := markdownsyntax.ScanWikilinks(source, markdownsyntax.DefaultWikilinkOptions)
	require.Len(t, links, 5)
	for index, expected := range []struct {
		target    string
		protected bool
	}{{"YAML", false}, {"Live", false}, {"Fence", true}, {"Indented", true}, {"Inline", true}} {
		require.Equal(t, expected.target, links[index].Target)
		require.Equal(t, expected.protected, links[index].InsideCodeBlock)
	}
	require.Equal(t, []string{"YAML", "Live"}, markdownsyntax.ExtractWikilinks(source, markdownsyntax.DefaultWikilinkOptions))
	var active []string
	for _, link := range markdownsyntax.ScanStructuredLinkSnapshot(source).Links {
		active = append(active, link.Target)
	}
	require.Equal(t, []string{"YAML", "Live"}, active)
	all := markdownsyntax.ScanAllLinks(source, markdownsyntax.DefaultWikilinkOptions, markdownsyntax.DefaultMdLinkOptions)
	require.Equal(t, []markdownsyntax.LinkDetail{{Target: "YAML", LinkType: "wikilink", Subtype: "basic"}, {Target: "Live", LinkType: "wikilink", Subtype: "basic"}}, all)
}

func FuzzScannersRespectProtectedSource(f *testing.F) {
	f.Add("[[Live]]\n~~~\n[[Code]]\n~~~\n#tag")
	f.Fuzz(func(t *testing.T, source string) {
		fenceLength := 3
		for _, run := range strings.FieldsFunc(source, func(r rune) bool { return r != '~' }) {
			if len(run) >= fenceLength {
				fenceLength = len(run) + 1
			}
		}
		fence := strings.Repeat("~", fenceLength)
		prefix := "[[Before]]\n\n" + fence + "\n"
		content := prefix + source + "\n[[Protected]]\n" + fence + "\n\n[[After]]\n"
		protectedStart := len(prefix) + len(source) + 1
		afterStart := len(content) - len("[[After]]\n")
		seenBefore, seenProtected, seenAfter := false, false, false
		for _, link := range markdownsyntax.ScanWikilinks(content, markdownsyntax.DefaultWikilinkOptions) {
			switch link.Start {
			case 0:
				require.Equal(t, "Before", link.Target)
				require.False(t, link.InsideCodeBlock)
				seenBefore = true
			case afterStart:
				require.Equal(t, "After", link.Target)
				require.False(t, link.InsideCodeBlock)
				seenAfter = true
			default:
				require.True(t, link.InsideCodeBlock)
				if link.Start == protectedStart {
					require.Equal(t, "Protected", link.Target)
					seenProtected = true
				}
			}
		}
		require.True(t, seenBefore && seenProtected && seenAfter)
		var active []string
		for _, link := range markdownsyntax.ScanStructuredLinkSnapshot(content).Links {
			active = append(active, link.Target)
		}
		require.Equal(t, []string{"Before", "After"}, active)
		require.Equal(t, []string{"Before", "After"}, markdownsyntax.ExtractWikilinks(content, markdownsyntax.DefaultWikilinkOptions))
		require.Equal(t, []markdownsyntax.LinkDetail{{Target: "Before", LinkType: "wikilink", Subtype: "basic"}, {Target: "After", LinkType: "wikilink", Subtype: "basic"}}, markdownsyntax.ScanAllLinks(content, markdownsyntax.DefaultWikilinkOptions, markdownsyntax.DefaultMdLinkOptions))
	})
}

func TestLinkNamePrefersUniqueExactSpellingWithinFoldedCandidates(t *testing.T) {
	cache := BuildNotePathCacheWithAliases([]string{"a/Foo.md", "b/foo.md"}, map[string][]string{"a/Foo.md": {"Alpha"}, "b/foo.md": {"alpha"}})
	for input, want := range map[string]string{"Foo": "a/Foo.md", "foo": "b/foo.md", "Alpha": "a/Foo.md", "alpha": "b/foo.md", "a/FOO.md": "a/Foo.md"} {
		got, ok := cache.ResolveNote(input)
		require.True(t, ok, input)
		require.Equal(t, want, got, input)
	}
	_, ok := cache.ResolveNote("FOO")
	require.False(t, ok)
	_, ok = cache.ResolveNote("ALPHA")
	require.False(t, ok)
	cache.Remove("b/foo.md")
	got, ok := cache.ResolveNote("FOO")
	require.True(t, ok)
	require.Equal(t, "a/Foo.md", got)
}

func TestNamespaceRewritePreservesFrontmatterPropertyLinks(t *testing.T) {
	source := "---\nrelated: '[[Old]]'\n---\n[[Old]]\n~~~\n[[Old]]\n~~~\n"
	changed, count := RewriteLinksPreservingDisplay(source, "Old.md", "New.md", true)
	require.Equal(t, 2, count)
	require.Equal(t, "---\nrelated: '[[New|Old]]'\n---\n[[New|Old]]\n~~~\n[[Old]]\n~~~\n", changed)
}

func FuzzRewriteLeavesNonLinkBytesUnchanged(f *testing.F) {
	f.Add("---\nrelated: '[[Old]]'\n---\n[[Old]] and `[[Old]]`\n~~~\n[[Old]]\n~~~")
	f.Add("[label](Old.md#fragment) and plain Old.md")
	f.Fuzz(func(t *testing.T, source string) {
		rewritten, _ := RewriteLinksPreservingDisplay(source, "Old.md", "New.md", true)
		outside := func(content string) string {
			var result strings.Builder
			cursor := 0
			for _, link := range markdownsyntax.ScanStructuredLinkSnapshot(content).Links {
				result.WriteString(content[cursor:link.RawSpan.Start])
				cursor = link.RawSpan.End
			}
			result.WriteString(content[cursor:])
			return result.String()
		}
		if outside(source) != outside(rewritten) {
			t.Fatal("rewrite changed bytes outside scanned links")
		}
	})
}
