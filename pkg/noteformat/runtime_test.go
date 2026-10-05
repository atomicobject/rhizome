package noteformat_test

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestRuntimeDispatchesRegisteredProjectorAndCopiesItsOutput(t *testing.T) {
	registry, err := noteformat.NewRegistry(testProvider{descriptor: testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault)})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	metadata, err := noteformat.NewMetadataValue(map[string]any{"values": []any{"original"}})
	if err != nil {
		t.Fatalf("NewMetadataValue: %v", err)
	}
	projection := currentProjection(t, noteformat.ProjectionFacts{
		Title: &noteformat.TitleFact{Value: "Original title", Range: exactRange(0, 8)},
		RootMetadata: []noteformat.RootMetadataFact{{
			Key: "nested", Value: metadata, Range: exactRange(0, 8),
		}},
	})
	markdown := testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault)
	projector := &stubProjector{descriptor: markdown, projection: projection}
	runtime, err := noteformat.NewRuntime(registry, projector)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath("notes/test.md"), testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault), []byte("# title\n"), 0)
	if err != nil {
		t.Fatalf("NewAuthoredSource: %v", err)
	}
	got, err := runtime.Project(source)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if projector.calls != 1 || projector.lastPath != "notes/test.md" {
		t.Fatalf("projector dispatch = calls=%d path=%q", projector.calls, projector.lastPath)
	}
	if got.Facts.Title == nil || got.Facts.Title.Value != "Original title" {
		t.Fatalf("projection title = %#v", got.Facts.Title)
	}

	got.Facts.Title.Value = "changed"
	got.Facts.RootMetadata[0].Value.Export().(map[string]any)["values"].([]any)[0] = "changed"
	again, err := runtime.Project(source)
	if err != nil {
		t.Fatalf("Project again: %v", err)
	}
	if again.Facts.Title.Value != "Original title" {
		t.Fatalf("runtime reused mutable title output: %#v", again.Facts.Title)
	}
	if value := again.Facts.RootMetadata[0].Value.Export().(map[string]any)["values"].([]any)[0]; value != "original" {
		t.Fatalf("runtime reused mutable metadata output: %#v", again.Facts.RootMetadata)
	}
}

func TestRuntimeReportsAbsentProjectorForDescriptorOnlyProvider(t *testing.T) {
	registry, err := noteformat.NewRegistry(testProvider{descriptor: testDescriptor("html", []string{".html"}, noteformat.OwnershipExplicitInclude)})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	runtime, err := noteformat.NewRuntime(registry)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath("notes/test.html"), testDescriptor("html", []string{".html"}, noteformat.OwnershipExplicitInclude), nil, 0)
	if err != nil {
		t.Fatalf("NewAuthoredSource: %v", err)
	}
	if _, err := runtime.Project(source); !errors.Is(err, noteformat.ErrProjectorUnavailable) {
		t.Fatalf("Project error = %v, want ErrProjectorUnavailable", err)
	}
	if runtime.CanProject("html") {
		t.Fatal("descriptor-only provider unexpectedly has a projector")
	}
}

func TestRuntimeReportsExecutableRootMetadataMutationTruth(t *testing.T) {
	descriptor := testDescriptor("test", []string{".test"}, noteformat.OwnershipExplicitInclude)
	descriptor.Capabilities = noteformat.MustCapabilities(noteformat.CapabilitySourceReading, noteformat.CapabilityRootMetadataMutation)
	projector := &metadataPatchingProjector{stubProjector: stubProjector{descriptor: descriptor, projection: currentProjectionFor(t, descriptor, noteformat.ProjectionFacts{})}}
	registry, err := noteformat.NewRegistry(projector)
	if err != nil {
		t.Fatal(err)
	}
	descriptorOnly, err := noteformat.NewRuntime(registry)
	if err != nil {
		t.Fatal(err)
	}
	if descriptorOnly.CanPatchRootMetadata("test") {
		t.Fatal("descriptor capability was reported as executable mutation support")
	}
	executable, err := noteformat.NewRuntime(registry, projector)
	if err != nil {
		t.Fatal(err)
	}
	if !executable.CanPatchRootMetadata("test") {
		t.Fatal("executable metadata planner was not reported")
	}
}

func TestRuntimeRejectsUnknownAndMismatchedProjectorsAndSources(t *testing.T) {
	markdown := testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault)
	registry, err := noteformat.NewRegistry(testProvider{descriptor: markdown})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := noteformat.NewRuntime(registry, &stubProjector{descriptor: testDescriptor("html", []string{".html"}, noteformat.OwnershipExplicitInclude), projection: currentProjection(t, noteformat.ProjectionFacts{})}); !errors.Is(err, noteformat.ErrUnknownProjectorFormat) {
		t.Fatalf("NewRuntime unknown projector error = %v", err)
	}
	if _, err := noteformat.NewRuntime(registry, &stubProjector{descriptor: markdown, projection: currentProjection(t, noteformat.ProjectionFacts{})}, &stubProjector{descriptor: markdown, projection: currentProjection(t, noteformat.ProjectionFacts{})}); !errors.Is(err, noteformat.ErrDuplicateProjector) {
		t.Fatalf("NewRuntime duplicate projector error = %v", err)
	}
	mismatched := markdown
	mismatched.ProviderVersion = "other"
	if _, err := noteformat.NewRuntime(registry, &stubProjector{descriptor: mismatched, projection: currentProjection(t, noteformat.ProjectionFacts{})}); !errors.Is(err, noteformat.ErrProjectorDescriptorMismatch) {
		t.Fatalf("NewRuntime mismatched descriptor error = %v", err)
	}
}

