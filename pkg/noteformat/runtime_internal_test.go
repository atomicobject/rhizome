package noteformat

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestRuntimeRejectsCorruptedSourceBeforeCallingProjector(t *testing.T) {
	descriptor := Descriptor{
		ID:                "markdown",
		Extensions:        []string{".md"},
		ProviderVersion:   "provider-v1",
		ProjectionVersion: "projection-v1",
		OwnershipPolicy:   OwnershipDefault,
		Capabilities:      MustCapabilities(CapabilitySourceReading),
	}
	registry, err := NewRegistry(internalProvider{descriptor: descriptor})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	projector := &internalProjector{descriptor: descriptor}
	runtime, err := NewRuntime(registry, projector)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	source, err := NewAuthoredSource(paths.NormalizeNotePath("notes/source.md"), descriptor, []byte("source"), 42)
	if err != nil {
		t.Fatalf("NewAuthoredSource: %v", err)
	}

	corruptions := map[string]func(*AuthoredSource){
		"non-canonical path":   func(source *AuthoredSource) { source.path = "./notes/source.md" },
		"non-canonical format": func(source *AuthoredSource) { source.format = "MARKDOWN" },
		"wrong hash":           func(source *AuthoredSource) { source.contentHash = "wrong" },
		"wrong size":           func(source *AuthoredSource) { source.size = 0 },
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			broken := source
			corrupt(&broken)
			projector.calls = 0
			if _, err := runtime.Project(broken); !errors.Is(err, ErrInvalidAuthoredSource) {
				t.Fatalf("Project error = %v, want ErrInvalidAuthoredSource", err)
			}
			if projector.calls != 0 {
				t.Fatalf("projector called %d times for invalid source", projector.calls)
			}
		})
	}
}

type internalProvider struct {
	descriptor Descriptor
}

func (p internalProvider) Descriptor() Descriptor {
	return p.descriptor
}

type internalProjector struct {
	descriptor Descriptor
	calls      int
}

func (p *internalProjector) Descriptor() Descriptor {
	return p.descriptor
}

func (p *internalProjector) Project(AuthoredSource) (Projection, error) {
	p.calls++
	return NewProjection(p.descriptor.ProviderVersion, p.descriptor.ProjectionVersion, ProjectionStatusCurrent, nil, p.descriptor.Capabilities)
}
