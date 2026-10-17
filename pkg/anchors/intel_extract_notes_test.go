package codeanchor

import (
	"testing"
)

func TestExtractIntelDocSections_IgnoresHeadingsInCodeBlocks(t *testing.T) {
	content := "# Title\n\n```markdown\n## Inside\n```\n\n## After\nBody\n"

	sections, _, _ := extractMarkdownIntelDocSections("notes/example.md", content)

	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}

	if sections[0].Title != "Title" {
		t.Fatalf("unexpected first title: %q", sections[0].Title)
	}

	if sections[1].Title != "After" {
		t.Fatalf("unexpected second title: %q", sections[1].Title)
	}

}

func TestExtractIntelDocSectionsPersistsCanonicalSourceIdentity(t *testing.T) {
	const path = "notes/example.md"
	content := "# Title\n\n## Decision\nfirst\n\n## Decision\n^decision\nlast\n"
	sections, _, _ := extractMarkdownIntelDocSections(path, content)
	if len(sections) != 3 {
		t.Fatalf("got %d sections", len(sections))
	}
	want := []string{"notes/example.md#title-0", "notes/example.md#decision-9", "notes/example.md#^decision"}
	for i, section := range sections {
		if section.SourceNodeID != want[i] {
			t.Errorf("section %d identity = %q; want %q", i, section.SourceNodeID, want[i])
		}
	}
	root, _, _ := extractMarkdownIntelDocSections(path, "plain body")
	if len(root) != 1 || root[0].SourceNodeID != "" {
		t.Fatalf("headingless identity = %#v", root)
	}
}
