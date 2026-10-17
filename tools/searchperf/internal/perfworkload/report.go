package perfworkload

import (
	"sort"
)

type Aggregate struct {
	Profile            string                    `json:"profile"`
	Concurrency        int                       `json:"concurrency"`
	Temperature        string                    `json:"temperature"`
	QueryID            string                    `json:"queryId"`
	LaneMode           string                    `json:"laneMode"`
	Samples            int                       `json:"samples"`
	Errors             int                       `json:"errors"`
	Deadlines          int                       `json:"deadlines"`
	DurationMS         Percentiles               `json:"durationMs"`
	ProviderDurationMS Percentiles               `json:"providerDurationMs"`
	ProviderCalls      Percentiles               `json:"providerCalls"`
	Candidates         Percentiles               `json:"retrievedCandidates"`
	BodyReads          Percentiles               `json:"bodyReads"`
	BodyReadBytes      Percentiles               `json:"bodyReadBytes"`
	ResponseBytes      Percentiles               `json:"responseBytes"`
	HeapAllocBytes     Percentiles               `json:"heapAllocBytes"`
	GoRuntimeSysBytes  Percentiles               `json:"goRuntimeSysBytes"`
	PeakRSSBytes       Percentiles               `json:"peakRssBytes"`
	Stages             map[string]StageAggregate `json:"stages"`
}

type StageAggregate struct {
	Samples    int         `json:"samples"`
	DurationMS Percentiles `json:"durationMs"`
}

type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}

func AggregateSamples(samples []Sample) []Aggregate {
	groups := map[string][]Sample{}
	keys := make([]string, 0)
	for _, sample := range samples {
		key := string(sample.Profile) + "\x00" + sample.Temperature + "\x00" + sample.QueryID + "\x00" + sample.LaneMode + "\x00" + string(rune(sample.Concurrency))
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], sample)
	}
	sort.Strings(keys)
	out := make([]Aggregate, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		aggregate := Aggregate{Profile: string(group[0].Profile), Concurrency: group[0].Concurrency, Temperature: group[0].Temperature, QueryID: group[0].QueryID, LaneMode: group[0].LaneMode, Samples: len(group), Stages: map[string]StageAggregate{}}
		var durations, providerDurations, providerCalls, candidates, bodyReads, bodyBytes, responseBytes, heap, sys, rss []float64
		stages := map[string][]float64{}
		for _, sample := range group {
			if sample.Error != "" {
				aggregate.Errors++
			}
			if sample.DeadlineExceeded {
				aggregate.Deadlines++
			}
			durations = append(durations, sample.DurationMS)
			providerDurations = append(providerDurations, sample.ProviderDurationMS)
			providerCalls = append(providerCalls, float64(sample.ProviderCalls))
			candidates = append(candidates, float64(sample.RetrievedCandidates))
			bodyReads = append(bodyReads, float64(sample.BodyReads))
			bodyBytes = append(bodyBytes, float64(sample.BodyReadBytes))
			responseBytes = append(responseBytes, float64(sample.ResponseBytes))
			heap = append(heap, float64(sample.HeapAllocBytes))
			sys = append(sys, float64(sample.GoRuntimeSysBytes))
			rss = append(rss, float64(sample.PeakRSSBytes))
			for _, stage := range sample.Stages {
				stages[stage.Kind+":"+stage.Name] = append(stages[stage.Kind+":"+stage.Name], stage.DurationMS)
			}
		}
		aggregate.DurationMS = percentile(durations)
		aggregate.ProviderDurationMS = percentile(providerDurations)
		aggregate.ProviderCalls = percentile(providerCalls)
		aggregate.Candidates = percentile(candidates)
		aggregate.BodyReads = percentile(bodyReads)
		aggregate.BodyReadBytes = percentile(bodyBytes)
		aggregate.ResponseBytes = percentile(responseBytes)
		aggregate.HeapAllocBytes = percentile(heap)
		aggregate.GoRuntimeSysBytes = percentile(sys)
		aggregate.PeakRSSBytes = percentile(rss)
		for name, values := range stages {
			aggregate.Stages[name] = StageAggregate{Samples: len(values), DurationMS: percentile(values)}
		}
		out = append(out, aggregate)
	}
	return out
}

func percentile(values []float64) Percentiles {
	if len(values) == 0 {
		return Percentiles{}
	}
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	at := func(fraction float64) float64 {
		index := int(float64(len(values)-1)*fraction + 0.999999)
		if index >= len(values) {
			index = len(values) - 1
		}
		return values[index]
	}
	return Percentiles{P50: at(.50), P95: at(.95), Max: values[len(values)-1]}
}
