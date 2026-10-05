package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type timingsKey struct{}

type TimingEvent struct {
	Name     string
	Kind     string
	Started  time.Time
	Duration time.Duration
	Status   string // ok|error|timeout|canceled|partial
	Err      string
}

type Timings struct {
	mu          sync.Mutex
	events      []TimingEvent
	display     *Timings
	collector   *indexingperf.Collector
	metricsOnly bool
}

func WithTimings(ctx context.Context, t *Timings) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil {
		return ctx
	}
	// Concurrent search facets share the display sink, but each context owns
	// its collector binding. Never mutate the sink's collector: rebinding it
	// would send another facet's stage measurements to the wrong report.
	bound := &Timings{display: t.eventSink(), collector: indexingperf.FromContext(ctx), metricsOnly: t.metricsOnly}
	return context.WithValue(ctx, timingsKey{}, bound)
}

func TimingsFromContext(ctx context.Context) *Timings {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(timingsKey{}); v != nil {
		if t, ok := v.(*Timings); ok {
			// A collector can be attached after WithTimings, including for a
			// later packing stage. Derive a local view without changing the
			// inherited binding or the shared display sink.
			if collector := indexingperf.FromContext(ctx); collector != t.collector {
				return &Timings{display: t.eventSink(), collector: collector, metricsOnly: t.metricsOnly}
			}
			return t
		}
	}
	return nil
}

func (t *Timings) Add(ev TimingEvent) {
	if t == nil {
		return
	}
	if !t.metricsOnly {
		display := t.eventSink()
		display.mu.Lock()
		display.events = append(display.events, ev)
		display.mu.Unlock()
	}
	collector := t.collector
	if collector != nil {
		var err error
		if ev.Status != "" && ev.Status != "ok" {
			err = errors.New("search stage did not complete")
		}
		collector.RecordSpanWindow("search."+ev.Name, ev.Started, ev.Started.Add(ev.Duration), err)
		ctx := indexingperf.WithPhase(indexingperf.WithCollector(context.Background(), collector), "search")
		status := safeTimingStatus(ev.Status)
		indexingperf.AddCount(ctx, "search.stage."+ev.Name+".outcome."+status, 1)
	}
}

func (t *Timings) Snapshot() []TimingEvent {
	if t == nil {
		return nil
	}
	display := t.eventSink()
	display.mu.Lock()
	defer display.mu.Unlock()
	out := make([]TimingEvent, len(display.events))
	copy(out, display.events)
	return out
}

func (t *Timings) eventSink() *Timings {
	if t.display != nil {
		return t.display
	}
	return t
}

func (t *Timings) Render() string {
	evs := t.Snapshot()
	if len(evs) == 0 {
		return ""
	}
	sort.SliceStable(evs, func(i, j int) bool {
		if !evs[i].Started.Equal(evs[j].Started) {
			return evs[i].Started.Before(evs[j].Started)
		}
		if evs[i].Kind != evs[j].Kind {
			return evs[i].Kind < evs[j].Kind
		}
		if evs[i].Name != evs[j].Name {
			return evs[i].Name < evs[j].Name
		}
		return evs[i].Duration > evs[j].Duration
	})

	var b strings.Builder
	b.WriteString("Timings:\n")
	for _, ev := range evs {
		line := fmt.Sprintf("- %-10s %-22s %7s", ev.Kind, ev.Name, fmt.Sprintf("%.0fms", float64(ev.Duration.Milliseconds())))
		if ev.Status != "" {
			line += " " + ev.Status
		}
		if strings.TrimSpace(ev.Err) != "" {
			line += " (" + strings.TrimSpace(ev.Err) + ")"
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func safeTimingStatus(status string) string {
	switch status {
	case "ok", "error", "timeout", "canceled", "partial":
		return status
	}
	return "unknown"
}

func withMetricsTimings(ctx context.Context) context.Context {
	if TimingsFromContext(ctx) == nil && indexingperf.FromContext(ctx) != nil {
		return WithTimings(ctx, &Timings{metricsOnly: true})
	}
	return ctx
}
