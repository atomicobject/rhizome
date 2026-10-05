package noteformat

import (
	"fmt"
	"strings"
)

// SourceRange is a half-open byte range in an AuthoredSource. Ranges always
// address original authored bytes rather than a normalized representation.
type SourceRange struct {
	StartByte int
	EndByte   int
}

// OptionalSourceRange represents a fact component that may be derived or lack
// an exact authored span. An absent range must contain a zero SourceRange.
type OptionalSourceRange struct {
	Present bool
	Range   SourceRange
}

// TitleFact is a title candidate and, when available, its authored span.
type TitleFact struct {
	Value string
	Range OptionalSourceRange
}

// RootMetadataFact is an unresolved root metadata occurrence.
type RootMetadataFact struct {
	Key        string
	Value      MetadataValue
	Range      OptionalSourceRange
	KeyRange   OptionalSourceRange
	ValueRange OptionalSourceRange
}

// InlinePropertyFact is an unresolved inline property occurrence.
type InlinePropertyFact struct {
	Key        string
	Value      string
	Range      OptionalSourceRange
	KeyRange   OptionalSourceRange
	ValueRange OptionalSourceRange
}

// TagFact is canonical unique tag evidence for the current projection contract.
// It is not a record of every authored tag occurrence or original spelling.
type TagFact struct {
	Value string
	Range OptionalSourceRange
}

// AliasFact is an authored alias occurrence before shared candidate matching.
type AliasFact struct {
	Value string
	Range OptionalSourceRange
}

// LinkResolution is the closed shared semantic used by resolution consumers.
type LinkResolution string

const (
	LinkResolutionNoteReference LinkResolution = "note_reference"
	LinkResolutionRelativePath  LinkResolution = "relative_path"
	LinkResolutionURI           LinkResolution = "uri"
)

type URIEncoding string

const URIEncodingPercent URIEncoding = "percent"

// URIReferenceFact is a provider-parsed URI reference. Components are decoded
// once; ranges address their exact authored spelling.
type URIReferenceFact struct {
	Raw              string
	Scheme           string
	Authority        string
	Path             string
	Query            string
	Fragment         string
	Encoding         URIEncoding
	ProtocolRelative bool
	RawRange         SourceRange
	PathRange        OptionalSourceRange
	QueryRange       OptionalSourceRange
	FragmentRange    OptionalSourceRange
}

// DocumentBaseFact is the first usable provider-authored document base.
type DocumentBaseFact struct {
	URI URIReferenceFact
}

// AuthoredLinkSyntax and AuthoredLinkSubtype preserve provider-specific source
// provenance without asking consumers to branch on a format ID.
type AuthoredLinkSyntax string
type AuthoredLinkSubtype string

// UnresolvedAuthoredLinkFact is an authored link before vault-aware candidate
// matching, filtering, or graph-edge policy. Target, Path, Fragment, and
// Display are decoded semantic values; their authored spelling is recoverable
// through the sealed component ranges. Resolution tells shared consumers which
// resolver semantics to apply; shared broad graph family derives from this
// closed semantic, never Syntax or FormatID. ResolverInput is the provider's
// exact input to that shared resolver. Syntax and Subtype retain provenance.
type UnresolvedAuthoredLinkFact struct {
	Resolution    LinkResolution
	Syntax        AuthoredLinkSyntax
	Subtype       AuthoredLinkSubtype
	ResolverInput string
	Embed         bool
	Target        string
	Path          string
	Fragment      string
	Display       string
	URI           *URIReferenceFact

	RawRange      SourceRange
	TargetRange   SourceRange
	PathRange     OptionalSourceRange
	FragmentRange OptionalSourceRange
	DisplayRange  OptionalSourceRange
}

// FragmentTargetKind identifies a provider-authored fragment target.
type FragmentTargetKind string

const (
	FragmentTargetHeading    FragmentTargetKind = "heading"
	FragmentTargetBlock      FragmentTargetKind = "block"
	FragmentTargetElementID  FragmentTargetKind = "element_id"
	FragmentTargetLegacyName FragmentTargetKind = "legacy_name"
)

// FragmentTargetFact is one unresolved destination target in source order.
type FragmentTargetFact struct {
	Kind           FragmentTargetKind
	Text           string
	NormalizedText string
	Ordinal        int
	Level          int
	Line           int
	Range          OptionalSourceRange
}

// ProjectionFacts is the format-neutral syntax result of a successful
// projection. It contains no filesystem, persistence, candidate, or policy.
type ProjectionFacts struct {
	DocumentBase     *DocumentBaseFact
	Title            *TitleFact
	RootMetadata     []RootMetadataFact
	InlineProperties []InlinePropertyFact
	Tags             []TagFact
	Aliases          []AliasFact
	Links            []UnresolvedAuthoredLinkFact
	FragmentTargets  []FragmentTargetFact
	SearchRegions    []SearchRegionFact
}

