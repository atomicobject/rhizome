package actions

import (
	"sort"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// NoteFacts is the format-neutral metadata projection used by CLI readers.
// It is populated only from injected, runtime-current notemeta snapshots. A
// plain NoteReader may still return source bytes, but it is not authority to
// parse note syntax.
type NoteFacts struct {
	byPath map[string]NoteFact
}

// NoteFact is a detached provider-projected source record for format-aware
// consumers. Format and search regions stay authoritative to the source
// snapshot: consumers must not infer syntax from the path or content.
type NoteFact struct {
	Format        noteformat.FormatID
	Frontmatter   map[string]any
	InlineProps   map[string][]string
	Tags          []string
	Content       string
	SearchRegions []noteformat.SearchRegionFact
}

// NoteFactsProvider is the narrow injected seam for neutral CLI readers. The
// owner composes it from an explicit notemeta Indexer/runtime or persisted
// metadata store; consumers never construct a provider or parse source bytes.
type NoteFactsProvider interface {
	NoteFacts() NoteFacts
}

// NoteFactsFromReader returns injected current projection facts, if any.
func NoteFactsFromReader(note obsidian.NoteReader) NoteFacts {
	provider, ok := note.(NoteFactsProvider)
	if !ok || provider == nil {
		return NoteFacts{}
	}
	return provider.NoteFacts()
}

// NewNoteFacts adapts the explicit provider/runtime boundary into the small
// read model required by CLI rendering. Descriptor-only and failed projections
// never appear in a valid source snapshot, so no consumer can fall through to
// a Markdown parser for those paths.
func NewNoteFacts(sources []notemeta.NoteSourceSnapshot) NoteFacts {
	set := NoteFacts{byPath: make(map[string]NoteFact, len(sources))}
	for _, source := range sources {
		path := paths.NormalizeNotePath(source.Path.String()).String()
		if path == "" {
			continue
		}
		set.byPath[path] = NoteFact{
			Format:        source.Format,
			Frontmatter:   cloneFactsMap(source.Frontmatter),
			InlineProps:   cloneFactsInline(source.InlineProps),
			Tags:          append([]string(nil), source.Tags...),
			Content:       source.Content,
			SearchRegions: source.Projection.Copy().Facts.SearchRegions,
		}
	}
	return set
}

// LookupFact returns copied source facts, including the selected provider
// format and projected search regions, for one current note.
func (f NoteFacts) LookupFact(path string) (NoteFact, bool) {
	entry, ok := f.byPath[paths.NormalizeNotePath(path).String()]
	if !ok {
		return NoteFact{}, false
	}
	return NoteFact{
		Format:        entry.Format,
		Frontmatter:   cloneFactsMap(entry.Frontmatter),
		InlineProps:   cloneFactsInline(entry.InlineProps),
		Tags:          append([]string(nil), entry.Tags...),
		Content:       entry.Content,
		SearchRegions: append([]noteformat.SearchRegionFact(nil), entry.SearchRegions...),
	}, true
}

// Paths returns the canonical paths with current provider projections.
func (f NoteFacts) Paths() []string {
	pathsOut := make([]string, 0, len(f.byPath))
	for path := range f.byPath {
		pathsOut = append(pathsOut, path)
	}
	sort.Strings(pathsOut)
	return pathsOut
}

func cloneFactsMap(source map[string]interface{}) map[string]any {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func cloneFactsInline(source map[string][]string) map[string][]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string][]string, len(source))
	for key, values := range source {
		out[key] = append([]string(nil), values...)
	}
	return out
}
