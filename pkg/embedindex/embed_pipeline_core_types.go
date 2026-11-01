package embedindex

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type embedPipelineOwnerState interface {
	pipelineReadyAt() time.Time
	pipelinePending() int
	pipelineSetPending(int)
}

type embedPipelineJobState[K comparable] interface {
	pipelineOwnerKey() K
	pipelineText() string
	pipelineBytes() int
}

type embedPipelineBatchResult[Owner any, Job any] struct {
	jobs []queuedEmbedJob[Owner, Job]
	vecs []embeddings.Embedding
	err  error
}

type queuedEmbedJob[Owner any, Job any] struct {
	owner      Owner
	job        Job
	enqueuedAt time.Time
}

type embedPipelineCore[K comparable, Prepared any, Owner embedPipelineOwnerState, Job embedPipelineJobState[K]] struct {
	ctx           context.Context
	provider      embeddings.Provider
	sharedNode    *SharedEmbeddingNode
	maxConcurrent int
	globalSem     chan struct{}
	opts          EmbedPackerOptions

	preparedCh chan Prepared
	resultCh   chan embedPipelineBatchResult[Owner, Job]

	readyMaxTexts int
	readyMaxBytes int
	completeStat  indexingperf.MetricName

	acceptPrepared func(Prepared) (K, Owner, []Job)
	applyResult    func(Owner, Job, embeddings.Embedding) error
	finalize       func(Owner) error
	// abandon, when set, receives the owners a failed run leaves unfinalized,
	// with every vector the provider returned applied, so paid vectors can be
	// kept without publishing incomplete owners.
	abandon func([]Owner)
}

func newEmbedPipelineCoreWithNode[K comparable, Prepared any, Owner embedPipelineOwnerState, Job embedPipelineJobState[K]](
	ctx context.Context,
	provider embeddings.Provider,
	sharedNode *SharedEmbeddingNode,
	maxConcurrent int,
	globalSem chan struct{},
	opts EmbedPackerOptions,
	preparedCh chan Prepared,
	acceptPrepared func(Prepared) (K, Owner, []Job),
	applyResult func(Owner, Job, embeddings.Embedding) error,
	finalize func(Owner) error,
) *embedPipelineCore[K, Prepared, Owner, Job] {
	if maxConcurrent < 1 {
		maxConcurrent = EffectiveMaxConcurrent(provider, 0)
	}
	opts = normalizeEmbedPackerOptions(opts)
	return &embedPipelineCore[K, Prepared, Owner, Job]{
		ctx:            ctx,
		provider:       provider,
		sharedNode:     sharedNode,
		maxConcurrent:  maxConcurrent,
		globalSem:      globalSem,
		opts:           opts,
		preparedCh:     preparedCh,
		resultCh:       make(chan embedPipelineBatchResult[Owner, Job], maxConcurrent*2),
		readyMaxTexts:  defaultCodePipelineReadyTexts,
		readyMaxBytes:  defaultCodePipelineReadyBytes,
		completeStat:   indexingperf.CodeembedCompleteWait,
		acceptPrepared: acceptPrepared,
		applyResult:    applyResult,
		finalize:       finalize,
	}
}
