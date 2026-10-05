package html_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestProviderDeclaresStaticProjectionCapabilities(t *testing.T) {
	descriptor := html.New().Descriptor()
	if descriptor.ID != "html" {
		t.Fatalf("ID = %q, want html", descriptor.ID)
	}
	if !descriptor.Capabilities.Has(noteformat.CapabilitySourceReading) {
		t.Fatal("HTML provider must declare source reading")
	}
	for _, capability := range []noteformat.Capability{
		noteformat.CapabilitySearchableContentProjection,
		noteformat.CapabilityRootMetadataReading,
		noteformat.CapabilityRootMetadataMutation,
		noteformat.CapabilityActiveContentViewing,
		noteformat.CapabilityAuthoredLinkExtraction,
		noteformat.CapabilityFragmentTargetExtraction,
	} {
		if !descriptor.Capabilities.Has(capability) {
			t.Fatalf("HTML provider must declare capability %v", capability)
		}
	}
	for _, capability := range []noteformat.Capability{
		noteformat.CapabilityLinkRetargeting,
		noteformat.CapabilityFileRenameMove,
		noteformat.CapabilityStructuralContentMutation,
	} {
		if descriptor.Capabilities.Has(capability) {
			t.Fatalf("HTML provider unexpectedly declares mutation capability %v", capability)
		}
	}
}

func TestProjectHTMLFactsPreserveSourceRangesAndExcludeNonContent(t *testing.T) {
	content := "\ufeff<!doctype html>\r\n<head><title>Doc &amp; title</title><script id=\"rhizome-metadata\" type=\"application/json\">\r\n {\"type\":\"ReferenceDoc\",\"title\":\"Canonical\",\"n\":9007199254740993}\r\n</script></head><body><h1 id=\"top\">Repeat 😀</h1><p>Repeat &amp; prose</p><table><tr><td>A</td><td>B</td></tr></table><pre> a\n  b </pre><img alt=\"diagram\"><div hidden>hidden</div><div aria-hidden=\"true\">aria-hidden</div><template><p>template</p></template><!-- comment --><script>const sentinel = 'script';</script><script src=\"external.js\">external</script><a href=\"docs/%E2%9C%93.html?x=1#top\">link</a><a href=\"docs/one.html\" href=\"docs/two.html\">duplicate</a><a name=\"legacy\"></a></body>"
	projection := project(t, "notes/Doc.HTML", []byte(content))
	if projection.Status != noteformat.ProjectionStatusCurrent {
		t.Fatalf("status = %q", projection.Status)
	}
	if projection.Facts.Title == nil || projection.Facts.Title.Value != "Canonical" {
		t.Fatalf("title = %#v", projection.Facts.Title)
	}
	if len(projection.Facts.RootMetadata) != 3 {
		t.Fatalf("metadata = %#v", projection.Facts.RootMetadata)
	}
	if got := projection.Facts.RootMetadata[2].Value.Export(); got != json.Number("9007199254740993") {
		t.Fatalf("number fidelity = %#v", got)
	}
	texts := make([]string, 0, len(projection.Facts.SearchRegions))
	for _, region := range projection.Facts.SearchRegions {
		texts = append(texts, region.Text)
		if !region.Range.Present || region.Range.Range.StartByte < 0 || region.Range.Range.EndByte > len(content) {
			t.Fatalf("invalid region range: %#v", region)
		}
	}
	joined := strings.Join(texts, " ")
	for _, excluded := range []string{"hidden", "aria-hidden", "template", "comment", "external.js"} {
		if strings.Contains(joined, excluded) {
			t.Fatalf("excluded text %q appeared in %q", excluded, joined)
		}
	}
	if !strings.Contains(joined, "Repeat 😀") || !strings.Contains(joined, "diagram") || !strings.Contains(joined, "const sentinel") {
		t.Fatalf("visible/supplemental text = %q", joined)
	}
	if len(projection.Facts.Links) != 2 || projection.Facts.Links[0].Path != "docs/✓.html" {
		t.Fatalf("links = %#v", projection.Facts.Links)
	}
	if projection.Facts.Links[0].URI == nil || projection.Facts.Links[1].URI == nil {
		t.Fatal("every HTML href must carry URI facts")
	}
	if projection.Facts.DocumentBase != nil {
		t.Fatalf("unexpected base = %#v", projection.Facts.DocumentBase)
	}
	if len(projection.Facts.FragmentTargets) != 3 || projection.Facts.FragmentTargets[0].Kind != noteformat.FragmentTargetElementID || projection.Facts.FragmentTargets[2].Kind != noteformat.FragmentTargetLegacyName {
		t.Fatalf("fragment targets = %#v", projection.Facts.FragmentTargets)
	}
}

