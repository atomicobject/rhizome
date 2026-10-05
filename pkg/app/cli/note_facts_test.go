package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// factReader supplies the already-projected facts that neutral CLI readers
// receive from command composition. It keeps parser fixtures out of the
// production reader path.
type factReader struct {
	obsidian.NoteReader
	facts NoteFacts
}

func (r factReader) NoteFacts() NoteFacts { return r.facts }

func newFactReader(reader obsidian.NoteReader, contents map[string]string) factReader {
	sources := make([]notemeta.NoteSourceSnapshot, 0, len(contents))
	for path, content := range contents {
		frontmatter, err := obsidian.ExtractFrontmatter(content)
		if err != nil {
			panic(err)
		}
		tags := frontmatterTags(frontmatter)
		for _, tag := range obsidian.ExtractHashtags(content) {
			tags = append(tags, strings.TrimPrefix(tag, "#"))
		}
		sources = append(sources, notemeta.NoteSourceSnapshot{
			Path:        paths.NormalizeNotePath(path),
			Format:      noteformat.FormatID("markdown"),
			Content:     content,
			Frontmatter: frontmatter,
			InlineProps: obsidian.ExtractInlineProperties(content),
			Tags:        tags,
		})
	}
	return factReader{NoteReader: reader, facts: NewNoteFacts(sources)}
}

func newProjectedFilesystemFactReader(t *testing.T, vaultDef obsidian.VaultDefinition, reader obsidian.NoteReader) factReader {
	t.Helper()
	sources, err := testNoteMetadataIndexer(t).BuildNoteSourceSnapshots(context.Background(), vaultDef, reader)
	if err != nil {
		t.Fatalf("build projected note facts: %v", err)
	}
	return factReader{NoteReader: reader, facts: NewNoteFacts(sources)}
}

func frontmatterTags(frontmatter map[string]any) []string {
	if frontmatter == nil {
		return nil
	}
	values, ok := frontmatter["tags"].([]string)
	if !ok {
		return nil
	}
	return append([]string(nil), values...)
}

func TestNoteFactsRetainsAuthoritativeFormatAndSearchRegions(t *testing.T) {
	t.Parallel()

	facts := NewNoteFacts([]notemeta.NoteSourceSnapshot{{
		Path:    paths.NormalizeNotePath("Notes/Rendered.future"),
		Format:  noteformat.FormatID("future"),
		Content: "source bytes must not select a parser",
		Projection: noteformat.Projection{Facts: noteformat.ProjectionFacts{
			SearchRegions: []noteformat.SearchRegionFact{{
				Origin:    noteformat.SearchRegionDerived,
				Kind:      noteformat.SearchRegionVisible,
				Text:      "provider-projected visible text",
				MediaType: "text/plain",
			}},
		}},
	}, {Path: paths.NormalizeNotePath("Notes/Decision.MD"), Tags: []string{"decision"}}})
	markdown, ok := facts.LookupFact("Notes/Decision.MD")
	if !ok || len(markdown.Tags) != 1 || markdown.Tags[0] != "decision" {
		t.Fatalf("mixed-case Markdown facts = %#v, %v", markdown.Tags, ok)
	}
	if _, ok := facts.LookupFact("Notes/Reference.html"); ok {
		t.Fatal("unpublished HTML facts appeared")
	}

	fact, ok := facts.LookupFact("Notes/Rendered.future")
	if !ok {
		t.Fatal("future provider facts are unavailable")
	}
	if fact.Format != noteformat.FormatID("future") {
		t.Fatalf("format = %q, want future", fact.Format)
	}
	if len(fact.SearchRegions) != 1 || fact.SearchRegions[0].Text != "provider-projected visible text" {
		t.Fatalf("search regions = %#v", fact.SearchRegions)
	}
	fact.SearchRegions[0].Text = "mutated"
	reloaded, ok := facts.LookupFact("Notes/Rendered.future")
	if !ok || reloaded.SearchRegions[0].Text != "provider-projected visible text" {
		t.Fatalf("search-region copy = %#v, %v", reloaded.SearchRegions, ok)
	}
}
