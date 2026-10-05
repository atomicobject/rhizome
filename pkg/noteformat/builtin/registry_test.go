package builtin_test

import (
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestNewRegistryRegistersMarkdownAndHTML(t *testing.T) {
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for path, wantID := range map[string]string{
		"notes/one.md":    "markdown",
		"notes/two.html":  "html",
		"notes/three.HTM": "html",
	} {
		provider, ok := registry.ProviderForPath(paths.Normalize(path))
		if !ok {
			t.Fatalf("ProviderForPath(%q) returned no provider", path)
		}
		if got := string(provider.Descriptor().ID); got != wantID {
			t.Fatalf("ProviderForPath(%q) ID = %q, want %q", path, got, wantID)
		}
	}
}

func TestNewRuntimeProjectsMarkdownAndHTML(t *testing.T) {
	runtime, err := builtin.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	for _, tc := range []struct {
		format, path, content, title, search string
	}{
		{"markdown", "notes/report.md", "# Markdown title\nMarkdown body", "Markdown title", "Markdown body"},
		{"html", "notes/report.html", "<h1>HTML title</h1><p>HTML body</p>", "HTML title", "HTML body"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			provider, ok := runtime.Provider(noteformat.FormatID(tc.format))
			if !ok {
				t.Fatalf("provider %q unavailable", tc.format)
			}
			source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath(tc.path), provider.Descriptor(), []byte(tc.content), 0)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := runtime.Project(source)
			if err != nil || projection.Status != noteformat.ProjectionStatusCurrent {
				t.Fatalf("Project(%q) = %#v, %v", tc.path, projection, err)
			}
			if projection.Facts.Title == nil || projection.Facts.Title.Value != tc.title {
				t.Fatalf("title = %#v, want %q", projection.Facts.Title, tc.title)
			}
			var search strings.Builder
			for _, region := range projection.Facts.SearchRegions {
				search.WriteString(region.Text)
			}
			if !strings.Contains(search.String(), tc.search) {
				t.Fatalf("search = %q, want %q", search.String(), tc.search)
			}
		})
	}
}
