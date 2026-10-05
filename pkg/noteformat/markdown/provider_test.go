package markdown_test

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestProviderDeclaresMarkdownCapabilities(t *testing.T) {
	descriptor := markdown.New().Descriptor()
	if descriptor.ID != "markdown" {
		t.Fatalf("ID = %q, want markdown", descriptor.ID)
	}
	if !descriptor.Capabilities.Has(noteformat.CapabilitySearchableContentProjection) {
		t.Fatal("Markdown provider must declare searchable content projection")
	}
	if !descriptor.Capabilities.Has(noteformat.CapabilityStructuralContentMutation) {
		t.Fatal("Markdown provider must declare structural content mutation")
	}
	if !descriptor.Capabilities.Has(noteformat.CapabilityFileRenameMove) {
		t.Fatal("Markdown provider must declare rename/move participation")
	}
}

func TestProjectEmitsUnresolvedMarkdownSyntaxFacts(t *testing.T) {
	content := "---\ntitle: Planned title\ntags: [product, shared]\naliases: [PLAN-1, Plan]\npriority: 3\n---\n\nStatus:: active\n\nSee [[Target#section|Label]] and ![Diagram](docs/diagram.md#details). #shared #inline\n\n# Heading\nParagraph ^block-a\n"
	projection := projectMarkdown(t, "Notes/MiXeD.MD", content)

	if projection.Status != noteformat.ProjectionStatusCurrent {
		t.Fatalf("status = %q, want current", projection.Status)
	}
	if projection.Facts.Title == nil || projection.Facts.Title.Value != "Planned title" || projection.Facts.Title.Range.Present {
		t.Fatalf("title = %#v, want frontmatter title without a fabricated range", projection.Facts.Title)
	}
	if got := projection.Facts.RootMetadata; len(got) != 4 || got[0].Key != "aliases" || got[3].Key != "title" {
		t.Fatalf("metadata keys = %#v, want deterministic sorted frontmatter facts", got)
	}
	if value := projection.Facts.RootMetadata[1].Value.Export(); value != json.Number("3") {
		t.Fatalf("priority metadata = %#v, want canonical number", value)
	}
	if got := projection.Facts.Aliases; len(got) != 2 || got[0].Value != "PLAN-1" || got[1].Value != "Plan" {
		t.Fatalf("aliases = %#v", got)
	}
	if got := projection.Facts.Tags; len(got) != 3 || got[0].Value != "product" || got[1].Value != "shared" || got[2].Value != "inline" {
		t.Fatalf("tags = %#v", got)
	}
	if got := projection.Facts.InlineProperties; len(got) != 1 || got[0].Key != "Status" || got[0].Value != "active" || got[0].Range.Present {
		t.Fatalf("inline properties = %#v", got)
	}
	if got := projection.Facts.Links; len(got) != 2 {
		t.Fatalf("links = %#v", got)
	} else {
		if got[0].Resolution != noteformat.LinkResolutionNoteReference || got[0].Syntax != "wikilink" || got[0].Subtype != "alias" || got[0].ResolverInput != "Target#section" || got[0].Path != "Target" || got[0].Fragment != "section" || !got[0].DisplayRange.Present {
			t.Fatalf("wikilink = %#v", got[0])
		}
		if got[1].Resolution != noteformat.LinkResolutionRelativePath || got[1].Syntax != "markdown" || got[1].Subtype != "embed" || got[1].ResolverInput != "docs/diagram.md#details" || got[1].Path != "docs/diagram.md" || got[1].Fragment != "details" {
			t.Fatalf("Markdown link = %#v", got[1])
		}
		for _, link := range got {
			if content[link.RawRange.StartByte:link.RawRange.EndByte] == "" || content[link.TargetRange.StartByte:link.TargetRange.EndByte] != link.Target {
				t.Fatalf("link ranges do not address authored syntax: %#v", link)
			}
		}
	}
	if got := projection.Facts.SearchRegions; len(got) != 1 || got[0].Origin != noteformat.SearchRegionAuthored || got[0].Kind != noteformat.SearchRegionVisible || got[0].MediaType != "text/markdown" || got[0].Text != content || !got[0].Range.Present || got[0].Range.Range != (noteformat.SourceRange{StartByte: 0, EndByte: len(content)}) {
		t.Fatalf("search regions = %#v", got)
	}
	if got := projection.Facts.FragmentTargets; len(got) != 2 || got[0].Kind != "heading" || got[0].Text != "Heading" || got[1].Kind != "block" || got[1].Text != "block-a" {
		t.Fatalf("fragment targets = %#v", got)
	}
}

func TestProjectNormalizesLeadingHashesBeforeDeduplicatingTags(t *testing.T) {
	projection := projectMarkdown(t, "notes/tags.md", "---\ntags: [\" #shared\"]\n---\n#shared\n")

	if got := projection.Facts.Tags; len(got) != 1 || got[0].Value != "shared" {
		t.Fatalf("tags = %#v, want one canonical shared tag", got)
	}
}

