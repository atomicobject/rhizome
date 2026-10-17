package embedindex

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/require"
)

type blockedNoteMetadataBatch struct {
	embeddings.Index
	started chan struct{}
	release chan struct{}
}

func (s *blockedNoteMetadataBatch) UpsertNoteMetaBatch(context.Context, []embeddings.NoteFileInfo) error {
	close(s.started)
	<-s.release
	return errors.New("metadata batch failed")
}

func TestNoteMetadataBatchReleasesQueuedSubmitterOnFailureOrCancellation(t *testing.T) {
	for _, externalCancellation := range []bool{false, true} {
		name := "batch failure"
		if externalCancellation {
			name = "caller cancellation"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			collector := indexingperf.New()
			ctx = indexingperf.WithCollector(ctx, collector)
			index := &blockedNoteMetadataBatch{started: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(index.release) }) }
			syncer := NoteSyncer{Index: index}
			submit, finish := syncer.planMetadataWrites(ctx, cancel, make(chan error, 1))
			defer func() { cancel(); release(); finish() }()
			first := make(chan error, 1)
			go func() { first <- submit(embeddings.NoteFileInfo{ID: "first"}, false) }()
			select {
			case <-index.started:
			case <-time.After(time.Second):
				t.Fatal("metadata batch did not begin")
			}
			second := make(chan error, 1)
			go func() { second <- submit(embeddings.NoteFileInfo{ID: "second"}, false) }()
			// Admission is measured after the send. Two admissions while the
			// first batch is held prove the second note is queued behind it.
			require.Eventually(t, func() bool {
				for _, metric := range collector.Snapshot().Latencies {
					if metric.Name == string(indexingperf.NoteplanWritebackMetaSubmitWait) {
						return metric.Count == 2
					}
				}
				return false
			}, time.Second, time.Millisecond)
			if externalCancellation {
				cancel()
			} else {
				release()
			}
			select {
			case err := <-second:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("queued metadata submission did not stop after failure or cancellation")
			}
			release()
			select {
			case err := <-first:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("active metadata submission did not finish")
			}
		})
	}
}
