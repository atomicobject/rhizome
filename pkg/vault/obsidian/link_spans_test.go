package obsidian

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanStructuredLinksReportsExactComponentSpans(t *testing.T) {
	content := "💡 See [[Folder/Spec-0005#^SPEC-0005-US1|SPEC-0005]] and [SPEC-0005](../spec.md#^SPEC-0005-US1)."

	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)

	assertStructuredLink(t, content, links[0], StructuredLink{
		Kind:     StructuredLinkWikilink,
		Target:   "Folder/Spec-0005#^SPEC-0005-US1",
		Path:     "Folder/Spec-0005",
		Fragment: "^SPEC-0005-US1",
		Display:  "SPEC-0005",
	})
	assertStructuredLink(t, content, links[1], StructuredLink{
		Kind:     StructuredLinkMarkdown,
		Target:   "../spec.md#^SPEC-0005-US1",
		Path:     "../spec.md",
		Fragment: "^SPEC-0005-US1",
		Display:  "SPEC-0005",
	})

	require.Equal(t, "[[Folder/Spec-0005#^SPEC-0005-US1|SPEC-0005]]", links[0].RawSpan.Text(content))
	require.Equal(t, "[SPEC-0005](../spec.md#^SPEC-0005-US1)", links[1].RawSpan.Text(content))
}

func TestScanStructuredLinksEmbedsAndSameNoteFragments(t *testing.T) {
	content := "![[Asset#section]] then ![alt](img.png) and [[#^block]]"

	links := ScanStructuredLinks(content)
	require.Len(t, links, 3)

	require.True(t, links[0].Embed)
	require.Equal(t, "![[Asset#section]]", links[0].RawSpan.Text(content))
	require.Equal(t, "Asset", links[0].PathSpan.Text(content))
	require.Equal(t, "section", links[0].FragmentSpan.Text(content))
	require.False(t, links[0].DisplaySpan.Valid())

	require.True(t, links[1].Embed)
	require.Equal(t, "![alt](img.png)", links[1].RawSpan.Text(content))
	require.Equal(t, "alt", links[1].DisplaySpan.Text(content))
	require.False(t, links[1].FragmentSpan.Valid())

	require.Equal(t, "", links[2].Path)
	require.True(t, links[2].PathSpan.Valid())
	require.Equal(t, "", links[2].PathSpan.Text(content))
	require.Equal(t, "^block", links[2].FragmentSpan.Text(content))
}

func TestScanStructuredLinksSkipsCodeAndExternalTargets(t *testing.T) {
	content := strings.Join([]string{
		"[[kept-wiki]] [kept-md](kept.md)",
		"`[[inline-wiki]] [inline-md](inline.md)`",
		"```markdown",
		"[[fenced-wiki]] [fenced-md](fenced.md)",
		"```",
		"~~~",
		"[[tilde-wiki]] [tilde-md](tilde.md)",
		"~~~",
		"[web](https://example.com) [mail](mailto:test@example.com)",
	}, "\n")

	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)
	require.Equal(t, "kept-wiki", links[0].Target)
	require.Equal(t, "kept.md", links[1].Target)
}

func TestStructuredLinkComponentSpansSupportPreciseRewrite(t *testing.T) {
	content := "Prose SPEC-0005; [[old.md#old-fragment|SPEC-0005]]; [SPEC-0005](old.md#old-fragment)."
	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)

	type replacement struct {
		span StructuredLinkSpan
		text string
	}
	var replacements []replacement
	for _, link := range links {
		replacements = append(replacements,
			replacement{span: link.PathSpan, text: "new.md"},
			replacement{span: link.FragmentSpan, text: "new-fragment"},
			replacement{span: link.DisplaySpan, text: "SPEC-0006"},
		)
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].span.Start > replacements[j].span.Start })
	for _, replacement := range replacements {
		content = content[:replacement.span.Start] + replacement.text + content[replacement.span.End:]
	}

	require.Equal(t, "Prose SPEC-0005; [[new.md#new-fragment|SPEC-0006]]; [SPEC-0006](new.md#new-fragment).", content)
}

func TestScanStructuredLinksSkipsEscapedOpenersAndEmbeds(t *testing.T) {
	content := `\[[literal wiki]] \![[literal embed]] \[literal md](no.md) \![literal image](no.png) \\[[real]]`

	links := ScanStructuredLinks(content)
	require.Len(t, links, 1)
	require.Equal(t, "real", links[0].Target)
	require.Equal(t, `[[real]]`, links[0].RawSpan.Text(content))
}

func TestScanWikilinksSkipsEscapedOpenersAndEmbeds(t *testing.T) {
	content := `\[[literal]] \![[literal embed]] \\[[real]]`

	links := ScanWikilinks(content, DefaultWikilinkOptions)
	require.Len(t, links, 1)
	require.Equal(t, "real", links[0].Target)
}

