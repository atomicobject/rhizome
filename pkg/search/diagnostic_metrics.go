package search

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func recordSearchFallback(ctx context.Context, stage string, err error) {
	if operation, _ := ctx.Value(searchOperationKey{}).(*searchOperation); operation != nil {
		operation.partial.Store(true)
	}
	indexingperf.AddCount(ctx, indexingperf.DiagnosticFallbackPrefix+stage+"."+statusFromErr(err), 1)
}

func searchResultStatus(ctx context.Context, err error) string {
	if err == nil {
		operation, _ := ctx.Value(searchOperationKey{}).(*searchOperation)
		if ctx.Err() != nil || (operation != nil && operation.partial.Load()) {
			return "partial"
		}
	}
	return statusFromErr(err)
}
