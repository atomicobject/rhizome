package noteformat_test

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

func TestProjectionFactsRequireClosedLinkSemanticsAndNestedSourceRanges(t *testing.T) {
	capabilities := noteformat.MustCapabilities(noteformat.CapabilitySourceReading)
	validLink := noteformat.UnresolvedAuthoredLinkFact{
		Resolution:    noteformat.LinkResolutionNoteReference,
		Syntax:        "wikilink",
		Subtype:       "basic",
		ResolverInput: "notes/target#fragment",
		RawRange:      noteformat.SourceRange{StartByte: 0, EndByte: 12},
		TargetRange:   noteformat.SourceRange{StartByte: 2, EndByte: 10},
		PathRange: noteformat.OptionalSourceRange{
			Present: true, Range: noteformat.SourceRange{StartByte: 2, EndByte: 6},
		},
		FragmentRange: noteformat.OptionalSourceRange{
			Present: true, Range: noteformat.SourceRange{StartByte: 7, EndByte: 10},
		},
	}
	if _, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, capabilities, noteformat.ProjectionFacts{Links: []noteformat.UnresolvedAuthoredLinkFact{validLink}}); err != nil {
		t.Fatalf("NewProjectionWithFacts valid link: %v", err)
	}
	emptyResolverInput := validLink
	emptyResolverInput.ResolverInput = ""
	if _, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, capabilities, noteformat.ProjectionFacts{Links: []noteformat.UnresolvedAuthoredLinkFact{emptyResolverInput}}); err != nil {
		t.Fatalf("NewProjectionWithFacts empty resolver input: %v", err)
	}

	tests := []struct {
		name  string
		facts noteformat.ProjectionFacts
	}{
		{
			name: "unknown resolution",
			facts: noteformat.ProjectionFacts{Links: []noteformat.UnresolvedAuthoredLinkFact{{
				Resolution: "unknown", Syntax: "wikilink", Subtype: "basic", RawRange: validLink.RawRange, TargetRange: validLink.TargetRange,
			}}},
		},
		{
			name: "target outside raw",
			facts: noteformat.ProjectionFacts{Links: []noteformat.UnresolvedAuthoredLinkFact{{
				Resolution: noteformat.LinkResolutionNoteReference, Syntax: "wikilink", Subtype: "basic",
				RawRange: noteformat.SourceRange{StartByte: 2, EndByte: 10}, TargetRange: noteformat.SourceRange{StartByte: 1, EndByte: 8},
			}}},
		},
		{
			name: "path outside target",
			facts: noteformat.ProjectionFacts{Links: []noteformat.UnresolvedAuthoredLinkFact{{
				Resolution: noteformat.LinkResolutionNoteReference, Syntax: "wikilink", Subtype: "basic",
				RawRange: validLink.RawRange, TargetRange: validLink.TargetRange,
				PathRange: noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: 1, EndByte: 4}},
			}}},
		},
		{
			name: "absent range carries bytes",
			facts: noteformat.ProjectionFacts{Tags: []noteformat.TagFact{{
				Value: "tag", Range: noteformat.OptionalSourceRange{Range: noteformat.SourceRange{StartByte: 1, EndByte: 2}},
			}}},
		},
		{
			name: "tags out of source order",
			facts: noteformat.ProjectionFacts{Tags: []noteformat.TagFact{
				{Value: "later", Range: noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: 8, EndByte: 9}}},
				{Value: "earlier", Range: noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: 2, EndByte: 3}}},
			}},
		},
		{
			name: "metadata value outside fact",
			facts: noteformat.ProjectionFacts{RootMetadata: []noteformat.RootMetadataFact{{
				Key: "title", Value: metadataValue(t, "value"),
				Range:      noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: 2, EndByte: 8}},
				ValueRange: noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: 1, EndByte: 4}},
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, capabilities, test.facts); err == nil {
				t.Fatal("NewProjectionWithFacts succeeded")
			}
		})
	}
}

