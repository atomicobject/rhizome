package html_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestPlanMetadataPatchInsertsIntoExistingHeadPreservingBOMCRLFAndBody(t *testing.T) {
	source := []byte("\ufeff<!doctype html>\r\n<html>\r\n<head>\r\n  <title>Report</title>\r\n</head>\r\n<body>\r\n  <script>const source = \"script-like </scriptish>\";</script>\r\n  <p>Body bytes stay authored.</p>\r\n</body>\r\n</html>")
	plan, err := html.PlanMetadataPatch(source, html.MetadataPatchRequest{Operations: []html.MetadataOperation{{Kind: html.MetadataSet, Key: "title", Value: "Canonical"}, {Kind: html.MetadataAdd, Key: "type", Value: "ReferenceDoc"}, {Kind: html.MetadataAdd, Key: "description", Value: "unsafe </ScRiPt marker"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Preview.Inserted || len(plan.Edits) != 1 || plan.Edits[0].StartByte != plan.Edits[0].EndByte {
		t.Fatalf("insertion plan = %#v", plan)
	}
	if !bytes.HasPrefix(plan.UpdatedSource, []byte("\ufeff<!doctype html>\r\n")) {
		t.Fatal("BOM or doctype changed")
	}
	if !strings.Contains(string(plan.UpdatedSource), "<script id=\"rhizome-metadata\" type=\"application/json\">\r\n") {
		t.Fatalf("updated source lacks CRLF metadata block:\n%s", plan.UpdatedSource)
	}
	if !strings.Contains(string(plan.UpdatedSource), "\\u003c/ScRiPt") {
		t.Fatal("metadata JSON did not use safe JSON encoding for script terminators")
	}
	if !strings.Contains(string(plan.UpdatedSource), `const source = "script-like </scriptish>";`) {
		t.Fatal("script-like body content changed")
	}
	if !strings.Contains(string(plan.UpdatedSource), "<p>Body bytes stay authored.</p>") {
		t.Fatal("body content changed")
	}
	if !bytes.Equal(plan.UpdatedSource[:plan.Edits[0].StartByte], source[:plan.Edits[0].StartByte]) || !bytes.Equal(plan.UpdatedSource[plan.Edits[0].StartByte+len(plan.Edits[0].Replacement):], source[plan.Edits[0].StartByte:]) {
		t.Fatal("insertion changed bytes outside its bounded insertion span")
	}
	if plan.SourceHash != html.SourceHash(source) {
		t.Fatalf("source hash = %q, want %q", plan.SourceHash, html.SourceHash(source))
	}
}

func TestPlanMetadataPatchInsertsBeforeBodyWithoutHead(t *testing.T) {
	source := []byte("<!doctype html>\n<html>\n<body><p>Body</p></body>\n</html>\n")
	plan, err := html.PlanMetadataPatch(source, html.MetadataPatchRequest{Operations: []html.MetadataOperation{{Kind: html.MetadataAdd, Key: "type", Value: "Report"}}})
	if err != nil {
		t.Fatal(err)
	}
	updated := string(plan.UpdatedSource)
	if !strings.Contains(updated, "<head>\n  <script id=\"rhizome-metadata\"") {
		t.Fatalf("metadata head was not inserted before body:\n%s", updated)
	}
	if strings.Index(updated, "<head>") > strings.Index(updated, "<body>") {
		t.Fatal("head was inserted after body")
	}
	if !strings.Contains(updated, "</script>\n</head>\n<body>") {
		t.Fatalf("generated head is not structurally bounded:\n%s", updated)
	}
}

func TestPlanMetadataPatchReplacesOnlyCanonicalBodyAndSupportsDeleteRename(t *testing.T) {
	source := []byte(`<html><head><title>Page</title><script id="rhizome-metadata" type="application/json">{
  "keep": "unknown",
  "old": "value"
}</script></head><body><p>Keep this exact body.</p></body></html>`)
	plan, err := html.PlanMetadataPatch(source, html.MetadataPatchRequest{Operations: []html.MetadataOperation{{Kind: html.MetadataRename, Key: "old", NewKey: "new"}, {Kind: html.MetadataDelete, Key: "keep"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Edits) != 1 || plan.Edits[0].StartByte == plan.Edits[0].EndByte {
		t.Fatalf("replacement plan = %#v", plan)
	}
	if !bytes.Equal(source[:plan.Edits[0].StartByte], plan.UpdatedSource[:plan.Edits[0].StartByte]) {
		t.Fatal("bytes before metadata body changed")
	}
	updated := string(plan.UpdatedSource)
	if !strings.Contains(updated, `"new": "value"`) || strings.Contains(updated, `"old"`) || strings.Contains(updated, `"keep"`) {
		t.Fatalf("metadata operations not reflected:\n%s", updated)
	}
	if !strings.Contains(updated, "<p>Keep this exact body.</p>") {
		t.Fatal("body changed during metadata replacement")
	}
}

func TestPlanMetadataPatchNoOpAndStaleSource(t *testing.T) {
	source := []byte(`<head><script id="rhizome-metadata" type="application/json">{
  "title": "Same"
}</script></head><body>body</body>`)
	plan, err := html.PlanMetadataPatch(source, html.MetadataPatchRequest{ExpectedSourceHash: html.SourceHash(source), Operations: []html.MetadataOperation{{Kind: html.MetadataSet, Key: "title", Value: "Same"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Edits) != 0 || plan.HasMaterialChange || !bytes.Equal(plan.UpdatedSource, source) {
		t.Fatalf("no-op plan = %#v", plan)
	}
	_, err = html.PlanMetadataPatch(source, html.MetadataPatchRequest{ExpectedSourceHash: html.SourceHash([]byte("stale")), Operations: []html.MetadataOperation{{Kind: html.MetadataSet, Key: "title", Value: "New"}}})
	assertPatchCode(t, err, "html_metadata_source_stale")
}

func TestPlanMetadataPatchFailsClosedForMalformedDuplicateAndUnsafeShapes(t *testing.T) {
	tests := map[string]string{
		"malformed":            `<head><script id="rhizome-metadata" type="application/json">{"title":</script></head><body>body</body>`,
		"duplicate":            `<head><script id="rhizome-metadata" type="application/json">{"title":"a"}</script><script id="rhizome-metadata" type="application/json">{"title":"b"}</script></head><body>body</body>`,
		"duplicate attributes": `<head><script id="rhizome-metadata" id="other" type="application/json">{"title":"a"}</script></head><body>body</body>`,
		"no insertion point":   `<div>fragment only</div>`,
		"unclosed head":        `<head><title>Page</title><body>body</body>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := html.PlanMetadataPatch([]byte(source), html.MetadataPatchRequest{Operations: []html.MetadataOperation{{Kind: html.MetadataSet, Key: "title", Value: "new"}}})
			if err == nil {
				t.Fatal("expected metadata patch to block")
			}
		})
	}
}

func TestProviderMetadataPatchPlannerAdaptsGenericRootChanges(t *testing.T) {
	provider := html.New()
	sourceBytes := []byte(`<html><body><p>Body</p></body></html>`)
	source, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath("report.html"), provider.Descriptor(), sourceBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := provider.Project(source)
	if err != nil {
		t.Fatal(err)
	}
	value, err := noteformat.NewMetadataValue("Report")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := provider.PlanRootMetadataPatch(source, projection, []noteformat.RootMetadataChange{{Key: "title", Value: value}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Patches) != 1 || plan.Patches[0].Range.StartByte != plan.Patches[0].Range.EndByte {
		t.Fatalf("generic planner result = %#v", plan)
	}
	if err := noteformat.ValidateMetadataPatchPlan(source, plan); err != nil {
		t.Fatal(err)
	}
	patch := plan.Patches[0]
	updated := append([]byte(nil), sourceBytes[:patch.Range.StartByte]...)
	updated = append(updated, patch.Replacement...)
	updated = append(updated, sourceBytes[patch.Range.EndByte:]...)
	updatedSource, err := noteformat.NewAuthoredSource(source.Path(), provider.Descriptor(), updated, 0)
	if err != nil {
		t.Fatal(err)
	}
	updatedProjection, err := provider.Project(updatedSource)
	if err != nil || updatedProjection.Facts.Title == nil || updatedProjection.Facts.Title.Value != "Report" {
		t.Fatalf("patched title = %#v, error = %v", updatedProjection.Facts.Title, err)
	}
}

func assertPatchCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %q", code)
	}
	var patchErr *html.MetadataPatchError
	if !errors.As(err, &patchErr) {
		t.Fatalf("error %v does not expose MetadataPatchError", err)
	}
	if patchErr.Code != code {
		t.Fatalf("error code = %q, want %q", patchErr.Code, code)
	}
}