func TestProjectRetainsWhitespaceOnlyAuthoredSearchRegion(t *testing.T) {
	content := " \t\n"
	projection := projectMarkdown(t, "notes/whitespace.md", content)

	if got := projection.Facts.SearchRegions; len(got) != 1 || got[0].Text != content || got[0].Origin != noteformat.SearchRegionAuthored || got[0].Kind != noteformat.SearchRegionVisible || got[0].Range.Range != (noteformat.SourceRange{StartByte: 0, EndByte: len(content)}) {
		t.Fatalf("search regions = %#v", got)
	}
}

func TestProjectPreservesPermissiveMalformedFrontmatterBehavior(t *testing.T) {
	projection := projectMarkdown(t, "notes/fallback.md", "---\ntitle: [unterminated\n---\n# Body title\n#inline\n")

	if projection.Status != noteformat.ProjectionStatusCurrent {
		t.Fatalf("status = %q, want current", projection.Status)
	}
	if projection.Facts.Title == nil || projection.Facts.Title.Value != "Body title" {
		t.Fatalf("title = %#v, want H1 fallback", projection.Facts.Title)
	}
	if len(projection.Facts.RootMetadata) != 0 || len(projection.Facts.Tags) != 1 || projection.Facts.Tags[0].Value != "inline" {
		t.Fatalf("facts = %#v, want non-frontmatter syntax retained", projection.Facts)
	}
	if len(projection.Diagnostics) != 1 || projection.Diagnostics[0].Code != "markdown_frontmatter_invalid" || projection.Diagnostics[0].Blocking {
		t.Fatalf("diagnostics = %#v, want one nonblocking frontmatter diagnostic", projection.Diagnostics)
	}
}

func TestProjectTitlePrecedence(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		want    string
	}{
		{name: "title wins", path: "notes/fallback.md", content: "---\ntitle: Explicit\nname: Secondary\n---\n# Heading\n", want: "Explicit"},
		{name: "name before H1", path: "notes/fallback.md", content: "---\nname: Preferred\n---\n# Heading\n", want: "Preferred"},
		{name: "one H1", path: "notes/fallback.md", content: "# Heading\n", want: "Heading"},
		{name: "many H1 fall back to basename", path: "notes/Fallback.MD", content: "# One\n# Two\n", want: "Fallback"},
		{name: "wiki display title", path: "notes/fallback.md", content: "# [[Target|Shown]] ###\n", want: "Shown"},
		{name: "C sharp title", path: "notes/fallback.md", content: "# C#\n", want: "C#"},
		{name: "indented H1 excluded", path: "notes/fallback.md", content: "  # Not a title\n", want: "fallback"},
		{name: "single equals setext excluded", path: "notes/fallback.md", content: "No title\n=\n", want: "fallback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projection := projectMarkdown(t, test.path, test.content)
			if projection.Facts.Title == nil || projection.Facts.Title.Value != test.want {
				t.Fatalf("title = %#v, want %q", projection.Facts.Title, test.want)
			}
		})
	}
}

func TestProjectLinkSyntaxParity(t *testing.T) {
	content := "[[plain]] [[alias|Label]] ![[embed]] [[note#heading]] [[#same]] [markdown](docs/one.md) [angle](<Folder/My Spec.md#details> \"Readable title\") [external](https://example.com) \\[[escaped]] `[[code]]`\n"
	projection := projectMarkdown(t, "notes/links.md", content)
	got := projection.Facts.Links
	if len(got) != 7 {
		t.Fatalf("links = %#v, want seven internal non-escaped non-code links", got)
	}
	want := []struct {
		resolution noteformat.LinkResolution
		subtype    noteformat.AuthoredLinkSubtype
		input      string
		path       string
		fragment   string
	}{
		{noteformat.LinkResolutionNoteReference, "basic", "plain", "plain", ""},
		{noteformat.LinkResolutionNoteReference, "alias", "alias", "alias", ""},
		{noteformat.LinkResolutionNoteReference, "embed", "embed", "embed", ""},
		{noteformat.LinkResolutionNoteReference, "heading", "note#heading", "note", "heading"},
		{noteformat.LinkResolutionNoteReference, "heading", "#same", "", "same"},
		{noteformat.LinkResolutionRelativePath, "basic", "docs/one.md", "docs/one.md", ""},
		{noteformat.LinkResolutionRelativePath, "heading", `<Folder/My Spec.md#details> "Readable title"`, "Folder/My Spec.md", "details"},
	}
	for index, expected := range want {
		link := got[index]
		if link.Resolution != expected.resolution || link.Subtype != expected.subtype || link.ResolverInput != expected.input || link.Path != expected.path || link.Fragment != expected.fragment {
			t.Fatalf("link %d = %#v, want resolution=%q subtype=%q input=%q path=%q fragment=%q", index, link, expected.resolution, expected.subtype, expected.input, expected.path, expected.fragment)
		}
	}
}

