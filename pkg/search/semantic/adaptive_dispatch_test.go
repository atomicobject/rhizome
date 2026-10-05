package semantic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdaptiveDispatchDrainsBelowMinTexts(t *testing.T) {
	t.Parallel()

	decision := adaptiveDispatch(adaptiveDispatchInput{
		Opts: EmbedPackerOptions{
			MinTexts:         256,
			AdaptiveMinTexts: 256,
			MaxTexts:         512,
			MaxBytes:         1 << 20,
			MaxWait:          time.Hour,
		},
		Pending:       3,
		PendingBytes:  12,
		MaxConcurrent: 16,
		Draining:      true,
	})

	require.True(t, decision.Ready)
	require.Equal(t, 1, decision.Threshold)
	require.Equal(t, 512, decision.BatchTexts)
	require.False(t, decision.Warmup)
}

func TestAdaptiveDispatchWarmsThenRequiresFullness(t *testing.T) {
	t.Parallel()

	opts := EmbedPackerOptions{
		MinTexts:         256,
		AdaptiveMinTexts: 256,
		MaxTexts:         512,
		MaxBytes:         1 << 20,
		MaxWait:          time.Hour,
	}
	warm := adaptiveDispatch(adaptiveDispatchInput{
		Opts:          opts,
		Pending:       256,
		PendingBytes:  256,
		MaxConcurrent: 16,
		Warmups:       0,
	})
	require.True(t, warm.Ready)
	require.Equal(t, 256, warm.Threshold)
	require.Equal(t, 256, warm.BatchTexts)
	require.True(t, warm.Warmup)

	steady := adaptiveDispatch(adaptiveDispatchInput{
		Opts:          opts,
		Pending:       256,
		PendingBytes:  256,
		MaxConcurrent: 16,
		Warmups:       adaptiveWarmupBudget(16),
	})
	require.True(t, steady.Ready)
	require.Equal(t, 256, steady.Threshold)
	require.Equal(t, 256, steady.BatchTexts)
	require.False(t, steady.Warmup)
}

func TestAdaptiveDispatchTargetsAvailableProviderSlots(t *testing.T) {
	t.Parallel()

	opts := EmbedPackerOptions{
		MinTexts:         256,
		AdaptiveMinTexts: 256,
		MaxTexts:         1000,
		MaxBytes:         4 << 20,
		MaxWait:          time.Hour,
	}
	decision := adaptiveDispatch(adaptiveDispatchInput{
		Opts:          opts,
		Pending:       12633,
		PendingBytes:  12633,
		MaxConcurrent: 32,
		Warmups:       adaptiveWarmupBudget(32),
	})

	require.True(t, decision.Ready)
	require.Equal(t, 395, decision.Threshold)
	require.Equal(t, 395, decision.BatchTexts)
	require.False(t, decision.Warmup)
}

func TestAdaptiveDispatchAgesTowardLowerThresholds(t *testing.T) {
	t.Parallel()

	opts := EmbedPackerOptions{
		MinTexts:         256,
		AdaptiveMinTexts: 256,
		MaxTexts:         512,
		MaxBytes:         1 << 20,
		MaxWait:          200 * time.Millisecond,
	}
	for _, tc := range []struct {
		name      string
		age       time.Duration
		threshold int
		batch     int
	}{
		{"young", 0, 400, 400},
		{"half wait", 100 * time.Millisecond, 256, 256},
		{"full wait", 200 * time.Millisecond, 1, 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decision := adaptiveDispatch(adaptiveDispatchInput{
				Opts: opts, Pending: 400, PendingBytes: 400,
				Active: 31, MaxConcurrent: 32, Warmups: adaptiveWarmupBudget(32),
				OldestAge: tc.age,
			})
			require.True(t, decision.Ready)
			require.Equal(t, tc.threshold, decision.Threshold)
			require.Equal(t, tc.batch, decision.BatchTexts)
			require.False(t, decision.Warmup)
		})
	}
}
