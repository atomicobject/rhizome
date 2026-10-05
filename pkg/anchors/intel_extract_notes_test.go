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
