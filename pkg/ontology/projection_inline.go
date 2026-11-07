package ontology

import (
	mdstructure "github.com/atomicobject/rhizome/pkg/markdownsyntax/structure"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func syntaxRanges(in []ByteRange) []mdstructure.ByteRange {
	out := make([]mdstructure.ByteRange, len(in))
	for i, r := range in {
		out[i] = mdstructure.ByteRange{Start: r.Start, End: r.End}
	}
	return out
}
func inlineSyntaxSpans(in []mdstructure.InlineFieldSpan) []InlineFieldSpan {
	out := make([]InlineFieldSpan, len(in))
	for i, s := range in {
		out[i] = InlineFieldSpan{Key: s.Key, Value: s.Value, LineRange: ByteRange(s.LineRange), KeyRange: ByteRange(s.KeyRange), ValueRange: ByteRange(s.ValueRange), WholeRange: ByteRange(s.WholeRange), PropertyKey: s.PropertyKey, AuthoringStyle: ontoschema.FieldAuthoringStyle(s.AuthoringStyle)}
	}
	return out
}
func scanInlineFields(content string, ranges []ByteRange) []InlineFieldSpan {
	return inlineSyntaxSpans(mdstructure.ScanInlineFields(content, syntaxRanges(ranges)))
}
func scanSectionInlineFields(content string, ranges []ByteRange) []InlineFieldSpan {
	return inlineSyntaxSpans(mdstructure.ScanSectionInlineFields(content, syntaxRanges(ranges)))
}
func scanInlineFieldsInListOwnContent(content string, ranges []ByteRange) []InlineFieldSpan {
	return inlineSyntaxSpans(mdstructure.ScanInlineFieldsInListOwnContent(content, syntaxRanges(ranges)))
}
func scanInlineFieldsInSegment(content string, offset int) []InlineFieldSpan {
	return inlineSyntaxSpans(mdstructure.ScanInlineFieldsInSegment(content, offset))
}
func scanInlineFieldsInListSegment(content string, offset int) []InlineFieldSpan {
	return inlineSyntaxSpans(mdstructure.ScanInlineFieldsInListSegment(content, offset))
}
func SectionInlineProperties(content string) map[string][]string {
	return mdstructure.SectionInlineProperties(content)
}

func trimSpaceLeftBounded(line string, start, end int) int {
	return mdstructure.TrimSpaceLeftBounded(line, start, end)
}
func trailingListMetadataTokenStart(line string, start, end int) int {
	return mdstructure.TrailingListMetadataTokenStart(line, start, end)
}
