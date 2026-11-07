package noteformat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func structureFixture(t *testing.T) *StructureFacts {
	t.Helper()
	scalar := func(v any) *MetadataValue { m, err := NewMetadataValue(v); require.NoError(t, err); return &m }
	return &StructureFacts{Root: "root", Owners: []OwnerFact{
		{Key: "root", Shape: OwnerDocument, Range: OptionalSourceRange{Present: true, Range: SourceRange{0, 8}}, Value: 1},
		{Key: "row", Parent: "root", Collection: "/rows", Order: 1, Shape: OwnerMapping, Address: "/rows/0", Range: OptionalSourceRange{Present: true, Range: SourceRange{1, 7}}, Value: 2, Properties: []PropertyOccurrenceFact{{Key: "name", Source: "property", Value: 3}}, Markdown: &MarkdownOwnerFact{ItemTitle: &TitleFact{Value: "title"}, Markers: []TagFact{{Value: "tag"}}, Checkbox: &CheckboxFact{TokenRange: SourceRange{1, 2}}}},
	}, Values: []SourceValueFact{
		{Kind: MetadataArray, Items: []ValueID{2}},
		{Kind: MetadataObject, Entries: []SourceEntryFact{{Key: *scalar("name"), Value: 3}}},
		{Kind: MetadataString, Scalar: scalar("")},
		{Kind: MetadataNull, Scalar: scalar(nil)},
		{Kind: MetadataArray},
		{Kind: MetadataObject},
	}}
}

func TestStructuralEnvelopeValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*StructureFacts)
		want   string
	}{
		{"empty key", func(s *StructureFacts) { s.Owners[1].Key = "" }, "empty owner"},
		{"duplicate key", func(s *StructureFacts) { s.Owners[1].Key = "root" }, "duplicate owner"},
		{"missing root", func(s *StructureFacts) { s.Root = "absent" }, "root"},
		{"second root", func(s *StructureFacts) { s.Owners[1].Parent = "" }, "missing parent"},
		{"missing parent", func(s *StructureFacts) { s.Owners[1].Parent = "gone" }, "missing parent"},
		{"owner cycle", func(s *StructureFacts) { s.Owners[1].Parent = "row" }, "unreachable"},
		{"unknown shape", func(s *StructureFacts) { s.Owners[1].Shape = "yaml" }, "unknown owner"},
		{"negative ordinal", func(s *StructureFacts) { s.Owners[1].Ordinal = -1 }, "order/ordinal"},
		{"duplicate order", func(s *StructureFacts) { s.Owners[1].Order = 0 }, "order/ordinal"},
		{"range outside bytes", func(s *StructureFacts) { s.Owners[1].Range.Range.EndByte = 9 }, "range"},
		{"absent range payload", func(s *StructureFacts) { s.Owners[1].Range.Present = false }, "absent range"},
		{"child outside parent", func(s *StructureFacts) { s.Owners[0].Range.Range.EndByte = 5 }, "outside parent"},
		{"own range outside owner", func(s *StructureFacts) { s.Owners[1].OwnRanges = []SourceRange{{0, 1}} }, "own ranges"},
		{"own range overlaps child", func(s *StructureFacts) { s.Owners[0].OwnRanges = []SourceRange{{0, 8}} }, "overlaps child"},
		{"own ranges overlap", func(s *StructureFacts) { s.Owners[1].OwnRanges = []SourceRange{{1, 4}, {3, 5}} }, "own ranges"},
		{"dangling owner value", func(s *StructureFacts) { s.Owners[1].Value = 99 }, "owner value"},
		{"dangling property value", func(s *StructureFacts) { s.Owners[1].Properties[0].Value = 99 }, "invalid property"},
		{"property outside owner", func(s *StructureFacts) {
			s.Owners[1].Properties[0].Range = OptionalSourceRange{Present: true, Range: SourceRange{0, 1}}
		}, "outside owner"},
		{"dangling value", func(s *StructureFacts) { s.Values[0].Items = []ValueID{99} }, "dangling value"},
		{"value cycle", func(s *StructureFacts) { s.Values[0].Items = []ValueID{1} }, "cycle"},
		{"shared child value", func(s *StructureFacts) { s.Values[0].Items = []ValueID{2, 2} }, "multiple container"},
		{"scalar on array", func(s *StructureFacts) { s.Values[0].Scalar = s.Values[2].Scalar }, "incompatible payload"},
		{"wrong scalar kind", func(s *StructureFacts) { s.Values[2].Kind = MetadataInteger }, "scalar payload"},
		{"missing null payload", func(s *StructureFacts) { s.Values[3].Scalar = nil }, "scalar payload"},
		{"duplicate mapping key", func(s *StructureFacts) { s.Values[1].Entries = append(s.Values[1].Entries, s.Values[1].Entries[0]) }, "duplicate mapping"},
	}
	require.NoError(t, ValidateStructureFacts(structureFixture(t), 8))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := structureFixture(t)
			tt.change(s)
			require.ErrorContains(t, ValidateStructureFacts(s, 8), tt.want)
		})
	}
	for _, kind := range []string{"ordinal", "overlap"} {
		t.Run("siblings "+kind, func(t *testing.T) {
			s := structureFixture(t)
			o := s.Owners[1]
			o.Key = "other"
			o.Order = 2
			o.Value = 0
			if kind == "overlap" {
				o.Ordinal = 1
			}
			s.Owners = append(s.Owners, o)
			require.Error(t, ValidateStructureFacts(s, 8))
		})
	}
}

