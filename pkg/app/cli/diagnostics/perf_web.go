package diagnostics

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
)

// webClientEvent names the browser batch event recorded by pkg/app/web.
const webClientEvent = "web.client"

// WebSummary aggregates browser batches for one surface (desktop or browser).
type WebSummary struct {
	Surface    string            `json:"surface"`
	Windows    int               `json:"windows"`
	Sessions   int               `json:"sessions"`
	Frames     WebFrames         `json:"frames"`
	Refresh    WebRefresh        `json:"refresh"`
	Reconnects uint64            `json:"reconnects"`
	Errors     map[string]uint64 `json:"errors,omitempty"`
	Rejections map[string]uint64 `json:"rejections,omitempty"`
	Regions    []WebRegion       `json:"regions,omitempty"`
}

type WebFrames struct {
	Over50ms  uint64  `json:"over_50ms"`
	Over200ms uint64  `json:"over_200ms"`
	StalledMs float64 `json:"stalled_ms"`
	LongestMs float64 `json:"longest_ms"`
}

type WebRefresh struct {
	Cycles    uint64 `json:"cycles"`
	Refetched uint64 `json:"refetched"`
	Unchanged uint64 `json:"unchanged"`
}

type WebRegion struct {
	Name                string  `json:"name"`
	Mount               uint64  `json:"mount"`
	Update              uint64  `json:"update"`
	NestedUpdate        uint64  `json:"nested_update"`
	ActualTotalMs       float64 `json:"actual_total_ms"`
	ActualMaxMs         float64 `json:"actual_max_ms"`
	CommitsAfterRefresh uint64  `json:"commits_after_refresh"`
}

// webBatchMetrics mirrors the metrics object pkg/app/web records for a batch.
type webBatchMetrics struct {
	Regions   map[string]WebRegion `json:"regions"`
	Frames    WebFrames            `json:"frames"`
	Refresh   WebRefresh           `json:"refresh"`
	Stability struct {
		Reconnects uint64            `json:"reconnects"`
		Errors     map[string]uint64 `json:"errors"`
		Rejections map[string]uint64 `json:"rejections"`
	} `json:"stability"`
}

type webSurface struct {
	summary  WebSummary
	sessions map[string]bool
	regions  map[string]*WebRegion
}

type webAggregator struct {
	surfaces map[string]*webSurface
	stalls   PerfFlag
	churn    PerfFlag
	unstable PerfFlag
	seen     map[string]map[string]bool
}

func newWebAggregator() *webAggregator {
	return &webAggregator{surfaces: map[string]*webSurface{}, seen: map[string]map[string]bool{},
		stalls: PerfFlag{Kind: "web_frame_stalls"}, churn: PerfFlag{Kind: "web_refresh_render_churn"}, unstable: PerfFlag{Kind: "web_stream_instability"}}
}

func (w *webAggregator) visit(event evidence.Event) {
	var batch webBatchMetrics
	if len(event.Metrics) == 0 || json.Unmarshal(event.Metrics, &batch) != nil {
		return
	}
	name := attrString(event.Attributes, "surface")
	surface := w.surfaces[name]
	if surface == nil {
		surface = &webSurface{summary: WebSummary{Surface: name, Errors: map[string]uint64{}, Rejections: map[string]uint64{}},
			sessions: map[string]bool{}, regions: map[string]*WebRegion{}}
		w.surfaces[name] = surface
	}
	s := &surface.summary
	s.Windows++
	surface.sessions[attrString(event.Attributes, "session")] = true
	s.Frames.Over50ms += batch.Frames.Over50ms
	s.Frames.Over200ms += batch.Frames.Over200ms
	s.Frames.StalledMs += batch.Frames.StalledMs
	s.Frames.LongestMs = max(s.Frames.LongestMs, batch.Frames.LongestMs)
	s.Refresh.Cycles += batch.Refresh.Cycles
	s.Refresh.Refetched += batch.Refresh.Refetched
	s.Refresh.Unchanged += batch.Refresh.Unchanged
	s.Reconnects += batch.Stability.Reconnects
	for errorName, count := range batch.Stability.Errors {
		s.Errors[errorName] += count
	}
	for errorName, count := range batch.Stability.Rejections {
		s.Rejections[errorName] += count
	}
	churned := false
	for regionName, region := range batch.Regions {
		total := surface.regions[regionName]
		if total == nil {
			total = &WebRegion{Name: regionName}
			surface.regions[regionName] = total
		}
		total.Mount += region.Mount
		total.Update += region.Update
		total.NestedUpdate += region.NestedUpdate
		total.ActualTotalMs += region.ActualTotalMs
		total.ActualMaxMs = max(total.ActualMaxMs, region.ActualMaxMs)
		total.CommitsAfterRefresh += region.CommitsAfterRefresh
		churned = churned || region.CommitsAfterRefresh > 0
	}
	if batch.Frames.Over200ms > 0 {
		w.note(&w.stalls, event, batch.Frames.Over200ms)
	}
	// Every refetch in the window returned identical data, yet regions still
	// committed right after a refresh: those renders had no new data to show.
	if churned && batch.Refresh.Refetched > 0 && batch.Refresh.Unchanged == batch.Refresh.Refetched {
		w.note(&w.churn, event, 1)
	}
	if batch.Stability.Reconnects > batch.Refresh.Cycles {
		w.note(&w.unstable, event, 1)
	}
}

