package noteformat_test

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestRegistryFindsProvidersByCaseInsensitiveExtensionWithoutChangingPath(t *testing.T) {
	registry, err := noteformat.NewRegistry(
		testProvider{descriptor: testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault)},
		testProvider{descriptor: testDescriptor("html", []string{".html", ".htm"}, noteformat.OwnershipExplicitInclude)},
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	path := paths.NormalizeNotePath("Notes/Decision.HTML")
	provider, ok := registry.ProviderForPath(paths.RelPath(path))
	if !ok {
		t.Fatal("ProviderForPath returned no provider")
	}
	if got := provider.Descriptor().ID; got != "html" {
		t.Fatalf("provider ID = %q, want html", got)
	}
	if got := path.String(); got != "Notes/Decision.HTML" {
		t.Fatalf("path was changed to %q", got)
	}
}

func TestRegistryRejectsCaseFoldedFormatIDConflicts(t *testing.T) {
	_, err := noteformat.NewRegistry(
		testProvider{descriptor: testDescriptor("HTML", []string{".html"}, noteformat.OwnershipExplicitInclude)},
		testProvider{descriptor: testDescriptor("html", []string{".htm"}, noteformat.OwnershipExplicitInclude)},
	)
	if !errors.Is(err, noteformat.ErrDuplicateFormatID) {
		t.Fatalf("NewRegistry error = %v, want ErrDuplicateFormatID", err)
	}
}

func TestRegistryRejectsCaseFoldedExtensionConflicts(t *testing.T) {
	_, err := noteformat.NewRegistry(
		testProvider{descriptor: testDescriptor("first", []string{".HTML"}, noteformat.OwnershipExplicitInclude)},
		testProvider{descriptor: testDescriptor("second", []string{".html"}, noteformat.OwnershipExplicitInclude)},
	)
	if !errors.Is(err, noteformat.ErrDuplicateExtension) {
		t.Fatalf("NewRegistry error = %v, want ErrDuplicateExtension", err)
	}
}

func TestRegistryCopiesDescriptorSlices(t *testing.T) {
	provider := testProvider{descriptor: testDescriptor("html", []string{".html"}, noteformat.OwnershipExplicitInclude)}
	registry, err := noteformat.NewRegistry(provider)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	provider.descriptor.Extensions[0] = ".changed"
	found, ok := registry.Provider("HTML")
	if !ok {
		t.Fatal("Provider returned no provider")
	}
	descriptor := found.Descriptor()
	descriptor.Extensions[0] = ".mutated"
	if got := found.Descriptor().Extensions[0]; got != ".html" {
		t.Fatalf("registry descriptor mutated to %q", got)
	}
}

func TestRegistryRejectsMissingProjectionVersion(t *testing.T) {
	_, err := noteformat.NewRegistry(
		testProvider{descriptor: noteformat.Descriptor{ID: "html", Extensions: []string{".html"}, ProviderVersion: "1", OwnershipPolicy: noteformat.OwnershipExplicitInclude}},
	)
	if err == nil {
		t.Fatal("NewRegistry succeeded with missing projection version")
	}
}

func TestRegistryRejectsMissingProviderVersion(t *testing.T) {
	_, err := noteformat.NewRegistry(
		testProvider{descriptor: noteformat.Descriptor{ID: "html", Extensions: []string{".html"}, ProjectionVersion: "1", OwnershipPolicy: noteformat.OwnershipExplicitInclude}},
	)
	if err == nil {
		t.Fatal("NewRegistry succeeded with missing provider version")
	}
}

func TestRegistryManifestFingerprintIsOrderIndependent(t *testing.T) {
	markdown := testProvider{descriptor: testDescriptor("markdown", []string{".md"}, noteformat.OwnershipDefault)}
	html := testProvider{descriptor: noteformat.Descriptor{ID: "html", Extensions: []string{".htm", ".html"}, ProviderVersion: "provider-v2", ProjectionVersion: "projection-v3", OwnershipPolicy: noteformat.OwnershipExplicitInclude, Capabilities: noteformat.MustCapabilities(noteformat.CapabilitySourceReading)}}
	first, err := noteformat.NewRegistry(markdown, html)
	if err != nil {
		t.Fatalf("NewRegistry first: %v", err)
	}
	second, err := noteformat.NewRegistry(html, markdown)
	if err != nil {
		t.Fatalf("NewRegistry second: %v", err)
	}
	if first.ManifestFingerprint() != second.ManifestFingerprint() {
		t.Fatalf("manifest fingerprints differ: %q != %q", first.ManifestFingerprint(), second.ManifestFingerprint())
	}
}

func TestRegistryCanonicalizesFormatID(t *testing.T) {
	registry, err := noteformat.NewRegistry(testProvider{descriptor: testDescriptor("HTML", []string{".html"}, noteformat.OwnershipExplicitInclude)})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	provider, ok := registry.Provider("html")
	if !ok || provider.Descriptor().ID != "html" {
		t.Fatalf("canonical provider = %#v, found = %v", provider, ok)
	}
}

func TestRegistryManifestFingerprintChangesForEveryPersistedDimension(t *testing.T) {
	baseline := testDescriptor("html", []string{".html"}, noteformat.OwnershipExplicitInclude)
	baselineRegistry, err := noteformat.NewRegistry(testProvider{descriptor: baseline})
	if err != nil {
		t.Fatalf("NewRegistry baseline: %v", err)
	}
	baselineFingerprint := baselineRegistry.ManifestFingerprint()

	tests := []struct {
		name   string
		mutate func(*noteformat.Descriptor)
	}{
		{
			name: "format ID",
			mutate: func(descriptor *noteformat.Descriptor) {
				descriptor.ID = "webnote"
			},
		},
		{
			name: "extension",
			mutate: func(descriptor *noteformat.Descriptor) {
				descriptor.Extensions = []string{".htm"}
			},
		},
		{
			name: "provider version",
			mutate: func(descriptor *noteformat.Descriptor) {
				descriptor.ProviderVersion = "provider-v2"
			},
		},
		{
			name: "projection version",
			mutate: func(descriptor *noteformat.Descriptor) {
				descriptor.ProjectionVersion = "projection-v2"
			},
		},
		{
			name: "ownership policy",
			mutate: func(descriptor *noteformat.Descriptor) {
				descriptor.OwnershipPolicy = noteformat.OwnershipDefault
			},
		},
		{
			name: "capabilities",
			mutate: func(descriptor *noteformat.Descriptor) {
				descriptor.Capabilities = noteformat.MustCapabilities(noteformat.CapabilitySourceReading)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			descriptor := baseline
			descriptor.Extensions = append([]string(nil), baseline.Extensions...)
			tt.mutate(&descriptor)
			registry, err := noteformat.NewRegistry(testProvider{descriptor: descriptor})
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			if got := registry.ManifestFingerprint(); got == baselineFingerprint {
				t.Fatal("manifest fingerprint did not change")
			}
		})
	}
}

type testProvider struct {
	descriptor noteformat.Descriptor
}

func (p testProvider) Descriptor() noteformat.Descriptor {
	return p.descriptor
}

func testDescriptor(id noteformat.FormatID, extensions []string, policy noteformat.OwnershipPolicy) noteformat.Descriptor {
	return noteformat.Descriptor{ID: id, Extensions: extensions, ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", OwnershipPolicy: policy}
}
