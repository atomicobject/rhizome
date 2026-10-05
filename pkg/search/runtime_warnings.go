package search

import (
	"context"
	"strings"
	"sync"
)

type warningSinkKey struct{}

type warningSink struct {
	mu       sync.Mutex
	warnings []Warning
}

func withWarningSink(ctx context.Context) (context.Context, *warningSink) {
	sink := &warningSink{}
	return context.WithValue(ctx, warningSinkKey{}, sink), sink
}

func AddRuntimeWarning(ctx context.Context, warning Warning) {
	if ctx == nil {
		return
	}
	sink, _ := ctx.Value(warningSinkKey{}).(*warningSink)
	if sink == nil {
		return
	}
	warning.Code = strings.TrimSpace(warning.Code)
	warning.Message = strings.TrimSpace(warning.Message)
	if warning.Code == "" || warning.Message == "" {
		return
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.warnings = append(sink.warnings, warning)
}

func (s *warningSink) snapshot() []Warning {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.warnings) == 0 {
		return nil
	}
	out := make([]Warning, 0, len(s.warnings))
	seen := make(map[Warning]struct{}, len(s.warnings))
	for _, warning := range s.warnings {
		if _, ok := seen[warning]; ok {
			continue
		}
		seen[warning] = struct{}{}
		out = append(out, warning)
	}
	return out
}