func TestStructureCopyAndValuePresence(t *testing.T) {
	original := structureFixture(t)
	copy, err := NewStructureFacts(*original, 8)
	require.NoError(t, err)
	copy.Owners[1].Properties[0].Key = "changed"
	copy.Owners[1].Markdown.ItemTitle.Value = "changed"
	copy.Owners[1].Markdown.Checkbox.Checked = true
	copy.Owners[1].Markdown.Markers[0].Value = "changed"
	copy.Values[0].Items[0] = 4
	require.Equal(t, "name", original.Owners[1].Properties[0].Key)
	require.Equal(t, "title", original.Owners[1].Markdown.ItemTitle.Value)
	require.False(t, original.Owners[1].Markdown.Checkbox.Checked)
	require.Equal(t, "tag", original.Owners[1].Markdown.Markers[0].Value)
	require.Equal(t, ValueID(2), original.Values[0].Items[0])
	encoded, err := json.Marshal(original.Values[3:])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"kind":"null","scalar":null`)
	require.Contains(t, string(encoded), `"kind":"array"`)
	require.Contains(t, string(encoded), `"kind":"object"`)
	require.NotEqual(t, ValueID(0), original.Owners[1].Properties[0].Value)
}

type syntheticStructuralProvider struct {
	facts      ProjectionFacts
	structural bool
}

func (p syntheticStructuralProvider) Descriptor() Descriptor {
	caps := MustCapabilities(CapabilitySourceReading)
	if p.structural {
		caps = MustCapabilities(CapabilitySourceReading, CapabilityStructuralReading)
	}
	return Descriptor{ID: "synthetic", OwnershipPolicy: OwnershipDefault, Extensions: []string{".synthetic"}, ProviderVersion: "1", ProjectionVersion: "1", Capabilities: caps}
}
func (p syntheticStructuralProvider) Project(AuthoredSource) (Projection, error) {
	d := p.Descriptor()
	return NewProjectionWithFacts("1", "1", ProjectionStatusCurrent, nil, d.Capabilities, p.facts)
}

func TestSyntheticStructuralRuntimeWithoutMutations(t *testing.T) {
	p := syntheticStructuralProvider{facts: ProjectionFacts{Structure: structureFixture(t)}, structural: true}
	registry, err := NewRegistry(p)
	require.NoError(t, err)
	_, err = NewRuntime(registry)
	require.ErrorContains(t, err, "executable projector")
	runtime, err := NewRuntime(registry, p)
	require.NoError(t, err)
	require.True(t, runtime.CanReadStructure("synthetic"))
	require.False(t, runtime.CanPatchRootMetadata("synthetic"))
	source, err := NewAuthoredSource("host.synthetic", p.Descriptor(), []byte("!native!"), 0)
	require.NoError(t, err)
	projected, err := runtime.Project(source)
	require.NoError(t, err)
	require.Len(t, projected.Facts.Structure.Owners, 2)
	for _, cap := range projected.Capabilities.Values() {
		require.False(t, strings.Contains(string(cap), "mutation"))
	}
	p.facts.Structure = nil
	runtime, err = NewRuntime(registry, p)
	require.NoError(t, err)
	_, err = runtime.Project(source)
	require.ErrorContains(t, err, "lacks root")
	p.structural = false
	p.facts.Structure = structureFixture(t)
	registry, err = NewRegistry(p)
	require.NoError(t, err)
	runtime, err = NewRuntime(registry, p)
	require.NoError(t, err)
	source, err = NewAuthoredSource("host.synthetic", p.Descriptor(), []byte("!native!"), 0)
	require.NoError(t, err)
	_, err = runtime.Project(source)
	require.ErrorContains(t, err, "without structural_reading")
	for _, field := range []string{"link", "region"} {
		facts := ProjectionFacts{Structure: structureFixture(t)}
		if field == "link" {
			facts.Links = []UnresolvedAuthoredLinkFact{{Owner: "missing"}}
		} else {
			facts.SearchRegions = []SearchRegionFact{{Owner: "missing"}}
		}
		require.ErrorContains(t, validateProjectionFacts(facts, 8), "dangling owner")
	}
}
