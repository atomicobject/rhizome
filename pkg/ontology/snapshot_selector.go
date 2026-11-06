package ontology

import (
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

// BuildMarkdownDocumentSnapshot attaches provider-current selector facts to a
// genuine Markdown syntax snapshot. Empty provider facts remain authoritative.
func BuildMarkdownDocumentSnapshot(root *RootDocumentSnapshot) (*DocumentSnapshot, error) {
	if root == nil || root.Format != noteformat.FormatID("markdown") {
		return nil, fmt.Errorf("Markdown snapshot requires a Markdown source")
	}
	modTime := time.Time{}
	if root.Mtime > 0 {
		modTime = time.Unix(root.Mtime, 0)
	}
	if root.Projection.Facts.Structure == nil {
		return nil, fmt.Errorf("Markdown provider did not supply structure")
	}
	snapshot := markdownSnapshotFromStructure(root.NotePath.String(), string(root.RawSource), modTime, root.Projection.Facts.Structure, root.Metadata)
	snapshot.root = root
	snapshot.selectorFacts = cloneSelectorFacts(&ontoschema.Subject{
		Path: root.NotePath.String(), Title: root.Title, Tags: root.Tags,
		Frontmatter: root.Metadata, Inline: root.InlineProperties,
	})
	return snapshot, nil
}

// markdownEditView is the syntax boundary used after neutral source capture.
func markdownEditView(root *RootDocumentSnapshot) (*DocumentSnapshot, error) {
	if root == nil || root.Format != "markdown" || root.Projection.Facts.Structure == nil {
		return nil, nil
	}
	return BuildMarkdownDocumentSnapshot(root)
}

// SelectorFacts returns a defensive copy of the facts captured when this
// snapshot was built. Reading facts never parses source or replaces provider facts.
func (s *DocumentSnapshot) SelectorFacts() ontoschema.Subject {
	if s == nil || s.selectorFacts == nil {
		return ontoschema.Subject{}
	}
	return *cloneSelectorFacts(s.selectorFacts)
}

func markdownSelectorFacts(notePath, content string, frontmatter map[string]any) *ontoschema.Subject {
	return &ontoschema.Subject{
		Path: notePath, Title: markdownsyntax.TitleValue(notePath, content, frontmatter),
		Tags:        markdownsyntax.TagValues(content, frontmatter),
		Frontmatter: cloneSelectorMap(frontmatter), Inline: markdownsyntax.ExtractInlineProperties(content),
	}
}

func cloneSelectorFacts(facts *ontoschema.Subject) *ontoschema.Subject {
	return &ontoschema.Subject{Path: facts.Path, Title: facts.Title, Tags: append([]string(nil), facts.Tags...),
		Frontmatter: cloneSelectorMap(facts.Frontmatter), Inline: cloneNoteInline(facts.Inline)}
}

func cloneSelectorMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneSelectorValue(value)
	}
	return out
}

func cloneSelectorValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneSelectorMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneSelectorValue(item)
		}
		return out
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}
