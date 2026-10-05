// Package markdown provides the projector for Rhizome's built-in Markdown
// note format.
package markdown

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Provider projects Markdown syntax using the shared Obsidian parsing
// primitives. It never reads files, resolves targets, or persists facts.
type Provider struct{}

var _ noteformat.Projector = Provider{}

// New returns the built-in Markdown provider descriptor.
func New() Provider {
	return Provider{}
}

// Descriptor returns the stable Markdown provider declaration.
func (Provider) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "markdown",
		Extensions:        []string{".md"},
		ProviderVersion:   "markdown-provider-v1",
		ProjectionVersion: "markdown-projection-v3",
		OwnershipPolicy:   noteformat.OwnershipDefault,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilitySearchableContentProjection,
			noteformat.CapabilityRootMetadataReading,
			noteformat.CapabilityAuthoredLinkExtraction,
			noteformat.CapabilityFragmentTargetExtraction,
			noteformat.CapabilityRootMetadataMutation,
			noteformat.CapabilityLinkRetargeting,
			noteformat.CapabilityFileRenameMove,
			noteformat.CapabilityStructuralContentMutation,
		),
	}
}

// Project produces unresolved authored facts from the sealed source snapshot.
func (p Provider) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	content := string(source.Bytes())
	facts, diagnostics := projectFacts(source.Path().String(), content)
	descriptor := p.Descriptor()
	projection, err := noteformat.NewProjectionWithFacts(
		descriptor.ProviderVersion,
		descriptor.ProjectionVersion,
		noteformat.ProjectionStatusCurrent,
		diagnostics,
		descriptor.Capabilities,
		facts,
	)
	if err != nil {
		return noteformat.Projection{}, fmt.Errorf("project Markdown syntax: %w", err)
	}
	return projection, nil
}

func projectFacts(notePath, content string) (noteformat.ProjectionFacts, []noteformat.Diagnostic) {
	facts := noteformat.ProjectionFacts{}
	frontmatter, frontmatterErr := obsidian.ExtractFrontmatter(content)
	diagnostics := make([]noteformat.Diagnostic, 0, 1)
	if frontmatterErr != nil {
		diagnostics = append(diagnostics, noteformat.Diagnostic{
			Code:    "markdown_frontmatter_invalid",
			Message: frontmatterErr.Error(),
		})
		frontmatter = nil
	}

	facts.Title = titleFact(notePath, content, frontmatter)
	facts.RootMetadata, diagnostics = appendRootMetadata(frontmatterWithTemporalProvenance(content, frontmatter), diagnostics)
	facts.Aliases = aliasFacts(frontmatter)
	facts.Tags = tagFacts(content, frontmatter)
	facts.InlineProperties = inlinePropertyFacts(content)
	facts.Links = linkFacts(content)
	facts.FragmentTargets = fragmentTargetFacts(content)
	facts.SearchRegions = searchRegions(content)
	return facts, diagnostics
}

func titleFact(notePath, content string, frontmatter map[string]any) *noteformat.TitleFact {
	for _, key := range []string{"title", "name"} {
		if value, ok := frontmatter[key].(string); ok && strings.TrimSpace(value) != "" {
			return &noteformat.TitleFact{Value: strings.TrimSpace(value)}
		}
	}
	if title := obsidian.SingleMarkdownH1Title(content); title != "" {
		return &noteformat.TitleFact{Value: title}
	}
	base := path.Base(notePath)
	return &noteformat.TitleFact{Value: strings.TrimSuffix(base, path.Ext(base))}
}

func appendRootMetadata(frontmatter map[string]any, diagnostics []noteformat.Diagnostic) ([]noteformat.RootMetadataFact, []noteformat.Diagnostic) {
	keys := make([]string, 0, len(frontmatter))
	for key := range frontmatter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	facts := make([]noteformat.RootMetadataFact, 0, len(keys))
	for _, key := range keys {
		value, err := noteformat.NewMetadataValue(frontmatter[key])
		if err != nil {
			diagnostics = append(diagnostics, noteformat.Diagnostic{
				Code:    "markdown_metadata_unsupported",
				Message: fmt.Sprintf("frontmatter %q: %v", key, err),
			})
			continue
		}
		facts = append(facts, noteformat.RootMetadataFact{Key: key, Value: value})
	}
	return facts, diagnostics
}

func aliasFacts(frontmatter map[string]any) []noteformat.AliasFact {
	aliases := obsidian.AliasListFromFrontmatter(frontmatter)
	facts := make([]noteformat.AliasFact, 0, len(aliases))
	for _, alias := range aliases {
		facts = append(facts, noteformat.AliasFact{Value: alias})
	}
	return facts
}

