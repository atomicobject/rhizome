package codeanchor

import (
	"testing"
)

func TestExtractSpan_FallbackToLines(t *testing.T) {
	content := []byte("line1\nline2\nline3\n")
	anchor := IntelAnchor{StartLine: 2, EndLine: 3}
	span := ExtractSpan(content, anchor)
	if span == "" {
		t.Fatalf("expected non-empty span")
	}
	if span != "line2\nline3" {
		t.Fatalf("unexpected span: %q", span)
	}
}
