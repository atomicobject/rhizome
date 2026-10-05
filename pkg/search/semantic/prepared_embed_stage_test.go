package semantic

import (
	"testing"
	"time"
)

func TestPreparedEmbedQueueSizeUsesThroughputScaledCapacity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		workers int
		options EmbedPackerOptions
		want    int
	}{
		{"throughput ceiling", 32, EmbedPackerOptions{MinTexts: 256, MaxTexts: 512, MaxBytes: 384 << 10, MaxWait: 200 * time.Millisecond}, 16384},
		{"small workload floor", 1, EmbedPackerOptions{MinTexts: 8, MaxTexts: 32, MaxBytes: 96 << 10, MaxWait: 75 * time.Millisecond}, 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if size := preparedEmbedQueueSize(tc.workers, tc.options); size != tc.want {
				t.Fatalf("prepared queue size=%d want %d", size, tc.want)
			}
		})
	}
}
