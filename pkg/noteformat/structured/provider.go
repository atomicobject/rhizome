package structured

import (
	"path"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

// Provider reads explicitly selected structured sources. It has no write engine.
type Provider struct {
	descriptor noteformat.Descriptor
	parse      func([]byte) (*Value, []Diagnostic)
}

func NewYAML() Provider {
	return Provider{descriptor("yaml", []string{".yaml", ".yml"}, "yaml-projection-v1-"+ScalarProfile), ParseYAML}
}

func NewJSON() Provider {
	return Provider{descriptor("json", []string{".json"}, "json-projection-v1"), ParseJSON}
}

func descriptor(id noteformat.FormatID, extensions []string, projection string) noteformat.Descriptor {
	return noteformat.Descriptor{ID: id, Extensions: extensions, ProviderVersion: string(id) + "-provider-v1", ProjectionVersion: projection,
		OwnershipPolicy: noteformat.OwnershipExplicitInclude,
		Capabilities:    noteformat.MustCapabilities(noteformat.CapabilitySourceReading, noteformat.CapabilityStructuralReading)}
}

func (p Provider) Descriptor() noteformat.Descriptor {
	d := p.descriptor
	d.Extensions = slices.Clone(d.Extensions)
	return d
}

func (p Provider) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	value, issues := p.parse(source.Bytes())
	d := p.Descriptor()
	if len(issues) > 0 {
		diagnostics := make([]noteformat.Diagnostic, 0, len(issues))
		for _, issue := range issues {
			diagnostics = append(diagnostics, noteformat.Diagnostic{Code: issue.Code, Message: source.Path().String() + ": " + issue.Message,
				Category: noteformat.DiagnosticCategorySource, Blocking: true, AffectedOperation: noteformat.DiagnosticOperationProjection})
		}
		return noteformat.NewProjection(d.ProviderVersion, d.ProjectionVersion, noteformat.ProjectionStatusFatal, diagnostics, d.Capabilities)
	}
	facts, err := structureFacts(value)
	if err != nil {
		return noteformat.Projection{}, err
	}
	title := &noteformat.TitleFact{Value: strings.TrimSuffix(path.Base(source.Path().String()), path.Ext(source.Path().String()))}
	facts.Owners[0].Title = title
	return noteformat.NewProjectionWithFacts(d.ProviderVersion, d.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, d.Capabilities,
		noteformat.ProjectionFacts{Title: title, Structure: facts})
}
