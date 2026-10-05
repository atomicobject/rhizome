package retrieval

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/search"
)

func addTiming(ctx context.Context, ev search.TimingEvent) {
	if t := search.TimingsFromContext(ctx); t != nil {
		t.Add(ev)
	}
}

func timingStatus(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "error"
}

func timingErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
