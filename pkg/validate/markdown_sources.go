package validate

import (
	"context"
	"fmt"
	"sort"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

// markdownValidationSources selects provider-current Markdown source facts for
// validation checks that still need Markdown syntax. It is the only admission
// path for those checks: descriptor-only and future provider sources must not
// enter their legacy Markdown parsers.
func markdownValidationSources(ctx context.Context, runCtx RunContext) ([]notemeta.NoteSourceSnapshot, error) {
	if err := runCtx.NoteMetadata.Validate(); err != nil {
		return nil, fmt.Errorf("cannot identify provider-current Markdown validation sources: %w", err)
	}
	var sources []notemeta.NoteSourceSnapshot
	if runCtx.sourceSnapshot != nil {
		sources = append([]notemeta.NoteSourceSnapshot(nil), runCtx.sourceSnapshot.sources...)
	} else {
		var err error
		sources, err = runCtx.NoteMetadata.BuildNoteSourceSnapshots(ctx, runCtx.VaultDef, runCtx.NoteReader)
		if err != nil {
			return nil, err
		}
	}
	markdown := make([]notemeta.NoteSourceSnapshot, 0, len(sources))
	for _, source := range sources {
		if source.Format == noteformat.FormatID("markdown") {
			markdown = append(markdown, source)
		}
	}
	sort.Slice(markdown, func(i, j int) bool { return markdown[i].Path < markdown[j].Path })
	return markdown, nil
}

func markdownSourceContentByPath(sources []notemeta.NoteSourceSnapshot) map[string]string {
	content := make(map[string]string, len(sources))
	for _, source := range sources {
		content[source.Path.String()] = source.Content
	}
	return content
}

// markdownBlockFragmentAuthored is the syntax-authoring boundary for excluding
// frontmatter examples; other providers retain their exact authored spans.
func markdownBlockFragmentAuthored(source notemeta.NoteSourceSnapshot, fact noteformat.UnresolvedAuthoredLinkFact) bool {
	if source.Format != noteformat.FormatID("markdown") {
		return true
	}
	if _, bodyStart, ok := markdownsyntax.Frontmatter(source.Content); ok && fact.RawRange.StartByte < bodyStart {
		return false
	}
	return true
}
