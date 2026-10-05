package html

import (
	"bytes"
	"testing"
)

func TestViewerBootstrapOffsetPrecedesAuthoredScript(t *testing.T) {
	for _, source := range [][]byte{
		[]byte("<!doctype html><html><head><script>authored()</script></head></html>"),
		[]byte("<html><body><script>authored()</script></body></html>"),
		[]byte("<title>&lt;head&gt;</title><script>authored()</script>"),
		append([]byte{0xef, 0xbb, 0xbf}, []byte("<!doctype html>\r\n<script>authored()</script>")...),
	} {
		offset, err := ViewerBootstrapOffset(source)
		if err != nil {
			t.Fatal(err)
		}
		script := bytes.Index(bytes.ToLower(source), []byte("<script"))
		if script >= 0 && offset > script {
			t.Fatalf("offset %d follows authored script at %d in %q", offset, script, source)
		}
	}
}

func TestViewerBootstrapOffsetRejectsNestedHead(t *testing.T) {
	_, err := ViewerBootstrapOffset([]byte("<template><head></head></template><script>authored()</script>"))
	if err == nil {
		t.Fatal("nested head unexpectedly accepted")
	}
}

func TestViewerBootstrapOffsetSkipsNonExecutableScripts(t *testing.T) {
	source := []byte(`<html><head><script type="application/json">{"data":true}</script><script type="module">authored()</script></head></html>`)
	offset, err := ViewerBootstrapOffset(source)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Index(source, []byte(`<script type="module">`))
	if offset != want {
		t.Fatalf("offset = %d, want first executable script at %d", offset, want)
	}
}

func TestViewerBootstrapOffsetPreservesPrefixWhenNoExecutableScript(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte("<!doctype html>\r\n<html><body><p>plain</p></body></html>")...)
	offset, err := ViewerBootstrapOffset(source)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Index(source, []byte("<html>"))
	if offset != want {
		t.Fatalf("offset = %d, want after BOM/doctype whitespace at %d", offset, want)
	}
	if !bytes.Equal(source[:3], []byte{0xef, 0xbb, 0xbf}) || !bytes.HasPrefix(source[3:], []byte("<!doctype html>")) {
		t.Fatal("source prefix changed while finding bootstrap offset")
	}
}

func TestViewerBootstrapOffsetIgnoresTemplateScripts(t *testing.T) {
	source := []byte(`<template><script>not executed</script></template><script>authored()</script>`)
	offset, err := ViewerBootstrapOffset(source)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.LastIndex(source, []byte(`<script>`))
	if offset != want {
		t.Fatalf("offset = %d, want executable script at %d", offset, want)
	}
}