// note counts an occurrence; the flag lists client sessions, since browser
// batches carry no operation identity of their own.
func (w *webAggregator) note(flag *PerfFlag, event evidence.Event, count uint64) {
	flag.Count += int(count)
	flag.FirstAt = earliest(flag.FirstAt, event.Time)
	flag.LastAt = latest(flag.LastAt, event.Time)
	session := attrString(event.Attributes, "session")
	if w.seen[flag.Kind] == nil {
		w.seen[flag.Kind] = map[string]bool{}
	}
	if session != "" && !w.seen[flag.Kind][session] && len(flag.OperationIDs) < maxFlagOperations {
		w.seen[flag.Kind][session] = true
		flag.OperationIDs = append(flag.OperationIDs, "session:"+session)
	}
}

func (w *webAggregator) summaries() []WebSummary {
	out := make([]WebSummary, 0, len(w.surfaces))
	for _, name := range slices.Sorted(maps.Keys(w.surfaces)) {
		surface := w.surfaces[name]
		summary := surface.summary
		summary.Sessions = len(surface.sessions)
		for _, region := range surface.regions {
			summary.Regions = append(summary.Regions, *region)
		}
		sort.Slice(summary.Regions, func(i, j int) bool { return summary.Regions[i].ActualTotalMs > summary.Regions[j].ActualTotalMs })
		out = append(out, summary)
	}
	return out
}

func (w *webAggregator) flags() []PerfFlag {
	var flags []PerfFlag
	if w.stalls.Count > 0 {
		w.stalls.Observation = fmt.Sprintf("The web UI had %d frame gaps over 200 ms; the main thread was blocked long enough to freeze the page.", w.stalls.Count)
		flags = append(flags, w.stalls)
	}
	if w.churn.Count > 0 {
		w.churn.Observation = fmt.Sprintf("In %d windows, render regions committed after a background refresh whose refetched data was unchanged; those renders may be unnecessary.", w.churn.Count)
		flags = append(flags, w.churn)
	}
	if w.unstable.Count > 0 {
		w.unstable.Observation = fmt.Sprintf("In %d windows, the event stream reconnected more often than refreshes ran; the runtime connection may be unstable.", w.unstable.Count)
		flags = append(flags, w.unstable)
	}
	return flags
}

func writeWebText(b *strings.Builder, summaries []WebSummary) {
	for _, s := range summaries {
		fmt.Fprintf(b, "Web UI (%s): %d windows from %d sessions; frames >50ms=%d >200ms=%d stalled=%s longest=%s; refresh cycles=%d refetched=%d unchanged=%d; reconnects=%d\n",
			s.Surface, s.Windows, s.Sessions, s.Frames.Over50ms, s.Frames.Over200ms, msDuration(s.Frames.StalledMs), msDuration(s.Frames.LongestMs),
			s.Refresh.Cycles, s.Refresh.Refetched, s.Refresh.Unchanged, s.Reconnects)
		for _, region := range s.Regions {
			fmt.Fprintf(b, "  region %s: mount=%d update=%d nested=%d render total=%s max=%s after-refresh=%d\n",
				region.Name, region.Mount, region.Update, region.NestedUpdate, msDuration(region.ActualTotalMs), msDuration(region.ActualMaxMs), region.CommitsAfterRefresh)
		}
		for _, counts := range []struct {
			label  string
			values map[string]uint64
		}{{"errors", s.Errors}, {"rejections", s.Rejections}} {
			for _, name := range slices.Sorted(maps.Keys(counts.values)) {
				fmt.Fprintf(b, "  %s %s: %d\n", counts.label, name, counts.values[name])
			}
		}
	}
}

func msDuration(ms float64) string {
	return time.Duration(ms * float64(time.Millisecond)).Round(time.Millisecond).String()
}
