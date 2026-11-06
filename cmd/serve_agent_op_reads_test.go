package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/stretchr/testify/require"
)

func TestCodeModeSourcesReadyPreservesCallerAndOwnerCancellation(t *testing.T) {
	permanent := errors.New("source read failed")
	for _, test := range []struct {
		name       string
		err        error
		cancelCall bool
		wantReady  bool
		wantErr    error
	}{
		{name: "ready", wantReady: true},
		{name: "watcher pending", err: bootstrap.ErrWatcherPending},
		{name: "owner canceled", err: context.Canceled},
		{name: "owner preempted", err: lane.ErrPreempted},
		{name: "wrapped preemption", err: errors.Join(lane.ErrPreempted, context.Canceled)},
		{name: "permanent failure", err: permanent, wantErr: permanent},
		{name: "caller canceled", err: context.Canceled, cancelCall: true, wantErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancelCall {
				cancel()
			}
			ready, err := codeModeSourcesReady(ctx, test.err)
			require.Equal(t, test.wantReady, ready)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