func TestProjectHTMLAuthoredURIsUseURLComponentsAndExactRanges(t *testing.T) {
	content := `<base href="/docs/"><a href="target.html?view=wide#section">target</a><a href="#only">same</a><a href="/docs/%E2%9C%93.html?q=one%20two#frag%20ment">root</a><a href="https://example.test/docs/remote.html?q=x#remote">external</a>`
	projection := project(t, "notes/page.html", []byte(content))
	require.Len(t, projection.Facts.Links, 4)
	for _, link := range projection.Facts.Links {
		require.Equal(t, noteformat.LinkResolutionURI, link.Resolution)
		require.NotNil(t, link.URI)
		require.Equal(t, "html", string(link.Syntax))
		require.Equal(t, "anchor", string(link.Subtype))
	}

	first := projection.Facts.Links[0]
	require.Equal(t, "target.html", first.URI.Path)
	require.Equal(t, "view=wide", first.URI.Query)
	require.Equal(t, "section", first.URI.Fragment)
	require.Equal(t, "target.html?view=wide#section", first.URI.Raw)
	require.Equal(t, first.Target, first.URI.Raw)
	require.Equal(t, exactRangeAt(content, `target.html?view=wide#section`, "target.html"), first.URI.PathRange)
	require.Equal(t, exactRangeAt(content, `target.html?view=wide#section`, "view=wide"), first.URI.QueryRange)
	require.Equal(t, exactRangeAt(content, `target.html?view=wide#section`, "section"), first.URI.FragmentRange)

	same := projection.Facts.Links[1]
	require.Empty(t, same.URI.Path)
	require.False(t, same.URI.PathRange.Present)
	require.Equal(t, "only", same.URI.Fragment)
	require.True(t, same.URI.FragmentRange.Present)

	root := projection.Facts.Links[2]
	require.Equal(t, "/docs/✓.html", root.URI.Path)
	require.Equal(t, "q=one two", root.URI.Query)
	require.Equal(t, "frag ment", root.URI.Fragment)
	require.Equal(t, "/docs/%E2%9C%93.html", sourceSlice(content, root.URI.PathRange.Range))

	external := projection.Facts.Links[3]
	require.Equal(t, "https", external.URI.Scheme)
	require.Equal(t, "example.test", external.URI.Authority)
	require.Equal(t, "/docs/remote.html", external.URI.Path)
}

func TestProjectHTMLURIComponentsDecodePercentEscapesOnce(t *testing.T) {
	projection := project(t, "page.html", []byte(`<a href="space%2520.html?value=%2520#frag%2520">link</a>`))
	require.Len(t, projection.Facts.Links, 1)
	uri := projection.Facts.Links[0].URI
	require.NotNil(t, uri)
	require.Equal(t, "space%20.html", uri.Path)
	require.Equal(t, "value=%20", uri.Query)
	require.Equal(t, "frag%20", uri.Fragment)
}

func exactRangeAt(source, occurrence, component string) noteformat.OptionalSourceRange {
	start := strings.Index(source, occurrence)
	if start < 0 {
		panic("occurrence not found")
	}
	componentStart := strings.Index(source[start:start+len(occurrence)], component)
	if componentStart < 0 {
		panic("component not found")
	}
	componentStart += start
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: componentStart, EndByte: componentStart + len(component)}}
}

func sourceSlice(source string, span noteformat.SourceRange) string {
	return source[span.StartByte:span.EndByte]
}

func TestProjectMetadataFailuresKeepOtherFacts(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate": `{"type":"A","type":"B"}`,
		"scalar":    `[]`,
		"malformed": `{"type":`,
	} {
		t.Run(name, func(t *testing.T) {
			content := `<head><script id="rhizome-metadata" type="application/json">` + body + `</script></head><body><h1>Body</h1><a href="next.html">next</a></body>`
			projection := project(t, "notes/page.html", []byte(content))
			if len(projection.Facts.RootMetadata) != 0 || len(projection.Facts.Links) != 1 || len(projection.Facts.SearchRegions) < 1 {
				t.Fatalf("facts = %#v", projection.Facts)
			}
			if len(projection.Diagnostics) == 0 || projection.Diagnostics[0].Category != noteformat.DiagnosticCategoryMetadata {
				t.Fatalf("diagnostics = %#v", projection.Diagnostics)
			}
		})
	}
}

