package codeintel

import (
	"context"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

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
