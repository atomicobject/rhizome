package search

import (
	"context"
	"reflect"
	"testing"
)

func TestRuntimeWarningsPreserveDistinctFields(t *testing.T) {
	ctx, sink := withWarningSink(context.Background())
	want := []Warning{
		{Code: "fallback", Message: "failed|retriever", Source: "vector", Kind: "retrieval"},
		{Code: "fallback", Message: "failed", Source: "retriever|vector", Kind: "retrieval"},
	}
	for _, warning := range want {
		AddRuntimeWarning(ctx, warning)
		AddRuntimeWarning(ctx, warning)
	}
	if got := sink.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %#v, want distinct warnings in insertion order %#v", got, want)
	}
}