func TestProjectionFactsValidateSearchRegionsAndCopyTheirText(t *testing.T) {
	capabilities := noteformat.MustCapabilities(noteformat.CapabilitySourceReading)
	facts := noteformat.ProjectionFacts{SearchRegions: []noteformat.SearchRegionFact{
		{Origin: noteformat.SearchRegionAuthored, Kind: noteformat.SearchRegionVisible, Text: "authored", MediaType: "text/markdown", Range: exactRegionRange(0, 8)},
		{Origin: noteformat.SearchRegionDerived, Kind: noteformat.SearchRegionSupplemental, Text: "derived", MediaType: "text/plain"},
	}}
	projection, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, capabilities, facts)
	if err != nil {
		t.Fatalf("NewProjectionWithFacts: %v", err)
	}
	projection.Facts.SearchRegions[0].Text = "changed"
	if got := facts.SearchRegions[0].Text; got != "authored" {
		t.Fatalf("facts changed through projection copy: %q", got)
	}

	for name, facts := range map[string]noteformat.ProjectionFacts{
		"unknown origin": {SearchRegions: []noteformat.SearchRegionFact{{Origin: "unknown", Kind: noteformat.SearchRegionVisible, Text: "text", MediaType: "text/plain"}}},
		"missing kind":   {SearchRegions: []noteformat.SearchRegionFact{{Origin: noteformat.SearchRegionDerived, Text: "text", MediaType: "text/plain"}}},
		"empty text":     {SearchRegions: []noteformat.SearchRegionFact{{Origin: noteformat.SearchRegionDerived, Kind: noteformat.SearchRegionSupplemental, MediaType: "text/plain"}}},
		"invalid media":  {SearchRegions: []noteformat.SearchRegionFact{{Origin: noteformat.SearchRegionDerived, Kind: noteformat.SearchRegionSupplemental, Text: "text", MediaType: "not a media type"}}},
		"authored absent range": {SearchRegions: []noteformat.SearchRegionFact{{
			Origin: noteformat.SearchRegionAuthored, Kind: noteformat.SearchRegionVisible, Text: "text", MediaType: "text/plain",
		}}},
		"out of order": {SearchRegions: []noteformat.SearchRegionFact{
			{Origin: noteformat.SearchRegionAuthored, Kind: noteformat.SearchRegionVisible, Text: "later", MediaType: "text/plain", Range: exactRegionRange(8, 10)},
			{Origin: noteformat.SearchRegionAuthored, Kind: noteformat.SearchRegionVisible, Text: "earlier", MediaType: "text/plain", Range: exactRegionRange(2, 4)},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, capabilities, facts); err == nil {
				t.Fatal("NewProjectionWithFacts succeeded")
			}
		})
	}
}

func TestProjectionFactsValidateAndCopyURIResolutionInput(t *testing.T) {
	capabilities := noteformat.MustCapabilities(noteformat.CapabilitySourceReading)
	uri := &noteformat.URIReferenceFact{
		Raw: "../report.html?view=wide#chart", Path: "../report.html", Query: "view=wide", Fragment: "chart",
		Encoding: noteformat.URIEncodingPercent, RawRange: noteformat.SourceRange{StartByte: 6, EndByte: 42},
		PathRange: exactRegionRange(6, 20), QueryRange: exactRegionRange(21, 30), FragmentRange: exactRegionRange(31, 36),
	}
	facts := noteformat.ProjectionFacts{Links: []noteformat.UnresolvedAuthoredLinkFact{{
		Resolution: noteformat.LinkResolutionURI, Syntax: "html", Subtype: "anchor", ResolverInput: uri.Raw,
		Target: uri.Raw, Path: uri.Path, Fragment: uri.Fragment, RawRange: noteformat.SourceRange{StartByte: 0, EndByte: 48},
		TargetRange: uri.RawRange, URI: uri,
	}}}
	projection, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, capabilities, facts)
	if err != nil {
		t.Fatalf("NewProjectionWithFacts: %v", err)
	}
	projection.Facts.Links[0].URI.Path = "changed"
	if facts.Links[0].URI.Path != "../report.html" {
		t.Fatal("URI fact was not copied")
	}

	bad := facts
	bad.Links[0].URI.Encoding = "double-decoded"
	if _, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, capabilities, bad); err == nil {
		t.Fatal("invalid URI encoding accepted")
	}
}

func TestProjectionDiagnosticsValidateClosedProvenance(t *testing.T) {
	capabilities := noteformat.MustCapabilities(noteformat.CapabilitySourceReading)
	valid := noteformat.Diagnostic{
		Code: "metadata_shape", Category: noteformat.DiagnosticCategoryMetadata, Message: "metadata is not an object",
		Range: exactRegionRange(1, 4), AffectedOperation: noteformat.DiagnosticOperationMetadataRead,
	}
	if _, err := noteformat.NewProjection("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, []noteformat.Diagnostic{valid}, capabilities); err != nil {
		t.Fatalf("valid diagnostic: %v", err)
	}
	invalid := valid
	invalid.Category = "unknown"
	if _, err := noteformat.NewProjection("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, []noteformat.Diagnostic{invalid}, capabilities); err == nil {
		t.Fatal("unknown diagnostic category accepted")
	}
}

func metadataValue(t *testing.T, value any) noteformat.MetadataValue {
	t.Helper()
	metadata, err := noteformat.NewMetadataValue(value)
	if err != nil {
		t.Fatalf("NewMetadataValue: %v", err)
	}
	return metadata
}

func exactRegionRange(start, end int) noteformat.OptionalSourceRange {
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: start, EndByte: end}}
}
