package perfworkload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type Sample struct {
	Profile             unifiedsearch.Profile `json:"profile"`
	Concurrency         int                   `json:"concurrency"`
	Temperature         string                `json:"temperature"`
	QueryID             string                `json:"queryId"`
	LaneMode            string                `json:"laneMode"`
	EffectiveConfig     EffectiveConfig       `json:"effectiveConfig"`
	DurationMS          float64               `json:"durationMs"`
	Error               string                `json:"error,omitempty"`
	DeadlineExceeded    bool                  `json:"deadlineExceeded"`
	ProviderCalls       int64                 `json:"providerCalls"`
	ProviderDurationMS  float64               `json:"providerDurationMs"`
	RetrievedCandidates int                   `json:"retrievedCandidates"`
	ReturnedSources     int                   `json:"returnedSources"`
	BodyReads           int64                 `json:"bodyReads"`
	BodyReadBytes       int64                 `json:"bodyReadBytes"`
	ResponseBytes       int                   `json:"responseBytes"`
	PackedBytes         int                   `json:"packedBytes"`
	HeapAllocBytes      uint64                `json:"heapAllocBytes"`
	GoRuntimeSysBytes   uint64                `json:"goRuntimeSysBytes"`
	PeakRSSBytes        uint64                `json:"peakRssBytes"`
	Stages              []Stage               `json:"stages,omitempty"`
	LaneResultCounts    map[string]int        `json:"laneResultCounts,omitempty"`
	LaneStatuses        []search.LaneStatus   `json:"laneStatuses,omitempty"`
}

type EffectiveConfig struct {
	Intent          string   `json:"intent"`
	Types           []string `json:"types,omitempty"`
	PathPrefixes    []string `json:"pathPrefixes,omitempty"`
	UseVector       bool     `json:"useVector"`
	UseIntel        bool     `json:"useIntel"`
	UseGraph        bool     `json:"useGraph"`
	Pack            bool     `json:"pack"`
	RequireBody     bool     `json:"requireBody,omitempty"`
	CandidateWindow int      `json:"candidateWindow"`
	DeadlineMillis  int64    `json:"deadlineMillis"`
}

