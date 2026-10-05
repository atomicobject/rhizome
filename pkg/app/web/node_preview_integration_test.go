package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNodePreviewEndpoint(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "notes", "plain.md"), []byte("---\nsummary: Plain summary\nflag: true\n---\n# Plain\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "notes", "fragments.md"), []byte("# Fragment examples\n\n## C# Guide\n\n## Array]\n\n## Some Heading\n\nBlock paragraph. ^Block-ID\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.root, "notes", "chart.html"), []byte(`<!doctype html><html><head><title>Chart page</title></head><body><h1>Chart page</h1><div id="Chart-A">Chart</div><a name="Legacy-A">Legacy</a></body></html>`), 0o644))
	fixture.vaultDef.Root = fixture.root
	fixture.vaultDef.Includes = []string{"**/*.md", "**/*.html"}
	_, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)
	rows, err := fixture.intelStore.AllOntologyNodes(context.Background())
	require.NoError(t, err)
	var sectionRef, embeddedRef ontology.NodeRef
	var sectionTitle, embeddedTitle string
	for _, row := range rows {
		var ref ontology.NodeRef
		require.NoError(t, json.Unmarshal([]byte(row.NodeRefJSON), &ref))
		switch {
		case row.TypeName == "StoriesSection":
			sectionRef, sectionTitle = ref, row.DisplayLabel
		case row.TypeName == "Criterion" && !strings.HasPrefix(ref.Fragment, "^"):
			embeddedRef, embeddedTitle = ref, row.DisplayLabel
		}
	}
	require.Equal(t, ontology.NodeKindSection, sectionRef.Kind)
	require.NotEmpty(t, sectionRef.Structural)
	require.Equal(t, ontology.NodeKindEmbedded, embeddedRef.Kind)
	require.NotEmpty(t, embeddedRef.Structural)

	runtime := &Runtime{IntelStore: fixture.intelStore}
	srv := newFixtureServer(t, fixture, runtime)
	reader := &countingNoteReader{contents: map[string]string{
		"specs/100-demo/spec.md": "must not be read",
		"specs/100-demo/plan.md": "must not be read",
		"notes/plain.md":         "must not be read",
		"notes/fragments.md":     "must not be read",
		"notes/chart.html":       "must not be read",
	}}
	srv.noteReader = reader
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()

	tests := []struct {
		name               string
		method             string
		path               string
		status             int
		fragmentResolved   *bool
		wantFragment       *NodePreviewFragment
		wantType           string
		wantTitle          string
		wantRef            string
		anyType            bool // internal fallback type naming is not this row's contract
		wantSummary        string
		wantResolvedTarget string
	}{
		{name: "typed note with resolved link", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=specs%2F100-demo%2Fplan.md", status: http.StatusOK, wantType: "Plan", wantSummary: "Demo plan summary", wantResolvedTarget: "specs/100-demo/spec.md"},
		{name: "link values resolve from the previewed note, not the referrer", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=specs%2F100-demo%2Fplan.md&from=notes%2Fplain.md", status: http.StatusOK, wantType: "Plan", wantSummary: "Demo plan summary", wantResolvedTarget: "specs/100-demo/spec.md"},
		{name: "untyped note", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=notes%2Fplain.md", status: http.StatusOK, wantSummary: "Plain summary"},
		{name: "heading fragment", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=specs%2F100-demo%2Fspec.md%23Requirements", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "heading", Text: "Requirements"}, wantType: "Spec", wantSummary: "Demo spec summary"},
		{name: "normalized heading fragment", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=specs%2F100-demo%2Fspec.md%23%20rEqUiReMeNtS%20", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "heading", Text: "Requirements"}, wantType: "Spec", wantSummary: "Demo spec summary"},
		{name: "heading fragment containing hash", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=notes%2Ffragments.md%23C%23%20Guide", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "heading", Text: "C# Guide"}},
		{name: "heading fragment ending in bracket", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=notes%2Ffragments.md%23Array%5D", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "heading", Text: "Array]"}},
		{name: "markdown block fragment keeps authored case", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=notes%2Ffragments.md%23%5EBlock-ID", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "block", Text: "Block-ID"}, anyType: true},
		{name: "markdown block fragment is case sensitive", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=notes%2Ffragments.md%23%5Eblock-id", status: http.StatusOK, fragmentResolved: boolPointer(false)},
		{name: "html element id", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=notes%2Fchart.html%23CHART-A", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "element_id", Text: "Chart-A"}},
		{name: "html legacy anchor name", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=notes%2Fchart.html%23Legacy-A", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "element_id", Text: "Legacy-A"}},
		{name: "block fragment", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=specs%2F100-demo%2Fspec.md%23%5Evalidation", status: http.StatusOK, fragmentResolved: boolPointer(true), wantFragment: &NodePreviewFragment{Kind: "block", Text: "validation"}, wantType: "Story"},
		{name: "structural section selector", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=" + url.QueryEscape(sectionRef.NotePath+"#struct:"+sectionRef.Structural), status: http.StatusOK, fragmentResolved: boolPointer(true), wantType: sectionRef.TypeName, wantTitle: sectionTitle, wantRef: sectionRef.String()},
		{name: "structural unanchored embedded selector", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=" + url.QueryEscape(embeddedRef.NotePath+"#struct:"+embeddedRef.Structural), status: http.StatusOK, fragmentResolved: boolPointer(true), wantType: embeddedRef.TypeName, wantTitle: embeddedTitle, wantRef: embeddedRef.String(), wantSummary: embeddedTitle},
		{name: "fragment fallback", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=specs%2F100-demo%2Fspec.md%23Missing", status: http.StatusOK, fragmentResolved: boolPointer(false), wantType: "Spec", wantSummary: "Demo spec summary"},
		{name: "missing structural selector", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=specs%2F100-demo%2Fspec.md%23struct%3Amissing", status: http.StatusNotFound},
		{name: "missing ref", method: http.MethodGet, path: "/api/v1/nodes/preview", status: http.StatusBadRequest},
		{name: "unresolved ref", method: http.MethodGet, path: "/api/v1/nodes/preview?ref=missing%2Fnote.md", status: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPut, path: "/api/v1/nodes/preview?ref=notes%2Fplain.md", status: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(test.method, httpServer.URL+test.path, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, test.status, resp.StatusCode)
			if test.status != http.StatusOK {
				var publicErr ErrorResponse
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&publicErr))
				require.NotEmpty(t, publicErr.Code)
				return
			}
			var preview NodePreview
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&preview))
			if !test.anyType {
				require.Equal(t, test.wantType, preview.TypeName)
			}
			if test.wantTitle != "" {
				require.Equal(t, test.wantTitle, preview.Title)
			}
			if test.wantRef != "" {
				require.Equal(t, test.wantRef, preview.Ref)
			}
			require.Equal(t, test.wantSummary, preview.Summary)
			if test.fragmentResolved != nil {
				require.Equal(t, *test.fragmentResolved, preview.FragmentResolved)
			}
			require.Equal(t, test.wantFragment, preview.Fragment)
			if test.wantResolvedTarget != "" {
				var found bool
				for _, field := range preview.Fields {
					for _, value := range field.Values {
						if value.Target == test.wantResolvedTarget {
							found = true
						}
					}
				}
				require.True(t, found)
			}
		})
	}
	require.Empty(t, reader.reads, "summary-only preview must not read note content")
}

func boolPointer(value bool) *bool {
	return &value
}
