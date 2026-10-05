package html_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestHTMLRecoveryKeepsNestedBlockAndPlainFragmentProse(t *testing.T) {
	source := []byte(`plain fragment <blockquote>quote<ul><li>one</li><li>two</li></ul>after</blockquote> suffix<div/>tail`)
	projection := projectRecovery(t, source)
	if projection.Status != noteformat.ProjectionStatusCurrent {
		t.Fatalf("status = %q, diagnostics = %#v", projection.Status, projection.Diagnostics)
	}
	var visible []string
	for _, region := range projection.Facts.SearchRegions {
		if region.Kind == noteformat.SearchRegionVisible {
			visible = append(visible, region.Text)
			if !region.Range.Present {
				t.Fatalf("visible region lost source range: %#v", region)
			}
		}
	}
	joined := strings.Join(visible, " ")
	for _, want := range []string{"plain fragment", "quote", "one", "two", "after", "suffix", "tail"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("visible text %q does not contain %q", joined, want)
		}
	}
}

func TestHTMLRecoveryUsesHTML5ImplicitHeadBodyAndNonVoidSelfClosingRules(t *testing.T) {
	source := []byte(`<title>Recovered title</title><div/>text after slash<p>paragraph<div>nested</div>tail`)
	projection := projectRecovery(t, source)
	if projection.Facts.Title == nil || projection.Facts.Title.Value != "Recovered title" {
		t.Fatalf("title = %#v", projection.Facts.Title)
	}
	joined := visibleText(projection)
	for _, want := range []string{"text after slash", "paragraph", "nested", "tail"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("recovered visible text %q does not contain %q", joined, want)
		}
	}
}

func TestHTMLRecoveryFosterParentingPreservesVisibleTextWithoutFabricatedRange(t *testing.T) {
	source := []byte(`<table>before<tr><td>cell</td></tr>after</table>`)
	projection := projectRecovery(t, source)
	if projection.Status != noteformat.ProjectionStatusCurrent {
		t.Fatalf("status = %q, diagnostics = %#v", projection.Status, projection.Diagnostics)
	}

	var visible []noteformat.SearchRegionFact
	hasUnranged := false
	for _, region := range projection.Facts.SearchRegions {
		if region.Kind != noteformat.SearchRegionVisible {
			continue
		}
		visible = append(visible, region)
		if !region.Range.Present {
			hasUnranged = true
			continue
		}
		if region.Range.Range.StartByte < 0 || region.Range.Range.EndByte > len(source) || region.Range.Range.EndByte < region.Range.Range.StartByte {
			t.Fatalf("visible region has invalid range: %#v", region)
		}
	}
	joined := ""
	for _, region := range visible {
		joined += " " + region.Text
	}
	for _, want := range []string{"before", "cell", "after"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("visible text %q does not contain %q", joined, want)
		}
	}
	if !hasUnranged {
		t.Fatalf("foster-parented visible text unexpectedly had only source ranges: %#v", visible)
	}
}

func TestHTMLVisibleTextDecodesEntitiesExactlyOnce(t *testing.T) {
	projection := projectRecovery(t, []byte(`<p>literal &amp;lt;script&amp;gt;</p>`))
	if got := visibleText(projection); got != `literal &lt;script&gt;` {
		t.Fatalf("visible text = %q, want browser-equivalent single decode", got)
	}
}

func TestHTMLRecoveryExcludesTemplateContentAfterMisnesting(t *testing.T) {
	source := []byte(`<p>before<div>inside</div>after<template><blockquote>template sentinel</blockquote></template><p>visible</p>`)
	projection := projectRecovery(t, source)
	joined := visibleText(projection)
	if strings.Contains(joined, "template sentinel") {
		t.Fatalf("template text leaked into visible extraction: %q", joined)
	}
	for _, want := range []string{"before", "inside", "after", "visible"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("visible text %q does not contain %q", joined, want)
		}
	}
}

func TestHTMLMetadataMutationRequiresRawTextCorrelation(t *testing.T) {
	valid := []byte(`<head><script id="rhizome-metadata" type="application/json">{"title":"old"}</script></head><body>body</body>`)
	plan, err := html.PlanMetadataPatch(valid, html.MetadataPatchRequest{Operations: []html.MetadataOperation{{Kind: html.MetadataSet, Key: "title", Value: "new"}}})
	if err != nil {
		t.Fatal(err)
	}
	bodyStart := bytes.Index(valid, []byte(`{"title":"old"}`))
	if len(plan.Edits) != 1 || plan.Edits[0].StartByte != bodyStart || plan.Edits[0].EndByte != bodyStart+len(`{"title":"old"}`) {
		t.Fatalf("metadata edit = %#v, want exact raw body span", plan.Edits)
	}

	unclosed := []byte(`<head><script id="rhizome-metadata" type="application/json">{"title":"old"}</head><body>body</body>`)
	if _, err := html.PlanMetadataPatch(unclosed, html.MetadataPatchRequest{Operations: []html.MetadataOperation{{Kind: html.MetadataSet, Key: "title", Value: "new"}}}); err == nil {
		t.Fatal("unclosed raw-text metadata unexpectedly accepted for mutation")
	}
}

func projectRecovery(t *testing.T, source []byte) noteformat.Projection {
	t.Helper()
	provider := html.New()
	authored, err := noteformat.NewAuthoredSource(paths.NormalizeNotePath("recovery.html"), provider.Descriptor(), source, 0)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := provider.Project(authored)
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func visibleText(projection noteformat.Projection) string {
	parts := make([]string, 0)
	for _, region := range projection.Facts.SearchRegions {
		if region.Kind == noteformat.SearchRegionVisible {
			parts = append(parts, region.Text)
		}
	}
	return strings.Join(parts, " ")
}