func (f ProjectionFacts) clone() ProjectionFacts {
	out := ProjectionFacts{
		RootMetadata:     append([]RootMetadataFact(nil), f.RootMetadata...),
		InlineProperties: append([]InlinePropertyFact(nil), f.InlineProperties...),
		Tags:             append([]TagFact(nil), f.Tags...),
		Aliases:          append([]AliasFact(nil), f.Aliases...),
		Links:            append([]UnresolvedAuthoredLinkFact(nil), f.Links...),
		FragmentTargets:  append([]FragmentTargetFact(nil), f.FragmentTargets...),
		SearchRegions:    append([]SearchRegionFact(nil), f.SearchRegions...),
	}
	if f.DocumentBase != nil {
		base := *f.DocumentBase
		out.DocumentBase = &base
	}
	if f.Title != nil {
		title := *f.Title
		out.Title = &title
	}
	for index := range out.RootMetadata {
		out.RootMetadata[index].Value = out.RootMetadata[index].Value.clone()
	}
	for index := range out.Links {
		if f.Links[index].URI != nil {
			uri := *f.Links[index].URI
			out.Links[index].URI = &uri
		}
	}
	return out
}

func (f ProjectionFacts) empty() bool {
	return f.Title == nil && f.DocumentBase == nil && len(f.RootMetadata) == 0 && len(f.InlineProperties) == 0 &&
		len(f.Tags) == 0 && len(f.Aliases) == 0 && len(f.Links) == 0 && len(f.FragmentTargets) == 0 && len(f.SearchRegions) == 0
}

func validateProjectionFacts(facts ProjectionFacts, sourceLength int) error {
	validateRange := func(name string, sourceRange SourceRange) error {
		if sourceRange.StartByte < 0 || sourceRange.EndByte < sourceRange.StartByte {
			return fmt.Errorf("%s has invalid source range [%d,%d)", name, sourceRange.StartByte, sourceRange.EndByte)
		}
		if sourceLength >= 0 && sourceRange.EndByte > sourceLength {
			return fmt.Errorf("%s source range ends at %d beyond source length %d", name, sourceRange.EndByte, sourceLength)
		}
		return nil
	}
	validateOptionalRange := func(name string, sourceRange OptionalSourceRange) error {
		if !sourceRange.Present {
			if sourceRange.Range != (SourceRange{}) {
				return fmt.Errorf("%s is absent with a non-zero source range", name)
			}
			return nil
		}
		return validateRange(name, sourceRange.Range)
	}
	contains := func(parent, child SourceRange) bool {
		return child.StartByte >= parent.StartByte && child.EndByte <= parent.EndByte
	}
	if facts.Title != nil {
		if err := validateOptionalRange("title", facts.Title.Range); err != nil {
			return err
		}
	}
	if facts.DocumentBase != nil {
		if err := validateURIReference("document base", facts.DocumentBase.URI, validateRange, validateOptionalRange, contains); err != nil {
			return err
		}
	}
	if err := validateFactOccurrences("root metadata", facts.RootMetadata, func(fact RootMetadataFact) OptionalSourceRange {
		return fact.Range
	}, validateOptionalRange); err != nil {
		return err
	}
	for _, fact := range facts.RootMetadata {
		if strings.TrimSpace(fact.Key) == "" {
			return fmt.Errorf("root metadata has an empty key")
		}
		if !fact.Value.valid() {
			return fmt.Errorf("root metadata %q has an invalid value", fact.Key)
		}
		if err := validateContainedFactComponents("root metadata", fact.Range, fact.KeyRange, fact.ValueRange, validateOptionalRange, contains); err != nil {
			return err
		}
	}
	if err := validateFactOccurrences("inline property", facts.InlineProperties, func(fact InlinePropertyFact) OptionalSourceRange {
		return fact.Range
	}, validateOptionalRange); err != nil {
		return err
	}
	for _, fact := range facts.InlineProperties {
		if strings.TrimSpace(fact.Key) == "" {
			return fmt.Errorf("inline property has an empty key")
		}
		if err := validateContainedFactComponents("inline property", fact.Range, fact.KeyRange, fact.ValueRange, validateOptionalRange, contains); err != nil {
			return err
		}
	}
	if err := validateFactOccurrences("tag", facts.Tags, func(fact TagFact) OptionalSourceRange { return fact.Range }, validateOptionalRange); err != nil {
		return err
	}
	for _, fact := range facts.Tags {
		if strings.TrimSpace(fact.Value) == "" {
			return fmt.Errorf("tag has an empty value")
		}
	}
	if err := validateFactOccurrences("alias", facts.Aliases, func(fact AliasFact) OptionalSourceRange { return fact.Range }, validateOptionalRange); err != nil {
		return err
	}
	for _, fact := range facts.Aliases {
		if strings.TrimSpace(fact.Value) == "" {
			return fmt.Errorf("alias has an empty value")
		}
	}
	lastLinkStart := -1
	for _, fact := range facts.Links {
		if fact.Resolution != LinkResolutionNoteReference && fact.Resolution != LinkResolutionRelativePath && fact.Resolution != LinkResolutionURI {
			return fmt.Errorf("authored link has invalid resolution %q", fact.Resolution)
		}
		if strings.TrimSpace(string(fact.Syntax)) == "" || strings.TrimSpace(string(fact.Subtype)) == "" {
			return fmt.Errorf("authored link requires syntax and subtype provenance")
		}
		if err := validateRange("authored link raw", fact.RawRange); err != nil {
			return err
		}
		if err := validateRange("authored link target", fact.TargetRange); err != nil {
			return err
		}
		if !contains(fact.RawRange, fact.TargetRange) {
			return fmt.Errorf("authored link target range is outside its raw range")
		}
		if fact.Resolution == LinkResolutionURI {
			if fact.URI == nil {
				return fmt.Errorf("URI link requires parsed URI input")
			}
			if err := validateURIReference("authored link URI", *fact.URI, validateRange, validateOptionalRange, contains); err != nil {
				return err
			}
		} else if fact.URI != nil {
			return fmt.Errorf("non-URI link must not contain parsed URI input")
		}
		for _, component := range []struct {
			name   string
			range_ OptionalSourceRange
			parent SourceRange
		}{
			{"authored link path", fact.PathRange, fact.TargetRange},
			{"authored link fragment", fact.FragmentRange, fact.TargetRange},
			{"authored link display", fact.DisplayRange, fact.RawRange},
		} {
			if err := validateOptionalRange(component.name, component.range_); err != nil {
				return err
			}
			if component.range_.Present && !contains(component.parent, component.range_.Range) {
				return fmt.Errorf("%s is outside its containing link component", component.name)
			}
		}
		if fact.RawRange.StartByte < lastLinkStart {
			return fmt.Errorf("authored links are not in source order")
		}
		lastLinkStart = fact.RawRange.StartByte
	}
	if err := validateFactOccurrences("fragment target", facts.FragmentTargets, func(fact FragmentTargetFact) OptionalSourceRange {
		return fact.Range
	}, validateOptionalRange); err != nil {
		return err
	}
	for _, fact := range facts.FragmentTargets {
		if strings.TrimSpace(string(fact.Kind)) == "" {
			return fmt.Errorf("fragment target has an empty kind")
		}
	}
	if err := validateSearchRegions(facts.SearchRegions, validateOptionalRange); err != nil {
		return err
	}
	return nil
}

