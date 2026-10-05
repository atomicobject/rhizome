package semantic

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

const (
	minPreparedEmbedQueueSize = 256
	maxPreparedEmbedQueueSize = 16384
)

// preparedEmbedStage decouples expensive chunk planning from provider dispatch.
// The queue is intentionally throughput-scaled so full scans can keep provider
// slots busy without letting prepared work grow without bound.
type preparedEmbedStage[K comparable, Prepared any, Owner embedPipelineOwnerState, Job embedPipelineJobState[K]] struct {
	preparedCh chan Prepared
	core       *embedPipelineCore[K, Prepared, Owner, Job]
}

// preparedEmbedQueueSize sizes prepared-work buffering from provider capacity,
// with a floor for small watcher batches and a cap for large full scans.
func preparedEmbedQueueSize(maxConcurrent int, opts EmbedPackerOptions) int {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	maxTexts := opts.MaxTexts
	if maxTexts <= 0 {
		maxTexts = defaultCodeEmbedPackerMaxTexts
	}
	size := maxConcurrent * maxTexts
	if size < minPreparedEmbedQueueSize {
		size = minPreparedEmbedQueueSize
	}
	if size > maxPreparedEmbedQueueSize {
		size = maxPreparedEmbedQueueSize
	}
	return size
}

func newPreparedEmbedStage[K comparable, Prepared any, Owner embedPipelineOwnerState, Job embedPipelineJobState[K]](
	ctx context.Context,
	provider embeddings.Provider,
	sharedNode *SharedEmbeddingNode,
	maxConcurrent int,
	globalSem chan struct{},
	opts EmbedPackerOptions,
	acceptPrepared func(Prepared) (K, Owner, []Job),
	applyResult func(Owner, Job, embeddings.Embedding) error,
) *preparedEmbedStage[K, Prepared, Owner, Job] {
	preparedQueueSize := preparedEmbedQueueSize(maxConcurrent, opts)
	preparedCh := make(chan Prepared, preparedQueueSize)
	stage := &preparedEmbedStage[K, Prepared, Owner, Job]{preparedCh: preparedCh}
	stage.core = newEmbedPipelineCoreWithNode(
		ctx,
		provider,
		sharedNode,
		maxConcurrent,
		globalSem,
		opts,
		preparedCh,
		acceptPrepared,
		applyResult,
		func(Owner) error { return nil },
	)
	return stage
}

func (s *preparedEmbedStage[K, Prepared, Owner, Job]) run(finalize func(Owner) error) error {
	if s == nil || s.core == nil {
		return nil
	}
	s.core.finalize = finalize
	return s.core.run()
}
