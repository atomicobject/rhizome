package ontology

import (
	mdstructure "github.com/atomicobject/rhizome/pkg/markdownsyntax/structure"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
	"time"
)

func markdownSnapshotFromStructure(path, content string, modTime time.Time, facts *noteformat.StructureFacts, fm map[string]any) *DocumentSnapshot {
	snapshot := &DocumentSnapshot{NotePath: path, Content: content, ModTime: modTime, Frontmatter: cloneAnyMap(fm), FrontmatterRange: frontmatterRange(content), ContentFingerprint: hashText(content), SectionsByID: map[string]*SectionNode{}, SourceSpansByID: map[string]*MarkdownSourceSpan{}, selectorFacts: markdownSelectorFacts(path, content, fm)}
	spans := map[noteformat.OwnerKey]*MarkdownSourceSpan{}
	sections := map[noteformat.OwnerKey]*SectionNode{}
	for _, o := range facts.Owners {
		if o.Markdown == nil {
			continue
		}
		m := o.Markdown
		r := ByteRange{o.Range.Range.StartByte, o.Range.Range.EndByte}
		cr := ByteRange{m.ContentRange.Range.StartByte, m.ContentRange.Range.EndByte}
		span := &MarkdownSourceSpan{ID: m.SourceID, NotePath: path, Shape: ontoschema.EmbeddedSourceShape(o.Shape), Title: o.Title.Value, Range: r, ContentRange: cr, Content: content[r.Start:r.End], MalformedBlockID: m.MalformedBlockID}
		if o.Parent != facts.Root {
			if parent := spans[o.Parent]; parent != nil {
				span.ParentID = parent.ID
			}
		}
		if m.MarkerRange.Present {
			span.MarkerRange = ByteRange{m.MarkerRange.Range.StartByte, m.MarkerRange.Range.EndByte}
		}
		for _, l := range o.Locators {
			if l.Kind == noteformat.FragmentTargetBlock {
				span.BlockID = l.Value
			}
		}
		if o.Shape != noteformat.OwnerSection {
			span.Markers = make([]MarkerSpan, 0, len(m.Markers))
		}
		for _, marker := range m.Markers {
			span.Markers = append(span.Markers, MarkerSpan{Value: marker.Value, Range: ByteRange{marker.Range.Range.StartByte, marker.Range.Range.EndByte}})
		}
		if m.Checkbox != nil {
			span.Checkbox = &CheckboxState{Checked: m.Checkbox.Checked, TokenRange: ByteRange{m.Checkbox.TokenRange.StartByte, m.Checkbox.TokenRange.EndByte}}
		}
		for _, own := range o.OwnRanges {
			span.OwnContentRanges = append(span.OwnContentRanges, ByteRange{own.StartByte, own.EndByte})
		}
		spans[o.Key] = span
		snapshot.SourceSpansByID[span.ID] = span
		if o.Shape == noteformat.OwnerSection {
			section := &SectionNode{ID: span.ID, NotePath: path, Title: span.Title, Level: headingLevel(m.HeadingLevel), BlockID: span.BlockID, MalformedBlockID: span.MalformedBlockID, StartByte: r.Start, EndByte: r.End, Content: span.Content}
			sections[o.Key] = section
			snapshot.SectionsByID[section.ID] = section
		}
	}
	for _, o := range facts.Owners {
		span := spans[o.Key]
		if span == nil {
			continue
		}
		if parent := spans[o.Parent]; parent != nil {
			parent.Children = append(parent.Children, span)
		}
		if section := sections[o.Key]; section != nil {
			if parent := sections[o.Parent]; parent != nil {
				parent.Children = append(parent.Children, section)
			} else {
				snapshot.Sections = append(snapshot.Sections, section)
			}
			snapshot.SourceSpans = append(snapshot.SourceSpans, span)
		}
	}
	for _, o := range facts.Owners {
		if span := spans[o.Key]; span != nil && span.Shape != ontoschema.EmbeddedSourceShapeSection {
			snapshot.SourceSpans = append(snapshot.SourceSpans, span)
		}
	}
	// The edit DTO retains the original section-own scope, including list syntax.
	for _, o := range facts.Owners {
		if span := spans[o.Key]; span != nil && span.Shape == ontoschema.EmbeddedSourceShapeSection {
			section := sections[o.Key]
			cursor := section.StartByte
			span.OwnContentRanges = nil
			for _, child := range section.Children {
				if child.StartByte > cursor {
					span.OwnContentRanges = append(span.OwnContentRanges, ByteRange{cursor, child.StartByte})
				}
				cursor = child.EndByte
			}
			if cursor < section.EndByte {
				span.OwnContentRanges = append(span.OwnContentRanges, ByteRange{cursor, section.EndByte})
			}
		}
	}
	snapshot.StructureFingerprint = hashText(structureFingerprintText(path, snapshot.Sections))
	return snapshot
}

func syntaxSourceSpan(s *MarkdownSourceSpan) *mdstructure.SourceSpan {
	out := &mdstructure.SourceSpan{Title: s.Title, Shape: mdstructure.Shape(s.Shape), ContentRange: mdstructure.ByteRange(s.ContentRange)}
	for _, m := range s.Markers {
		out.Markers = append(out.Markers, mdstructure.MarkerSpan{Value: m.Value, Range: mdstructure.ByteRange(m.Range)})
	}
	return out
}
func sourceSpanItemParts(content string, span *MarkdownSourceSpan) (string, string, string) {
	return mdstructure.SourceSpanItemParts(content, syntaxSourceSpan(span))
}
func sourceSpanDisplayTitle(content string, span *MarkdownSourceSpan) string {
	if span.Shape != ontoschema.EmbeddedSourceShapeListItem && span.Shape != ontoschema.EmbeddedSourceShapeCheckboxItem {
		return strings.TrimSpace(span.Title)
	}
	s := syntaxSourceSpan(span)
	headline := mdstructure.SourceSpanItemHeadline(content, s.ContentRange, s.Markers)
	if title, _, ok := mdstructure.SplitTitledItemHeadline(headline); ok {
		return title
	}
	if headline != "" {
		return headline
	}
	return strings.TrimSpace(span.Title)
}