func TestProjectPreservesCRLFRangesAndDuplicateFragmentOrdinals(t *testing.T) {
	content := "😀 [[Target]]\r\n# Repeat\r\nParagraph ^block\r\n# Repeat\r\nParagraph ^block\r\n"
	projection := projectMarkdown(t, "notes/ranges.md", content)
	if got := projection.Facts.Links; len(got) != 1 || content[got[0].RawRange.StartByte:got[0].RawRange.EndByte] != "[[Target]]" {
		t.Fatalf("CRLF link ranges = %#v", got)
	}
	if got := projection.Facts.FragmentTargets; len(got) != 4 || got[0].Ordinal != 1 || got[1].Ordinal != 1 || got[2].Ordinal != 2 || got[3].Ordinal != 2 {
		t.Fatalf("fragment target ordinals = %#v", got)
	}
	for _, target := range projection.Facts.FragmentTargets {
		if !target.Range.Present || content[target.Range.Range.StartByte:target.Range.Range.EndByte] == "" {
			t.Fatalf("target has invalid authored range: %#v", target)
		}
	}
}

func TestProjectPreservesYAMLScalarAndListKinds(t *testing.T) {
	content := "---\nname: Example\ninteger: 7\nfloat: 1.25\nboolean: true\ndate: 2026-08-03\ntimestamp: 2026-08-03T09:10:11.123456789-04:00\nlist: [one, 2, false]\n---\n"
	projection := projectMarkdown(t, "notes/kinds.md", content)
	values := make(map[string]noteformat.MetadataValue, len(projection.Facts.RootMetadata))
	for _, fact := range projection.Facts.RootMetadata {
		values[fact.Key] = fact.Value
	}
	for key, want := range map[string]noteformat.MetadataValueKind{
		"name": noteformat.MetadataString, "integer": noteformat.MetadataInteger, "float": noteformat.MetadataFloat,
		"boolean": noteformat.MetadataBoolean, "date": noteformat.MetadataDate, "timestamp": noteformat.MetadataTimestamp,
		"list": noteformat.MetadataArray,
	} {
		if got := values[key].Kind(); got != want {
			t.Fatalf("%s kind = %q, want %q", key, got, want)
		}
	}
	if got := values["timestamp"].Export(); got != "2026-08-03T09:10:11.123456789-04:00" {
		t.Fatalf("timestamp = %#v, want offset preserved", got)
	}
}

func TestProjectPreservesDateOnlyMetadataSeparatelyFromTimestamp(t *testing.T) {
	projection := projectMarkdown(t, "notes/dates.md", "---\ndate: 2026-08-03\ntimestamp: 2026-08-03T00:00:00-04:00\nnested:\n  date: 2026-08-04\n  timestamp: 2026-08-04T00:00:00+02:00\ntemporal_list: [2026-08-05, 2026-08-05T00:00:00Z]\n---\n")
	values := make(map[string]noteformat.MetadataValue, len(projection.Facts.RootMetadata))
	for _, fact := range projection.Facts.RootMetadata {
		values[fact.Key] = fact.Value
	}
	if got := values["date"]; got.Kind() != noteformat.MetadataDate || got.Export() != "2026-08-03" {
		t.Fatalf("date = kind %q, value %#v; want date-only calendar value", got.Kind(), got.Export())
	}
	if got := values["timestamp"]; got.Kind() != noteformat.MetadataTimestamp || got.Export() != "2026-08-03T00:00:00-04:00" {
		t.Fatalf("timestamp = kind %q, value %#v; want normalized timestamp", got.Kind(), got.Export())
	}
	nested := make(map[string]noteformat.MetadataValue)
	for _, entry := range values["nested"].Object() {
		nested[entry.Key] = entry.Value
	}
	if got := nested["date"]; got.Kind() != noteformat.MetadataDate || got.Export() != "2026-08-04" {
		t.Fatalf("nested date = kind %q, value %#v; want date-only calendar value", got.Kind(), got.Export())
	}
	if got := nested["timestamp"]; got.Kind() != noteformat.MetadataTimestamp || got.Export() != "2026-08-04T00:00:00+02:00" {
		t.Fatalf("nested timestamp = kind %q, value %#v; want exact-midnight timestamp", got.Kind(), got.Export())
	}
	temporalList := values["temporal_list"].Array()
	if got := temporalList[0]; got.Kind() != noteformat.MetadataDate || got.Export() != "2026-08-05" {
		t.Fatalf("list date = kind %q, value %#v; want date-only calendar value", got.Kind(), got.Export())
	}
	if got := temporalList[1]; got.Kind() != noteformat.MetadataTimestamp || got.Export() != "2026-08-05T00:00:00Z" {
		t.Fatalf("list timestamp = kind %q, value %#v; want exact-midnight timestamp", got.Kind(), got.Export())
	}
}

func projectMarkdown(t *testing.T, notePath, content string) noteformat.Projection {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	provider, ok := runtime.ProviderForPath(paths.Normalize(notePath))
	if !ok {
		t.Fatalf("ProviderForPath(%q) returned no provider", notePath)
	}
	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath(notePath), provider.Descriptor(), []byte(content), 17)
	if err != nil {
		t.Fatalf("NewAuthoredSource: %v", err)
	}
	projection, err := runtime.Project(source)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	return projection
}