func tagFacts(content string, frontmatter map[string]any) []noteformat.TagFact {
	values := make([]string, 0)
	if raw, ok := frontmatter["tags"]; ok {
		switch tags := raw.(type) {
		case []string:
			values = append(values, tags...)
		case []any:
			for _, tag := range tags {
				if text, ok := tag.(string); ok {
					values = append(values, text)
				}
			}
		}
	}
	for _, tag := range obsidian.ExtractHashtags(content) {
		values = append(values, strings.TrimPrefix(tag, "#"))
	}
	seen := make(map[string]struct{}, len(values))
	facts := make([]noteformat.TagFact, 0, len(values))
	for _, value := range values {
		value = strings.TrimPrefix(strings.TrimSpace(value), "#")
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		facts = append(facts, noteformat.TagFact{Value: value})
	}
	return facts
}

func inlinePropertyFacts(content string) []noteformat.InlinePropertyFact {
	properties := obsidian.ExtractInlinePropertyOccurrences(content)
	facts := make([]noteformat.InlinePropertyFact, 0, len(properties))
	for _, property := range properties {
		facts = append(facts, noteformat.InlinePropertyFact{Key: property.Key, Value: property.Value})
	}
	return facts
}

func linkFacts(content string) []noteformat.UnresolvedAuthoredLinkFact {
	snapshot := obsidian.ScanStructuredLinkSnapshot(content)
	if snapshot.Validate() != nil {
		return nil
	}
	facts := make([]noteformat.UnresolvedAuthoredLinkFact, 0, len(snapshot.Links))
	for _, link := range snapshot.Links {
		resolution := noteformat.LinkResolutionRelativePath
		if link.Kind == obsidian.StructuredLinkWikilink {
			resolution = noteformat.LinkResolutionNoteReference
		}
		facts = append(facts, noteformat.UnresolvedAuthoredLinkFact{
			Resolution:    resolution,
			Syntax:        noteformat.AuthoredLinkSyntax(link.Kind),
			Subtype:       noteformat.AuthoredLinkSubtype(linkSubtype(link)),
			ResolverInput: link.ResolverInput(),
			Embed:         link.Embed,
			Target:        link.Target,
			Path:          link.Path,
			Fragment:      link.Fragment,
			Display:       link.Display,
			RawRange:      sourceRange(link.RawSpan),
			TargetRange:   sourceRange(link.TargetSpan),
			PathRange:     optionalRange(link.PathSpan),
			FragmentRange: optionalRange(link.FragmentSpan),
			DisplayRange:  optionalRange(link.DisplaySpan),
		})
	}
	return facts
}

func searchRegions(content string) []noteformat.SearchRegionFact {
	if content == "" {
		return nil
	}
	return []noteformat.SearchRegionFact{{
		Origin:    noteformat.SearchRegionAuthored,
		Kind:      noteformat.SearchRegionVisible,
		Text:      content,
		MediaType: "text/markdown",
		Range:     exactRange(0, len(content)),
	}}
}

func linkSubtype(link obsidian.StructuredLink) string {
	if link.Embed {
		return "embed"
	}
	if link.Kind == obsidian.StructuredLinkWikilink {
		switch {
		case link.Display != "":
			return "alias"
		case strings.HasPrefix(link.Fragment, "^"):
			return "block"
		case link.Fragment != "":
			return "heading"
		default:
			return "basic"
		}
	}
	if link.Fragment != "" {
		return "heading"
	}
	return "basic"
}

func fragmentTargetFacts(content string) []noteformat.FragmentTargetFact {
	targets := obsidian.EnumerateMarkdownTargets(content)
	facts := make([]noteformat.FragmentTargetFact, 0, len(targets))
	for _, target := range targets {
		facts = append(facts, noteformat.FragmentTargetFact{
			Kind:           noteformat.FragmentTargetKind(target.Kind),
			Text:           target.Text,
			NormalizedText: target.NormalizedText,
			Ordinal:        target.Ordinal,
			Level:          target.Level,
			Line:           target.Line,
			Range:          exactRange(target.StartByte, target.EndByte),
		})
	}
	return facts
}

func exactRange(start, end int) noteformat.OptionalSourceRange {
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: start, EndByte: end}}
}

func optionalRange(span obsidian.StructuredLinkSpan) noteformat.OptionalSourceRange {
	if !span.Valid() {
		return noteformat.OptionalSourceRange{}
	}
	return exactRange(span.Start, span.End)
}

func sourceRange(span obsidian.StructuredLinkSpan) noteformat.SourceRange {
	return noteformat.SourceRange{StartByte: span.Start, EndByte: span.End}
}
