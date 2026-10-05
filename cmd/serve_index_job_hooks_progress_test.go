package cmd

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/stretchr/testify/require"
)

func TestLaneProgressBarCarriesPipelineSegmentLabels(t *testing.T) {
	fake := lane.NewFake()
	handle, _, err := fake.Submit(context.Background(), lane.Request{Kind: lane.KindExplicitIndex, Run: func(ctx context.Context, p lane.Reporter) error {
		bar := &laneProgressBar{progress: p}
		bar.Println("Indexing code")
		bar.Update(1, 4)
		bar.SetLabel("Flushing note ingest")
		bar.Update(3, 4)
		bar.SetLabel("   ")
		bar.Update(4, 4)
		return nil
	}})
	require.NoError(t, err)
	<-handle.Done()

	events, cancel := handle.Subscribe()
	defer cancel()
	var labels []string
	for e := range events {
		if e.Type == lane.EventProgress {
			labels = append(labels, e.Label)
		}
	}
	require.Equal(t, []string{"Indexing code", "Flushing note ingest", "Flushing note ingest"}, labels)
}