func TestProjectTitleFallbackBaseAndNestedJSON(t *testing.T) {
	content := `<html><head><title>Document title</title><base href="/docs/"><script id="rhizome-metadata" type="application/json">{"nested":{"count":7}}</script></head><body><h1>Heading title</h1><a href="https://example.com/a%20b?q=x%20y#frag%20ment">external</a></body></html>`
	projection := project(t, "notes/page.html", []byte(content))
	if projection.Facts.Title == nil || projection.Facts.Title.Value != "Document title" {
		t.Fatalf("title fallback = %#v", projection.Facts.Title)
	}
	if projection.Facts.DocumentBase == nil || projection.Facts.DocumentBase.URI.Path != "/docs/" {
		t.Fatalf("document base = %#v", projection.Facts.DocumentBase)
	}
	if len(projection.Facts.Links) != 1 || projection.Facts.Links[0].Resolution != noteformat.LinkResolutionURI || projection.Facts.Links[0].URI == nil || projection.Facts.Links[0].URI.Fragment != "frag ment" {
		t.Fatalf("external URI link = %#v", projection.Facts.Links)
	}
	if len(projection.Facts.RootMetadata) != 1 || projection.Facts.RootMetadata[0].Value.Object()[0].Key != "count" {
		t.Fatalf("nested metadata = %#v", projection.Facts.RootMetadata)
	}
}

func TestProjectInvalidUTF8IsFatal(t *testing.T) {
	projection := project(t, "notes/bad.html", []byte{'<', 'p', '>', 0xff, '<', '/', 'p', '>'})
	if projection.Status != noteformat.ProjectionStatusFatal || len(projection.Facts.SearchRegions) != 0 || len(projection.Diagnostics) != 1 || !projection.Diagnostics[0].Blocking {
		t.Fatalf("invalid UTF-8 projection = %#v", projection)
	}
}

func TestProjectConcurrentCallsAreDeterministic(t *testing.T) {
	provider := html.New()
	inputs := []struct {
		path, content, title, search string
	}{
		{"notes/alpha.html", `<body><h1>Alpha</h1><p>First prose</p></body>`, "Alpha", "First prose"},
		{"notes/beta.html", `<body><h1>Beta</h1><p>Second prose</p></body>`, "Beta", "Second prose"},
	}
	sources := make([]noteformat.AuthoredSource, len(inputs))
	wants := make([]noteformat.Projection, len(inputs))
	for i, input := range inputs {
		var err error
		sources[i], err = noteformat.NewAuthoredSource(paths.NormalizeNotePath(input.path), provider.Descriptor(), []byte(input.content), 0)
		if err != nil {
			t.Fatal(err)
		}
		wants[i], err = provider.Project(sources[i])
		if err != nil {
			t.Fatal(err)
		}
		if wants[i].Facts.Title == nil || wants[i].Facts.Title.Value != input.title {
			t.Fatalf("serial title = %#v, want %q", wants[i].Facts.Title, input.title)
		}
		var search strings.Builder
		for _, region := range wants[i].Facts.SearchRegions {
			search.WriteString(region.Text)
		}
		if !strings.Contains(search.String(), input.search) {
			t.Fatalf("serial search = %q, want %q", search.String(), input.search)
		}
	}
	var group sync.WaitGroup
	errorsFound := make(chan error, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			index := i % len(sources)
			got, err := provider.Project(sources[index])
			if err != nil {
				errorsFound <- err
				return
			}
			if !reflect.DeepEqual(got, wants[index]) {
				errorsFound <- errors.New("concurrent projection differed")
			}
		}(i)
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
}

func project(t *testing.T, path string, content []byte) noteformat.Projection {
	t.Helper()
	provider := html.New()
	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath(path), provider.Descriptor(), content, 0)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := provider.Project(source)
	if err != nil {
		t.Fatal(err)
	}
	return projection
}