func TestScanWikilinksIgnoresEscapedClosingDelimiter(t *testing.T) {
	content := `[[target\]]suffix]]`

	links := ScanWikilinks(content, DefaultWikilinkOptions)
	require.Len(t, links, 1)
	require.Equal(t, `target\]]suffix`, links[0].Target)
	require.Equal(t, content, links[0].Raw)
}

func TestScanStructuredLinksParsesNestedAndEscapedMarkdownDelimiters(t *testing.T) {
	content := `[outer [inner] and escaped \]](folder/SPEC-0005_(draft).md#frag)`

	links := ScanStructuredLinks(content)
	require.Len(t, links, 1)
	require.Equal(t, `outer [inner] and escaped \]`, links[0].Display)
	require.Equal(t, "folder/SPEC-0005_(draft).md", links[0].Path)
	require.Equal(t, "frag", links[0].Fragment)
	require.Equal(t, content, links[0].RawSpan.Text(content))
}

func TestScanStructuredLinksIsolatesAngleDestinationAndTitle(t *testing.T) {
	content := `[SPEC-0005](<Folder/My Spec.md#details> "Readable title")`

	links := ScanStructuredLinks(content)
	require.Len(t, links, 1)
	require.Equal(t, "Folder/My Spec.md#details", links[0].Target)
	require.Equal(t, "Folder/My Spec.md", links[0].Path)
	require.Equal(t, "details", links[0].Fragment)
	require.Equal(t, "Folder/My Spec.md#details", links[0].TargetSpan.Text(content))
	require.Equal(t, content, links[0].RawSpan.Text(content))

	// Keep the long-standing extraction API's authored parenthesized body while
	// the rewrite-oriented scanner isolates the path component.
	require.Equal(t, []string{`<Folder/My Spec.md#details> "Readable title"`}, ExtractMdLinks(content, DefaultMdLinkOptions))
}

func TestStructuredLinkResolverInputMatchesLegacyScanAllLinks(t *testing.T) {
	content := `[[Target|Label]] and [Spec](<Folder/My Spec.md#details> "Readable title")`
	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)
	require.Equal(t, "Target", links[0].ResolverInput())
	require.Equal(t, `<Folder/My Spec.md#details> "Readable title"`, links[1].ResolverInput())
}

func TestScanStructuredLinksRequiresLegalFenceCloser(t *testing.T) {
	content := strings.Join([]string{
		"```markdown",
		"[[code-one]]",
		"```go",
		"[code-two](code.md)",
		"```",
		"[[live]] [live](live.md)",
	}, "\n")

	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)
	require.Equal(t, "live", links[0].Target)
	require.Equal(t, "live.md", links[1].Target)
}

func TestScanStructuredLinksDoesNotRescanInsideWikilink(t *testing.T) {
	content := `[[note|[label](other.md)]] and [real](real.md)`

	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)
	require.Equal(t, StructuredLinkWikilink, links[0].Kind)
	require.Equal(t, "note", links[0].Target)
	require.Equal(t, StructuredLinkMarkdown, links[1].Kind)
	require.Equal(t, "real.md", links[1].Target)
}

func TestScanStructuredLinksMalformedMarkdownFailsClosed(t *testing.T) {
	tests := []string{
		`[nested [label](x.md)`,
		`[label](folder/(broken.md)`,
		`[label](old.md "unterminated)`,
		`[label](<old.md)`,
	}
	for _, content := range tests {
		t.Run(content, func(t *testing.T) {
			require.Empty(t, ScanStructuredLinks(content))
		})
	}
}

func TestScanStructuredLinksMalformedLabelIsBoundedToItsLine(t *testing.T) {
	content := "[broken [nested](no.md)\n[real](yes.md)"

	links := ScanStructuredLinks(content)
	require.Len(t, links, 1)
	require.Equal(t, "yes.md", links[0].Target)
}

func TestScanStructuredLinksSkipsIndentedCodeBlocks(t *testing.T) {
	content := strings.Join([]string{
		"    [[four-space-wiki]] [four-space-md](four.md)",
		"\t[[tab-wiki]] [tab-md](tab.md)",
		"[[live-wiki]] [live-md](live.md)",
	}, "\n")

	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)
	require.Equal(t, "live-wiki", links[0].Target)
	require.Equal(t, "live.md", links[1].Target)
}

func TestScanStructuredLinksMalformedProtectedWikiCannotSwallowLiveLink(t *testing.T) {
	content := "`[[malformed` then [[live]] and [live](live.md)"

	links := ScanStructuredLinks(content)
	require.Len(t, links, 2)
	require.Equal(t, "live", links[0].Target)
	require.Equal(t, "live.md", links[1].Target)
}