func TestRuntimeValidatesProjectionRangesAndDescriptorVersions(t *testing.T) {
	descriptor := testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault)
	descriptor.Capabilities = noteformat.MustCapabilities(noteformat.CapabilitySourceReading)
	registry, err := noteformat.NewRegistry(testProvider{descriptor: descriptor})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath("notes/test.md"), descriptor, []byte("short"), 0)
	if err != nil {
		t.Fatalf("NewAuthoredSource: %v", err)
	}

	tooLong := currentProjectionFor(t, descriptor, noteformat.ProjectionFacts{Title: &noteformat.TitleFact{Value: "too long", Range: exactRange(0, 6)}})
	runtime, err := noteformat.NewRuntime(registry, &stubProjector{descriptor: descriptor, projection: tooLong})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := runtime.Project(source); !errors.Is(err, noteformat.ErrInvalidProjectionFacts) {
		t.Fatalf("Project range error = %v, want ErrInvalidProjectionFacts", err)
	}

	wrongVersion, err := noteformat.NewProjectionWithFacts("wrong", "projection-v1", noteformat.ProjectionStatusCurrent, nil, descriptor.Capabilities, noteformat.ProjectionFacts{})
	if err != nil {
		t.Fatalf("NewProjectionWithFacts: %v", err)
	}
	runtime, err = noteformat.NewRuntime(registry, &stubProjector{descriptor: descriptor, projection: wrongVersion})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := runtime.Project(source); !errors.Is(err, noteformat.ErrProjectionDescriptorMismatch) {
		t.Fatalf("Project version error = %v, want ErrProjectionDescriptorMismatch", err)
	}

	missingCapabilities, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil, noteformat.MustCapabilities(), noteformat.ProjectionFacts{})
	if err != nil {
		t.Fatalf("NewProjectionWithFacts: %v", err)
	}
	runtime, err = noteformat.NewRuntime(registry, &stubProjector{descriptor: descriptor, projection: missingCapabilities})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := runtime.Project(source); !errors.Is(err, noteformat.ErrProjectionDescriptorMismatch) {
		t.Fatalf("Project capability error = %v, want ErrProjectionDescriptorMismatch", err)
	}

	invalid := noteformat.Projection{
		ProviderVersion:   descriptor.ProviderVersion,
		ProjectionVersion: descriptor.ProjectionVersion,
		Status:            "unknown",
		Capabilities:      descriptor.Capabilities,
	}
	runtime, err = noteformat.NewRuntime(registry, &stubProjector{descriptor: descriptor, projection: invalid})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, err := runtime.Project(source); !errors.Is(err, noteformat.ErrInvalidProjection) {
		t.Fatalf("Project invalid envelope error = %v, want ErrInvalidProjection", err)
	}
}

type stubProjector struct {
	descriptor noteformat.Descriptor
	projection noteformat.Projection
	calls      int
	lastPath   paths.NotePath
}

type metadataPatchingProjector struct {
	stubProjector
}

func (*metadataPatchingProjector) PlanRootMetadataPatch(noteformat.AuthoredSource, noteformat.Projection, []noteformat.RootMetadataChange) (noteformat.MetadataPatchPlan, error) {
	return noteformat.MetadataPatchPlan{}, nil
}

func (p *stubProjector) Descriptor() noteformat.Descriptor {
	return p.descriptor
}

func (p *stubProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	p.calls++
	p.lastPath = source.Path()
	return p.projection, nil
}

func currentProjection(t *testing.T, facts noteformat.ProjectionFacts) noteformat.Projection {
	return currentProjectionFor(t, testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault), facts)
}

func currentProjectionFor(t *testing.T, descriptor noteformat.Descriptor, facts noteformat.ProjectionFacts) noteformat.Projection {
	t.Helper()
	projection, err := noteformat.NewProjectionWithFacts(
		"provider-v1",
		"projection-v1",
		noteformat.ProjectionStatusCurrent,
		nil,
		descriptor.Capabilities,
		facts,
	)
	if err != nil {
		t.Fatalf("NewProjectionWithFacts: %v", err)
	}
	return projection
}

func exactRange(start, end int) noteformat.OptionalSourceRange {
	return noteformat.OptionalSourceRange{Present: true, Range: noteformat.SourceRange{StartByte: start, EndByte: end}}
}