func validateURIReference(name string, ref URIReferenceFact, validateRange func(string, SourceRange) error, validateOptionalRange func(string, OptionalSourceRange) error, contains func(SourceRange, SourceRange) bool) error {
	if ref.Encoding != URIEncodingPercent {
		return fmt.Errorf("%s has invalid encoding %q", name, ref.Encoding)
	}
	if err := validateRange(name+" raw", ref.RawRange); err != nil {
		return err
	}
	for component, sourceRange := range map[string]OptionalSourceRange{
		"path": ref.PathRange, "query": ref.QueryRange, "fragment": ref.FragmentRange,
	} {
		if err := validateOptionalRange(name+" "+component, sourceRange); err != nil {
			return err
		}
		if sourceRange.Present && !contains(ref.RawRange, sourceRange.Range) {
			return fmt.Errorf("%s %s range is outside raw range", name, component)
		}
	}
	return nil
}

func validateFactOccurrences[T any](name string, facts []T, rangeFor func(T) OptionalSourceRange, validate func(string, OptionalSourceRange) error) error {
	lastStart := -1
	for _, fact := range facts {
		sourceRange := rangeFor(fact)
		if err := validate(name, sourceRange); err != nil {
			return err
		}
		if sourceRange.Present {
			if sourceRange.Range.StartByte < lastStart {
				return fmt.Errorf("%s facts are not in source order", name)
			}
			lastStart = sourceRange.Range.StartByte
		}
	}
	return nil
}

func validateContainedFactComponents(name string, raw OptionalSourceRange, key OptionalSourceRange, value OptionalSourceRange, validate func(string, OptionalSourceRange) error, contains func(SourceRange, SourceRange) bool) error {
	for _, component := range []struct {
		name   string
		range_ OptionalSourceRange
	}{
		{name + " key", key},
		{name + " value", value},
	} {
		if err := validate(component.name, component.range_); err != nil {
			return err
		}
		if component.range_.Present && (!raw.Present || !contains(raw.Range, component.range_.Range)) {
			return fmt.Errorf("%s is outside its fact range", component.name)
		}
	}
	return nil
}