func TestScanStructuredLinksSkipsEveryURISchemeAndProtocolRelativeTarget(t *testing.T) {
	content := strings.Join([]string{
		"[https](https://example.com)", "[http](HTTP://EXAMPLE.COM)",
		"[mail](mailto:test@example.com)", "[tel](tel:+1234567890)",
		"[ftp](ftp://files.example.com)", "[file](file:///path/to/file)",
		"[data](data:text/plain,hi)", "[javascript](javascript:alert(1))",
		"[custom](vscode://file/test.md)", "[network](//example.com/test.md)",
		"[local](docs/local.md)", "[relative](./relative.md)",
		"[parent](../parent.md)", "[absolute](/absolute.md)",
		"[note](note.md)",
	}, " ")
	links := ScanStructuredLinks(content)
	got := make([]string, len(links))
	for i, link := range links {
		got[i] = link.Target
	}
	require.Equal(t, []string{"docs/local.md", "./relative.md", "../parent.md", "/absolute.md", "note.md"}, got)
}

func TestStructuredLinkSourceValidationRejectsForgedAndSameLengthStaleContent(t *testing.T) {
	content := "[[SPEC-0075]]"
	link := ScanStructuredLinks(content)[0]

	require.True(t, ValidateStructuredLinkSource(content, link))
	require.False(t, ValidateStructuredLinkSource("[[SPEC-9999]]", link))
	forged := link
	forged.Path = "SPEC-9999"
	require.False(t, ValidateStructuredLinkSource(content, forged))
}

func TestStructuredLinkScanValidationRejectsDroppedDuplicatedAndReorderedLinks(t *testing.T) {
	content := "[[one]] and [two](two.md)"
	links := ScanStructuredLinks(content)

	require.True(t, ValidateStructuredLinkScan(content, links))
	require.False(t, ValidateStructuredLinkScan(content, links[:1]))
	require.False(t, ValidateStructuredLinkScan(content, []StructuredLink{links[0], links[0]}))
	require.False(t, ValidateStructuredLinkScan(content, []StructuredLink{links[1], links[0]}))
}

func TestStructuredLinkSnapshotSealsEmptyScanAndStableSourceIdentity(t *testing.T) {
	content := "plain prose with no links"
	first := ScanStructuredLinkSnapshot(content)
	second := ScanStructuredLinkSnapshot(content)

	require.Empty(t, first.Links)
	require.NotEmpty(t, first.SourceFingerprint)
	require.Equal(t, first.SourceFingerprint, second.SourceFingerprint)
	validated, err := first.ValidatedSnapshot()
	require.NoError(t, err)
	require.Equal(t, first.SourceFingerprint, validated.SourceFingerprint)
	require.False(t, ValidateStructuredLinkScan(content, nil), "a bare empty slice cannot attest a completed scan")

	first.SourceFingerprint = "forged"
	_, err = first.ValidatedSnapshot()
	require.Error(t, err)

	nonEmpty := ScanStructuredLinkSnapshot("[[target]]")
	nonEmpty.Links[0].Target = "forged"
	_, err = nonEmpty.ValidatedSnapshot()
	require.Error(t, err)
}

func TestContainingSpanLookupHasLogarithmicBound(t *testing.T) {
	spans := make([]StructuredLinkSpan, 1<<14)
	for index := range spans {
		spans[index] = linkSpan(index*4, index*4+2)
	}

	span, found, steps := containingSpanLookup(spans, spans[len(spans)-1].Start)
	require.True(t, found)
	require.Equal(t, spans[len(spans)-1], span)
	require.LessOrEqual(t, steps, 15)
}

func TestScanStructuredLinksHandlesManyProtectedAndLiveSpans(t *testing.T) {
	var content strings.Builder
	const count = 2_000
	for index := 0; index < count; index++ {
		fmt.Fprintf(&content, "`[hidden-%d](hidden.md)` [live-%d](live-%d.md)\n", index, index, index)
	}

	snapshot := ScanStructuredLinkSnapshot(content.String())
	require.Len(t, snapshot.Links, count)
	require.NoError(t, snapshot.Validate())
}

func assertStructuredLink(t *testing.T, content string, got, want StructuredLink) {
	t.Helper()
	require.Equal(t, want.Kind, got.Kind)
	require.Equal(t, want.Target, got.Target)
	require.Equal(t, want.Path, got.Path)
	require.Equal(t, want.Fragment, got.Fragment)
	require.Equal(t, want.Display, got.Display)
	require.Equal(t, got.Target, got.TargetSpan.Text(content))
	require.Equal(t, got.Path, got.PathSpan.Text(content))
	require.Equal(t, got.Fragment, got.FragmentSpan.Text(content))
	require.Equal(t, got.Display, got.DisplaySpan.Text(content))
}
