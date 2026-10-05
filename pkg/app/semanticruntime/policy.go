package semanticruntime

// Docs:
// - [[semantic-runtime-lane-policy#^spec-0044-packer-rules]]
// - [[semantic-runtime-lane-policy#^spec-0044-source-latency-classes]]

import (
	"time"

	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

type IndexSource string

const (
	// IndexSourceFullScan is the batch indexing path; prefer throughput.
	IndexSourceFullScan IndexSource = "full_scan"
	// IndexSourceCLIPaths is a targeted CLI refresh; policy depends on size.
	IndexSourceCLIPaths IndexSource = "cli_paths"
	// IndexSourceWatcher is live save-driven indexing; prefer latency unless a
	// burst is large enough to benefit from bigger provider packets.
	IndexSourceWatcher IndexSource = "watcher"
)

type LatencyClass string

const (
	// LatencyAuto lets PolicyFor infer the tradeoff from source and path count.
	LatencyAuto LatencyClass = "auto"
	// LatencyLowLatency favors quick small batches for interactive refresh.
	LatencyLowLatency LatencyClass = "low_latency"
	// LatencyThroughput favors larger batches for full scans and large bursts.
	LatencyThroughput LatencyClass = "throughput"
)

// IndexRequest captures the caller's indexing shape so provider packing can
// choose latency-oriented or throughput-oriented defaults.
type IndexRequest struct {
	Source       IndexSource
	PathCount    int
	LatencyClass LatencyClass
}

// PipelinePolicy is the debounce plus embedding-packer policy for a run.
type PipelinePolicy struct {
	Debounce        time.Duration
	CodeEmbedPacker *semantic.EmbedPackerOptions
	NoteEmbedPacker *semantic.EmbedPackerOptions
}

const (
	watcherPipelineDebounce      = 150 * time.Millisecond
	throughputPromotionPathCount = 32
	throughputWatcherBurstCount  = 32
)

func PolicyFor(req IndexRequest) PipelinePolicy {
	throughput := false
	switch req.LatencyClass {
	case LatencyThroughput:
		throughput = true
	case LatencyLowLatency:
		throughput = false
	default:
		switch req.Source {
		case IndexSourceFullScan:
			throughput = true
		case IndexSourceWatcher:
			if req.PathCount >= throughputWatcherBurstCount {
				// Watcher bursts are user-visible but already large enough that
				// tiny low-latency batches waste provider capacity.
				return watcherBurstPipelinePolicy()
			}
		default:
			throughput = req.PathCount >= throughputPromotionPathCount
		}
	}
	if throughput {
		return PipelinePolicy{
			Debounce: watcherPipelineDebounce,
			CodeEmbedPacker: &semantic.EmbedPackerOptions{
				MinTexts:         256,
				AdaptiveMinTexts: 256,
				MaxTexts:         512,
				MaxBytes:         384 << 10,
				MaxWait:          150 * time.Millisecond,
			},
			NoteEmbedPacker: &semantic.EmbedPackerOptions{
				MinTexts:         256,
				AdaptiveMinTexts: semantic.FullScanAdaptivePackerFloor,
				MaxTexts:         512,
				MaxBytes:         384 << 10,
				MaxWait:          150 * time.Millisecond,
			},
		}
	}
	return PipelinePolicy{
		Debounce: watcherPipelineDebounce,
		CodeEmbedPacker: &semantic.EmbedPackerOptions{
			MinTexts: 8,
			MaxTexts: 128,
			MaxBytes: 96 << 10,
			MaxWait:  75 * time.Millisecond,
		},
		NoteEmbedPacker: &semantic.EmbedPackerOptions{
			MinTexts: 8,
			MaxTexts: 128,
			MaxBytes: 96 << 10,
			MaxWait:  75 * time.Millisecond,
		},
	}
}

func watcherBurstPipelinePolicy() PipelinePolicy {
	return PipelinePolicy{
		Debounce: watcherPipelineDebounce,
		CodeEmbedPacker: &semantic.EmbedPackerOptions{
			MinTexts: 64,
			MaxTexts: 256,
			MaxBytes: 192 << 10,
			MaxWait:  150 * time.Millisecond,
		},
		NoteEmbedPacker: &semantic.EmbedPackerOptions{
			MinTexts: 64,
			MaxTexts: 256,
			MaxBytes: 192 << 10,
			MaxWait:  150 * time.Millisecond,
		},
	}
}
