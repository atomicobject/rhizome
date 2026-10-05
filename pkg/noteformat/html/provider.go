// Package html provides the static projector for Rhizome's built-in HTML note format.
package html

import (
	"fmt"
	"unicode/utf8"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

// Provider projects authored HTML without executing it or reading any other
// source. It is safe for concurrent use because all parsing state is local to
// one Project call.
type Provider struct{}

var _ noteformat.Projector = Provider{}

// New returns the built-in HTML provider descriptor.
func New() Provider {
	return Provider{}
}

// Descriptor returns the stable HTML provider declaration.
func (Provider) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "html",
		Extensions:        []string{".html", ".htm"},
		ProviderVersion:   "html-provider-v2",
		ProjectionVersion: "html-projection-v3",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilitySearchableContentProjection,
			noteformat.CapabilityRootMetadataReading,
			noteformat.CapabilityRootMetadataMutation,
			noteformat.CapabilityActiveContentViewing,
			noteformat.CapabilityAuthoredLinkExtraction,
			noteformat.CapabilityFragmentTargetExtraction,
		),
	}
}

// Project extracts deterministic note facts from source bytes. Invalid UTF-8
// is fatal because replacing bytes would make source ranges unverifiable.
func (Provider) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	descriptor := Provider{}.Descriptor()
	content := source.Bytes()
	if !utf8.Valid(content) {
		projection, err := noteformat.NewProjectionWithFacts(
			descriptor.ProviderVersion,
			descriptor.ProjectionVersion,
			noteformat.ProjectionStatusFatal,
			[]noteformat.Diagnostic{{
				Code:              "html_invalid_utf8",
				Category:          noteformat.DiagnosticCategorySource,
				Message:           "HTML source is not valid UTF-8",
				Blocking:          true,
				AffectedOperation: noteformat.DiagnosticOperationProjection,
			}},
			descriptor.Capabilities,
			noteformat.ProjectionFacts{},
		)
		if err != nil {
			return noteformat.Projection{}, fmt.Errorf("project HTML: %w", err)
		}
		return projection, nil
	}

	parsed := parse(content)
	facts, diagnostics := projectFacts(source.Path().String(), content, parsed)
	projection, err := noteformat.NewProjectionWithFacts(
		descriptor.ProviderVersion,
		descriptor.ProjectionVersion,
		noteformat.ProjectionStatusCurrent,
		diagnostics,
		descriptor.Capabilities,
		facts,
	)
	if err != nil {
		return noteformat.Projection{}, fmt.Errorf("project HTML syntax: %w", err)
	}
	return projection, nil
}

// ViewerBootstrapOffset proves the ephemeral viewer injection boundary using
// the provider's source-aware parser.
func (Provider) ViewerBootstrapOffset(source noteformat.AuthoredSource) (int, error) {
	return ViewerBootstrapOffset(source.Bytes())
}
