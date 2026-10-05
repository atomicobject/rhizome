package codeintel

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

type writeQueueKey struct{}
type codeSemanticBatchSubmitterKey struct{}

type WriteQueue interface {
	SubmitCodeIndexWork(context.Context, codeanchor.CodeIndexWork) error
	SubmitNoteIndexWork(context.Context, codeanchor.NoteIndexWork) error
	FlushAndWait(context.Context) error
}

type CodeSemanticBatchSubmitter interface {
	SubmitPreparedCodeBatch(context.Context, []codeanchor.CodeIndexWork) error
}

type CodeSemanticWorkSubmitter interface {
	SubmitCodeIndexWork(context.Context, codeanchor.CodeIndexWork) error
}

// WithWriteQueue installs the shared writer lane used by unified indexing.
//
// Code and note ingest can run in separate worker pools, but SQLite remains a
// single-writer bottleneck. Passing the queue through context keeps worker code
// decoupled from orchestration while preserving one durability boundary.
func WithWriteQueue(ctx context.Context, q WriteQueue) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if q == nil {
		return ctx
	}
	return context.WithValue(ctx, writeQueueKey{}, q)
}

func writeQueueFromContext(ctx context.Context) WriteQueue {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(writeQueueKey{}); v != nil {
		if q, ok := v.(WriteQueue); ok {
			return q
		}
	}
	return nil
}

func WithCodeSemanticBatchSubmitter(ctx context.Context, submitter CodeSemanticBatchSubmitter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if submitter == nil {
		return ctx
	}
	return context.WithValue(ctx, codeSemanticBatchSubmitterKey{}, submitter)
}

func codeSemanticBatchSubmitterFromContext(ctx context.Context) CodeSemanticBatchSubmitter {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(codeSemanticBatchSubmitterKey{}); v != nil {
		if submitter, ok := v.(CodeSemanticBatchSubmitter); ok {
			return submitter
		}
	}
	return nil
}

func codeSemanticWorkSubmitterFromContext(ctx context.Context) CodeSemanticWorkSubmitter {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value(codeSemanticBatchSubmitterKey{}); v != nil {
		if submitter, ok := v.(CodeSemanticWorkSubmitter); ok {
			return submitter
		}
	}
	return nil
}