type Stage struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	DurationMS float64 `json:"durationMs"`
	Status     string  `json:"status,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type RunOptions struct {
	Root        string
	Profile     unifiedsearch.Profile
	Concurrency int
	Warmups     int
	Samples     int
	Query       Query
}

func ValidateQueries(ctx context.Context, root string, manifest Manifest, profiles []unifiedsearch.Profile) error {
	store, err := semdb.OpenReadOnlyExisting(IndexPath(root), ctx, sqliteutil.Options{})
	if err != nil {
		return err
	}
	defer store.Close()
	for _, profile := range profiles {
		for _, query := range manifest.Queries {
			sample := Measure(ctx, root, store, manifest, profile, 1, "validation", query)
			if err := validateSample(sample); err != nil {
				return fmt.Errorf("validate query %s/%s: %w", profile, query.ID, err)
			}
		}
	}
	return nil
}

func validateSample(sample Sample) error {
	if sample.Error != "" {
		return errors.New(sample.Error)
	}
	if sample.ReturnedSources == 0 {
		return errors.New("no eligible sources")
	}
	for _, stage := range sample.Stages {
		if stage.Error != "" {
			return fmt.Errorf("%s failed: %s", stage.Name, stage.Error)
		}
	}
	switch sample.LaneMode {
	case "lexical":
		if sample.ProviderCalls != 0 {
			return fmt.Errorf("lexical workload made %d provider calls", sample.ProviderCalls)
		}
	case "hybrid":
		if sample.ProviderCalls == 0 {
			return errors.New("hybrid workload did not call its provider")
		}
		if sample.LaneResultCounts["note_vector"]+sample.LaneResultCounts["code_vector"] == 0 {
			return errors.New("hybrid workload returned no vector evidence")
		}
	case "graph":
		if sample.LaneResultCounts["graph"] == 0 {
			return errors.New("graph workload returned no graph evidence")
		}
	}
	if sample.EffectiveConfig.Pack {
		if sample.PackedBytes == 0 {
			return errors.New("packed workload returned empty packed text")
		}
	}
	if sample.EffectiveConfig.RequireBody && (sample.BodyReads == 0 || sample.BodyReadBytes == 0) {
		return errors.New("body-required workload did not materialize indexed source content")
	}
	return nil
}

func RunWarm(ctx context.Context, manifest Manifest, opts RunOptions) ([]Sample, error) {
	if opts.Concurrency < 1 {
		return nil, errors.New("concurrency must be positive")
	}
	if opts.Warmups < 0 || opts.Samples < 1 {
		return nil, errors.New("warmups must be non-negative and samples positive")
	}
	store, err := semdb.OpenReadOnlyExisting(IndexPath(opts.Root), ctx, sqliteutil.Options{})
	if err != nil {
		return nil, err
	}
	defer store.Close()
	for i := 0; i < opts.Warmups; i++ {
		_ = Measure(ctx, opts.Root, store, manifest, opts.Profile, opts.Concurrency, "warmup", opts.Query)
	}
	jobs := make(chan int)
	results := make(chan indexedSample, opts.Samples)
	var wg sync.WaitGroup
	for worker := 0; worker < opts.Concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results <- indexedSample{i: i, sample: Measure(ctx, opts.Root, store, manifest, opts.Profile, opts.Concurrency, "warm", opts.Query)}
			}
		}()
	}
	go func() {
		for i := 0; i < opts.Samples; i++ {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	ordered := make([]indexedSample, 0, opts.Samples)
	for result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].i < ordered[j].i })
	out := make([]Sample, len(ordered))
	for i := range ordered {
		out[i] = ordered[i].sample
	}
	return out, nil
}

type indexedSample struct {
	i      int
	sample Sample
}

func Measure(ctx context.Context, root string, store *semdb.Store, manifest Manifest, profile unifiedsearch.Profile, concurrency int, temperature string, query Query) Sample {
	providerConfig := embeddings.ProviderConfig{Provider: manifest.Embeddings.Provider, Model: manifest.Embeddings.Model, Dimensions: manifest.Embeddings.Dimensions}
	provider := &measuredProvider{delegate: embeddings.NewDeterministicProvider(providerConfig)}
	collector := indexingperf.NewSemanticQueryCollector()
	ctx = indexingperf.WithCollector(ctx, collector)
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	useVector := query.LaneMode == "hybrid"
	useGraph := query.LaneMode == "graph"
	result, err := unifiedsearch.Execute(ctx, unifiedsearch.ApplicationOptions{
		Profile: profile, Diagnostics: true,
		Runtime: unifiedsearch.Options{
			Query: query.Text, IntentInput: query.Intent, Seeds: query.Seeds,
			UseVector: useVector, UseIntel: true, UseGraph: useGraph,
			Pack: query.Pack, BudgetChars: 8_000, VaultPath: root,
			VaultDef:     obsidian.VaultDefinition{Path: root},
			Filters:      search.Filters{Types: query.Types, PathPrefixes: query.PathPrefixes},
			IntelStore:   store,
			NoteProvider: provider, NoteProviderConfig: providerConfig,
			CodeProvider: provider, CodeProviderConfig: providerConfig,
		},
	})
	encoded, marshalErr := json.Marshal(result)
	duration := time.Since(started)
	if err == nil && marshalErr != nil {
		err = marshalErr
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	calls, providerDuration := provider.snapshot()
	sample := Sample{
		Profile: profile, Concurrency: concurrency, Temperature: temperature, QueryID: query.ID, LaneMode: query.LaneMode,
		EffectiveConfig:  EffectiveConfig{Intent: query.Intent, Types: append([]string(nil), query.Types...), PathPrefixes: append([]string(nil), query.PathPrefixes...), UseVector: useVector, UseIntel: true, UseGraph: useGraph, Pack: query.Pack, RequireBody: query.RequireBody, CandidateWindow: result.Request.Policy.CandidateWindow, DeadlineMillis: result.Request.Policy.DeadlineMillis},
		DurationMS:       float64(duration.Microseconds()) / 1000,
		DeadlineExceeded: errors.Is(err, context.DeadlineExceeded),
		ProviderCalls:    calls, ProviderDurationMS: float64(providerDuration.Microseconds()) / 1000,
		RetrievedCandidates: result.Counts.RetrievedCandidates, ReturnedSources: result.Counts.ReturnedSources,
		ResponseBytes: len(encoded), HeapAllocBytes: after.HeapAlloc, GoRuntimeSysBytes: after.Sys, PeakRSSBytes: processPeakRSSBytes(),
	}
	sample.PackedBytes = len(result.PackedText)
	if err != nil {
		sample.Error = err.Error()
	}
	for _, lane := range result.Lanes {
		sample.LaneStatuses = append(sample.LaneStatuses, lane)
		if sample.LaneResultCounts == nil {
			sample.LaneResultCounts = map[string]int{}
		}
		sample.LaneResultCounts[lane.Lane] = lane.ResultCount
	}
	if sample.Error == "" {
		sample.Error = requiredLaneError(query, result.Lanes)
	}
	for _, event := range result.Timings {
		sample.Stages = append(sample.Stages, Stage{Name: event.Name, Kind: event.Kind, DurationMS: float64(event.Duration.Microseconds()) / 1000, Status: event.Status, Error: event.Err})
	}
	for _, operation := range collector.SemanticQueryDiagnostics().Operations {
		switch operation.Label {
		case indexingperf.SemanticQueryOpBodyReads:
			sample.BodyReads = operation.Count
		case indexingperf.SemanticQueryOpBodyReadBytes:
			sample.BodyReadBytes = operation.Count
		}
	}
	_ = before
	return sample
}

func requiredLaneError(query Query, lanes []search.LaneStatus) string {
	statusByLane := make(map[string]search.LaneStatus, len(lanes))
	for _, lane := range lanes {
		statusByLane[lane.Lane] = lane
	}
	requireRan := func(name string) string {
		lane := statusByLane[name]
		if lane.Status != search.LaneStateRan || lane.ResultCount == 0 {
			return fmt.Sprintf("required %s lane was %s with %d results: %s", name, lane.Status, lane.ResultCount, lane.Reason)
		}
		return ""
	}
	switch query.LaneMode {
	case "graph":
		return requireRan("graph")
	case "hybrid":
		wantNote, wantCode := len(query.Types) == 0, len(query.Types) == 0
		for _, value := range query.Types {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "note", "notes", "doc_section", "note_chunk":
				wantNote = true
			case "code", "anchor", "code_chunk":
				wantCode = true
			}
		}
		if wantNote {
			if message := requireRan("note_vector"); message != "" {
				return message
			}
		}
		if wantCode {
			return requireRan("code_vector")
		}
	}
	return ""
}

type measuredProvider struct {
	delegate embeddings.Provider
	mu       sync.Mutex
	calls    int64
	duration time.Duration
}

func (p *measuredProvider) Dimensions() int { return p.delegate.Dimensions() }

func (p *measuredProvider) EmbedTexts(ctx context.Context, texts []string) ([]embeddings.Embedding, error) {
	started := time.Now()
	result, err := p.delegate.EmbedTexts(ctx, texts)
	p.mu.Lock()
	p.calls++
	p.duration += time.Since(started)
	p.mu.Unlock()
	return result, err
}

func (p *measuredProvider) snapshot() (int64, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.duration
}
