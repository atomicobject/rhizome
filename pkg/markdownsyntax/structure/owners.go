package structure

import (
	"github.com/atomicobject/rhizome/pkg/markdownsyntax/sectionparse"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"sort"
	"strconv"
)

// Parse moves the existing heading/list scanners behind the provider boundary.
// Syntax candidates stay independent of ontology type and identity selection.
func Parse(notePath, content string) *noteformat.StructureFacts {
	sections := sectionparse.Parse(notePath, content)
	spans := append(sectionSourceSpans(sections), ParseMarkdownListSourceSpans(notePath, content)...)
	linkListSourceSpansToSections(spans)
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].Range.Start < spans[j].Range.Start })
	facts := &noteformat.StructureFacts{Root: "$document"}
	root := noteformat.OwnerFact{Key: facts.Root, Shape: noteformat.OwnerDocument, Range: exact(ByteRange{End: len(content)})}

	levels := map[string]int{}
	var indexLevels func([]*sectionparse.Node)
	indexLevels = func(nodes []*sectionparse.Node) {
		for _, s := range nodes {
			levels[s.ID] = s.Level
			indexLevels(s.Children)
		}
	}
	indexLevels(sections)
	ordinals := map[noteformat.OwnerKey]int{}
	keys := map[*SourceSpan]noteformat.OwnerKey{}
	parents := map[*SourceSpan]*SourceSpan{}
	used := map[string]bool{}
	for _, span := range spans {
		key := span.ID
		if used[key] {
			key += "~" + strconv.Itoa(span.Range.Start)
		}
		used[key] = true
		keys[span] = noteformat.OwnerKey(key)
		for _, child := range span.Children {
			parents[child] = span
		}
	}
	var rootChildren []ByteRange
	facts.Owners = append(facts.Owners, root)
	for i, s := range spans {
		parent := keys[parents[s]]
		if parent == "" {
			parent = facts.Root
			rootChildren = append(rootChildren, s.Range)
		}
		o := noteformat.OwnerFact{Key: keys[s], Parent: parent, Collection: "children", Ordinal: ordinals[parent], Order: i + 1, Shape: noteformat.OwnerShape(s.Shape), Address: "/markdown/" + strconv.Itoa(s.Range.Start), Range: exact(s.Range), Title: &noteformat.TitleFact{Value: s.Title}}
		ordinals[parent]++
		childRanges := make([]ByteRange, 0, len(s.Children))
		for _, child := range s.Children {
			childRanges = append(childRanges, child.Range)
		}
		for _, r := range subtractByteRanges(s.ContentRange, childRanges) {
			o.OwnRanges = append(o.OwnRanges, noteformat.SourceRange{StartByte: r.Start, EndByte: r.End})
		}
		md := &noteformat.MarkdownOwnerFact{SourceID: s.ID, HeadingLevel: levels[s.ID], MalformedBlockID: s.MalformedBlockID, ContentRange: exact(s.ContentRange)}
		if s.Shape != Section {
			md.MarkerRange = exact(s.MarkerRange)
		}
		for _, m := range s.Markers {
			md.Markers = append(md.Markers, noteformat.TagFact{Value: m.Value, Range: exact(m.Range)})
		}
		if s.Checkbox != nil {
			md.Checkbox = &noteformat.CheckboxFact{Checked: s.Checkbox.Checked, TokenRange: noteformat.SourceRange{StartByte: s.Checkbox.TokenRange.Start, EndByte: s.Checkbox.TokenRange.End}}
		}
		if s.BlockID != "" {
			o.Locators = append(o.Locators, noteformat.OwnerLocatorFact{Kind: noteformat.FragmentTargetBlock, Value: s.BlockID})
		}
		if s.Shape == Section {
			o.Locators = append(o.Locators, noteformat.OwnerLocatorFact{Kind: noteformat.FragmentTargetHeading, Value: s.Title})
		} else {
			title, summary, detail := SourceSpanItemParts(content, s)
			md.ItemTitle = &noteformat.TitleFact{Value: title}
			md.ItemSummary = &noteformat.TitleFact{Value: summary}
			md.ItemDetail = &noteformat.TitleFact{Value: detail}
		}
		o.DisplayTitle = &noteformat.TitleFact{Value: displayTitle(content, s)}
		o.Markdown = md
		// Original own-content scopes are preserved for Markdown field extraction.
		occurrences := ScanSectionInlineFields(content, s.OwnContentRanges)
		if s.Shape != Section {
			occurrences = ScanInlineFieldsInListOwnContent(content, s.OwnContentRanges)
		}
		for _, p := range occurrences {
			value, _ := noteformat.NewMetadataValue(p.Value)
			o.Properties = append(o.Properties, noteformat.PropertyOccurrenceFact{Key: p.Key, Source: "inline", Value: facts.AddMetadataValue(value), Range: exact(p.WholeRange), KeyRange: exact(p.KeyRange), ValueRange: exact(p.ValueRange)})
		}
		facts.Owners = append(facts.Owners, o)
	}
	for _, r := range subtractByteRanges(ByteRange{End: len(content)}, rootChildren) {
		facts.Owners[0].OwnRanges = append(facts.Owners[0].OwnRanges, noteformat.SourceRange{StartByte: r.Start, EndByte: r.End})
	}
	return facts
}

func exact(r ByteRange) noteformat.OptionalSourceRange {
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: r.Start, EndByte: r.End}}
}
func displayTitle(content string, s *SourceSpan) string {
	if s.Shape == Section {
		return s.Title
	}
	headline := SourceSpanItemHeadline(content, s.ContentRange, s.Markers)
	if title, _, ok := SplitTitledItemHeadline(headline); ok {
		return title
	}
	if headline != "" {
		return headline
	}
	return s.Title
}
