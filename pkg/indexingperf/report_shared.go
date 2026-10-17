package indexingperf

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Both renderers consume the observed identities. A newly named producer is
// visible automatically instead of requiring a second hand-maintained lookup.
func (c *Collector) metricReportParts(phase string, window bool, elapsed time.Duration) []string {
	names := map[Metric]bool{}
	for metric := range c.observedMetrics() {
		if metric.Phase == phase {
			names[metric] = true
		}
	}
	metrics := make([]Metric, 0, len(names))
	for m := range names {
		metrics = append(metrics, m)
	}
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].Name == metrics[j].Name {
			return metrics[i].Kind < metrics[j].Kind
		}
		return metrics[i].Name < metrics[j].Name
	})
	var parts []string
	for _, m := range metrics {
		if m.Name == CodeembedPrepare || m.Name == NoteembedPrepare || m.Name == ProviderCapTexts || m.Name == ProviderDispatchFloorTexts || (!window && (m.Name == FsDiscovered || m.Name == FsCompleted || m.Name == NodePrepareIn || m.Name == NodePrepareOut || m.Name == NodeEmbedIn || m.Name == NodeEmbedOut)) {
			continue
		}
		if m.Kind == kindCounter && groupedCounter(m.Name) {
			continue
		}

		display, hasDisplay := metricDisplayFor(m.Name)
		total, base := display.summaryTotal, display.summaryBase
		if window {
			total, base = display.windowTotal, display.windowBase
		}
		if base == "" {
			base = strings.ReplaceAll(string(m.Name), ".", "_")
		}
		switch m.Kind {
		case kindCounter:
			value := c.counters[m].Total
			if window {
				value -= c.lastCounters[m]
			}
			if window && value == 0 {
				continue
			}
			if total == "" {
				total = base
			}
			if strings.HasSuffix(string(m.Name), "_bytes") {
				parts = append(parts, total+"="+humanBytes(value))
			} else {
				parts = append(parts, fmt.Sprintf("%s=%d", total, value))
			}
			if elapsed > 0 && (m.Name == FsRead || m.Name == SemanticChunks || m.Name == ProviderCalls || m.Name == ProviderTexts) {
				rateLabel := total + "/s"
				if window {
					rateLabel = total + "_s"
				}
				parts = append(parts, fmt.Sprintf("%s=%.1f", rateLabel, float64(value)/elapsed.Seconds()))
			}
		case kindLatency:
			value := c.latencies[m].Total
			if window {
				value -= c.lastLatencies[m]
			}
			if value <= 0 {
				continue
			}
			if total == "" {
				total = base + "_cum"
				if window {
					total += "_ms"
				}
			}
			formatted := shortDur(value)
			if window {
				formatted = fmt.Sprint(value.Milliseconds())
			}
			parts = append(parts, total+"="+formatted)
			if display.quantiles {
				p50, p95, peak := c.phaseLatencyPercentiles(phase, m.Name)
				if window {
					p50, p95, peak = c.windowLatencyWindowPercentilesPhase(phase, m.Name)
				}
				for i, q := range []time.Duration{p50, p95, peak} {
					suffix := []string{"p50", "p95", "max"}[i]
					formatted = shortDur(q)
					if window {
						suffix += "_ms"
						formatted = fmt.Sprint(q.Milliseconds())
					}
					parts = append(parts, base+"_"+suffix+"="+formatted)
				}
			}
		case kindSample:
			count := c.phaseSampleCount(phase, m.Name)
			if window {
				count = c.windowSampleCountPhase(phase, m.Name)
			}
			if count == 0 {
				continue
			}
			p50, p95, peak := c.phaseSamplePercentiles(phase, m.Name)
			if window {
				p50, p95, peak = c.windowSamplePercentilesPhase(phase, m.Name)
			}
			for i, value := range []int64{p50, p95, peak} {
				formatted := fmt.Sprint(value)
				if strings.Contains(string(m.Name), "bytes") {
					formatted = humanBytes(value)
				} else if strings.Contains(string(m.Name), "ratio") {
					formatted += "%"
				}
				parts = append(parts, base+"_"+[]string{"p50", "p95", "max"}[i]+"="+formatted)
			}
		case kindGauge:
			stat := c.gaugeStats[m]
			previous := statCounter{}
			peak := c.gaugePeaks[m]
			if window {
				previous = c.lastGaugeStats[m]
				peak = c.windowGaugePeaks[m]
			}
			count := stat.Count - previous.Count
			if count == 0 && c.gauges[m] == 0 {
				continue
			}
			showCurrent := total != "" || !hasDisplay
			if total == "" {
				total = base
			}
			if showCurrent {
				parts = append(parts, fmt.Sprintf("%s=%d", total, c.gauges[m]))
			}
			parts = append(parts, fmt.Sprintf("%s_peak=%d", base, peak))
			if count > 0 {
				parts = append(parts, fmt.Sprintf("%s_avg=%.1f", base, float64(stat.Total-previous.Total)/float64(count)))
			}
		}
	}
	return append(parts, c.derivedReportParts(phase, window)...)
}

func (c *Collector) observedMetrics() map[Metric]bool {
	names := map[Metric]bool{}
	for metric := range c.counters {
		names[metric] = true
	}
	for metric := range c.latencies {
		names[metric] = true
	}
	for metric := range c.gauges {
		names[metric] = true
	}
	for metric := range c.samples {
		names[metric] = true
	}
	return names
}

