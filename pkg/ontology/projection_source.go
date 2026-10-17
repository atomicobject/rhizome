package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

type noteDoc struct {
	Path        string
	Title       string
	Content     string
	Frontmatter map[string]interface{}
	Inline      map[string][]string
	Tags        []string
	Links       []notemeta.ResolvedNoteLink
	Projection  noteformat.Projection
	TypeName    string
	Root        *RootDocumentSnapshot
	Snapshot    *DocumentSnapshot
}

// projectNoteSource is the provider-neutral raw-note -> ontology root seam.
// Root-only formats remain one file-backed node over provider-owned facts.
func projectNoteSource(source notemeta.NoteSourceSnapshot) (*noteDoc, error) {
	if source.Format == "" {
		return nil, fmt.Errorf("ontology source format is required")
	}
	if source.Projection.Capabilities.Has(noteformat.CapabilityStructuralContentMutation) {
		return projectMarkdownNoteSource(source)
	}
	return projectRootNoteSource(source)
}

func projectRootNoteSource(source notemeta.NoteSourceSnapshot) (*noteDoc, error) {
	path := source.NotePathString()
	root, err := BuildRootDocumentSnapshot(source)
	if err != nil {
		return nil, err
	}
	fm := cloneAnyMap(source.Frontmatter)
	if fm == nil {
		fm = map[string]any{}
	}
	return &noteDoc{
		Path:        path,
		Title:       firstNonEmpty(strings.TrimSpace(source.Title), paths.NoteTitle(path)),
		Content:     string(root.RawSource),
		Frontmatter: fm,
		Inline:      cloneNoteInline(source.InlineProps),
		Tags:        append([]string(nil), source.Tags...),
		Links:       append([]notemeta.ResolvedNoteLink(nil), source.Links...),
		Projection:  source.Projection.Copy(),
		TypeName:    stringValue(fm[ontoschema.TypeFieldName()]),
		Root:        root,
	}, nil
}

func projectMarkdownNoteSource(source notemeta.NoteSourceSnapshot) (*noteDoc, error) {
	if source.Format != noteformat.FormatID("markdown") {
		return nil, fmt.Errorf("ontology Markdown projection requires Markdown source, got %q", source.Format)
	}
	doc, err := projectRootNoteSource(source)
	if err != nil {
		return nil, err
	}
	snapshot, err := BuildMarkdownDocumentSnapshot(doc.Root)
	if err != nil {
		return nil, err
	}
	doc.Root.MarkdownStructure = snapshot
	doc.Snapshot = snapshot
	return doc, nil
}

func cloneNoteInline(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func documentSnapshotForDoc(doc *noteDoc) (*DocumentSnapshot, error) {
	if doc == nil {
		return nil, fmt.Errorf("note document is required")
	}
	if doc.Snapshot != nil {
		return doc.Snapshot, nil
	}
	return BuildDocumentSnapshot(doc.Path, doc.Content, time.Time{})
}

func noteDocFromSnapshot(snapshot *DocumentSnapshot) *noteDoc {
	facts := snapshot.SelectorFacts()
	return &noteDoc{Path: snapshot.NotePath, Title: facts.Title, Content: snapshot.Content,
		Frontmatter: cloneAnyMap(facts.Frontmatter), Inline: cloneNoteInline(facts.Inline), Tags: append([]string(nil), facts.Tags...),
		TypeName: stringValue(facts.Frontmatter[ontoschema.TypeFieldName()]), Snapshot: snapshot}
}

func selectorSubjectForDoc(doc *noteDoc) *ontoschema.Subject {
	if doc == nil {
		return nil
	}
	return &ontoschema.Subject{Path: doc.Path, Title: doc.Title, Tags: doc.Tags, Frontmatter: doc.Frontmatter, Inline: doc.Inline}
}
