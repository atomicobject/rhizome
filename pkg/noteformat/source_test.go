package noteformat_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestNewAuthoredSourceCapturesCanonicalSourceFacts(t *testing.T) {
	descriptor := htmlDescriptor()
	descriptor.ID = "HTML"
	source, err := noteformat.NewAuthoredSource(
		paths.NormalizeNotePath("notes/decision.html"),
		descriptor,
		[]byte("<h1>Decision</h1>"),
		123,
	)
	if err != nil {
		t.Fatalf("NewAuthoredSource: %v", err)
	}
	if source.Path() != "notes/decision.html" || source.Format() != "html" {
		t.Fatalf("unexpected identity: %#v", source)
	}
	digest := sha256.Sum256([]byte("<h1>Decision</h1>"))
	if source.Size() != int64(len("<h1>Decision</h1>")) || source.Mtime() != 123 || source.ContentHash() != hex.EncodeToString(digest[:]) {
		t.Fatalf("unexpected source freshness: %#v", source)
	}
}

func TestCapabilitiesUseStableCanonicalNames(t *testing.T) {
	capabilities, err := noteformat.NewCapabilities(
		noteformat.CapabilitySourceReading,
		noteformat.CapabilityAuthoredLinkExtraction,
		noteformat.CapabilitySourceReading,
	)
	if err != nil {
		t.Fatalf("NewCapabilities: %v", err)
	}
	if !capabilities.Has(noteformat.CapabilitySourceReading) || capabilities.Has(noteformat.Capability("unknown")) || capabilities.Has("") {
		t.Fatalf("unexpected capability membership: %v", capabilities.Values())
	}
	if got := capabilities.Values(); len(got) != 2 || got[0] != noteformat.CapabilityAuthoredLinkExtraction || got[1] != noteformat.CapabilitySourceReading {
		t.Fatalf("canonical capability values = %v", got)
	}
	if _, err := noteformat.NewCapabilities("unknown"); err == nil {
		t.Fatal("NewCapabilities accepted an unknown stable identifier")
	}
}

func TestNewAuthoredSourceRejectsMissingIdentity(t *testing.T) {
	_, err := noteformat.NewAuthoredSource(paths.NotePath(""), htmlDescriptor(), nil, 0)
	if err == nil {
		t.Fatal("NewAuthoredSource succeeded without a path")
	}
}

func TestNewAuthoredSourceRejectsAbsoluteAndTraversalPaths(t *testing.T) {
	for _, path := range []paths.NotePath{"/outside/note.html", "../outside/note.html"} {
		if _, err := noteformat.NewAuthoredSource(path, htmlDescriptor(), nil, 0); err == nil {
			t.Fatalf("NewAuthoredSource(%q) succeeded", path)
		}
	}
}

func TestNewProjectionSeparatesProviderAndProjectionVersions(t *testing.T) {
	projection, err := noteformat.NewProjection(
		"provider-v1",
		"projection-v2",
		noteformat.ProjectionStatusCurrent,
		nil,
		noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	)
	if err != nil {
		t.Fatalf("NewProjection: %v", err)
	}
	if projection.ProviderVersion != "provider-v1" || projection.ProjectionVersion != "projection-v2" {
		t.Fatalf("projection versions = %#v", projection)
	}
}

func TestNewProjectionRequiresBlockingDiagnosticForFailure(t *testing.T) {
	_, err := noteformat.NewProjection(
		"provider-v1",
		"projection-v1",
		noteformat.ProjectionStatusFatal,
		nil,
		noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	)
	if err == nil {
		t.Fatal("NewProjection succeeded without a blocking diagnostic")
	}
}

func TestNewProjectionAcceptsFatalBlockingDiagnostic(t *testing.T) {
	projection, err := noteformat.NewProjection(
		"provider-v1",
		"projection-v1",
		noteformat.ProjectionStatusFatal,
		[]noteformat.Diagnostic{{Code: "invalid_utf8", Message: "source is not UTF-8", Blocking: true}},
		noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	)
	if err != nil {
		t.Fatalf("NewProjection: %v", err)
	}
	if projection.Status != noteformat.ProjectionStatusFatal || len(projection.Diagnostics) != 1 {
		t.Fatalf("unexpected fatal projection: %#v", projection)
	}
}

func TestAuthoredSourceCopiesInputAndOutputBytes(t *testing.T) {
	input := []byte("<h1>immutable</h1>")
	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath("notes/immutable.html"), htmlDescriptor(), input, 0)
	if err != nil {
		t.Fatalf("NewAuthoredSource: %v", err)
	}
	input[0] = 'X'
	first := source.Bytes()
	if string(first) != "<h1>immutable</h1>" {
		t.Fatalf("source changed with caller input: %q", first)
	}
	first[0] = 'Y'
	if got := string(source.Bytes()); got != "<h1>immutable</h1>" {
		t.Fatalf("source changed through returned bytes: %q", got)
	}
}

func TestNewProjectionAllowsStaleStatus(t *testing.T) {
	projection, err := noteformat.NewProjection(
		"provider-v1",
		"projection-v1",
		noteformat.ProjectionStatusStale,
		nil,
		noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	)
	if err != nil || projection.Status != noteformat.ProjectionStatusStale {
		t.Fatalf("stale projection = %#v, error = %v", projection, err)
	}
}

func TestNewProjectionRejectsBlockingDiagnosticForStaleStatus(t *testing.T) {
	_, err := noteformat.NewProjection(
		"provider-v1",
		"projection-v1",
		noteformat.ProjectionStatusStale,
		[]noteformat.Diagnostic{{Code: "stale_source", Message: "source changed", Blocking: true}},
		noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	)
	if err == nil {
		t.Fatal("NewProjection accepted a blocking stale diagnostic")
	}
}

func htmlDescriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID:                "html",
		Extensions:        []string{".html", ".htm"},
		ProviderVersion:   "provider-v1",
		ProjectionVersion: "projection-v1",
		OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		Capabilities:      noteformat.MustCapabilities(noteformat.CapabilitySourceReading),
	}
}