func (c *Collector) derivedReportParts(phase string, window bool) []string {
	counter := c.phaseCounterTotal
	if window {
		counter = c.windowCounterDeltaPhase
	}
	var parts []string
	prefixCounts := c.phaseCounterTotalPrefix
	if window {
		prefixCounts = c.windowCounterDeltaPrefixPhase
	}
	for _, group := range counterGroupDisplays {
		if formatted := formatSuffixCounts(group.label, prefixCounts(phase, group.prefix)); formatted != "" {
			parts = append(parts, formatted)
		}
	}
	if !window && (counter(phase, FsDiscovered) != 0 || counter(phase, FsCompleted) != 0) {
		parts = append(parts, fmt.Sprintf("discovery=%d/%d", counter(phase, FsCompleted), counter(phase, FsDiscovered)))
	}
	prep := c.phaseLatencyTotal(phase, CodeembedPrepare) + c.phaseLatencyTotal(phase, NoteembedPrepare)
	if window {
		prep = time.Duration(c.windowLatencyDeltaPhase(phase, CodeembedPrepare)+c.windowLatencyDeltaPhase(phase, NoteembedPrepare)) * time.Millisecond
	}
	if prep > 0 {
		if window {
			parts = append(parts, fmt.Sprintf("prep_cum_ms=%d", prep.Milliseconds()))
		} else {
			parts = append(parts, "prep_cum="+shortDur(prep))
		}
	}

	if !window {
		for _, pair := range []struct {
			label   string
			in, out MetricName
		}{{"prepare_tasks", NodePrepareIn, NodePrepareOut}, {"embed_texts", NodeEmbedIn, NodeEmbedOut}} {
			incoming, outgoing := counter(phase, pair.in), counter(phase, pair.out)
			if incoming > 0 || outgoing > 0 {
				parts = append(parts, fmt.Sprintf("%s=%d/%d", pair.label, incoming, outgoing))
			}
		}
	}
	responses := c.phaseSampleCount(phase, ProviderRequestVoyageStatus)
	if window {
		responses = c.windowSampleCountPhase(phase, ProviderRequestVoyageStatus)
	}
	if responses > 0 {
		parts = append(parts, fmt.Sprintf("voyage_responses=%d", responses))
	}
	calls, texts := counter(phase, ProviderCalls), counter(phase, ProviderTexts)
	logical := counter(phase, ProviderLogicalTexts)
	if logical == 0 {
		logical = texts
	}
	if calls > 0 && texts > 0 {
		label := "avg_texts/call"
		if window {
			label = "avg_texts_call"
		}
		parts = append(parts, fmt.Sprintf("%s=%.1f", label, float64(texts)/float64(calls)))
	}
	_, _, capTexts := c.phaseSamplePercentiles(phase, ProviderCapTexts)
	_, _, floorTexts := c.phaseSamplePercentiles(phase, ProviderDispatchFloorTexts)
	if window {
		_, _, capTexts = c.windowSamplePercentilesPhase(phase, ProviderCapTexts)
		_, _, floorTexts = c.windowSamplePercentilesPhase(phase, ProviderDispatchFloorTexts)
	}
	if needed := ceilDivInt64(logical, capTexts); needed > 0 {
		parts = append(parts, fmt.Sprintf("calls_needed_at_provider_cap=%d", needed))
	}
	if needed := ceilDivInt64(logical, floorTexts); needed > 0 {
		parts = append(parts, fmt.Sprintf("calls_needed_at_dispatch_floor=%d", needed))
	}
	if calls > 0 {
		if bytes := counter(phase, ProviderTextBytes); bytes > 0 {
			parts = append(parts, "avg_bytes/call="+humanBytes(bytes/calls))
		}
	}

	if !window {
		if counter(phase, CodeembedStateFirstBuildCandidate) > 0 {
			parts = append(parts, "state_interpretation=first_build_or_empty_state")
		} else if counter(phase, CodeembedStateSchemaOrFingerprintMismatch) > 0 {
			parts = append(parts, "state_interpretation=fingerprint_version_or_schema_change")
		} else if counter(phase, CodeEmbeddingNeed.Select("legacy_missing_item_hash")) > 0 {
			parts = append(parts, "state_interpretation=legacy_rows_missing_item_hash")
		}
		_, _, capacity := c.phaseSamplePercentiles(phase, ProviderCapacity)
		if average := c.phaseGaugeAverage(phase, ProviderInflight); capacity > 0 && average > 0 {
			parts = append(parts, fmt.Sprintf("provider_inflight_utilization=%.1f%%", average*100/float64(capacity)))
		}
		var intervalNames []MetricName
		for metric := range c.intervals {
			if metric.Phase == phase && metric.Name != ProviderLatency && metric.Name != NodeWriteBusy {
				intervalNames = append(intervalNames, metric.Name)
			}
		}
		sort.Slice(intervalNames, func(i, j int) bool { return intervalNames[i] < intervalNames[j] })
		for _, name := range intervalNames {
			label := metricDisplays[name].summaryTotal
			if label == "" {
				label = strings.ReplaceAll(string(name), ".", "_") + "_wall"
			}
			parts = append(parts, label+"="+shortDur(c.phaseIntervalUnion(phase, name)))
		}
	}
	return parts
}
