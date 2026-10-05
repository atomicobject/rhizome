package indexing

import (
	"context"
	"errors"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
)

func refreshValidationProjectionOnLane(ctx context.Context, request ValidationProjectionRequest) (*ValidationProjectionResult, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var result *ValidationProjectionResult
		handle, _, err := request.Lane.Submit(ctx, lane.Request{
			Kind: lane.KindValidationRefresh,
			Run: func(jobCtx context.Context, _ lane.Reporter) error {
				runCtx, cancel := context.WithCancel(jobCtx)
				stop := context.AfterFunc(ctx, cancel)
				defer func() { stop(); cancel() }()
				if err := ctx.Err(); err != nil {
					return err
				}
				if request.BeforeMutation != nil {
					if err := request.BeforeMutation(runCtx); err != nil {
						return err
					}
				}
				var err error
				result, err = refreshValidationProjectionWithHeldIndexLock(runCtx, request)
				return err
			},
		})
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			handle.Cancel()
			<-handle.Done()
		case <-handle.Done():
		}
		err = handle.Err()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err == nil {
			return result, nil
		}
		if result != nil {
			_ = result.Close()
		}
		if ctx.Err() != nil || !errors.Is(err, context.Canceled) {
			return nil, err
		}
		// Interactive indexing or editing displaced this projection. Queue a new
		// pass after it, retaining the caller's validation generation.
	}
}
