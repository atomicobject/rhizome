package lane

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFakeRunsJobAndReplaysEventsToLateSubscribers(t *testing.T) {
	fake := NewFake()
	handle, joined, err := fake.Submit(context.Background(), Request{Kind: KindExplicitIndex, Run: func(ctx context.Context, p Reporter) error {
		p.Segment("notes", 1, 2)
		p.Log("hello")
		return errors.New("boom")
	}})
	require.NoError(t, err)
	require.False(t, joined)
	<-handle.Done()
	require.EqualError(t, handle.Err(), "boom")

	events, cancel := handle.Subscribe()
	defer cancel()
	var types []EventType
	var last Event
	for e := range events {
		types = append(types, e.Type)
		last = e
	}
	require.Equal(t, []EventType{EventProgress, EventLog, EventDone}, types)
	require.Equal(t, OutcomeFailed, last.Outcome)

	found, ok := fake.Lookup(handle.ID())
	require.True(t, ok)
	require.Equal(t, KindExplicitIndex, found.Kind())
	require.Equal(t, "runtime/explicit-index", KindExplicitIndex.LockRole())
	require.False(t, KindExplicitIndex.Background())
	require.True(t, KindEmbedCycle.Background())

	fake.Close()
	_, _, err = fake.Submit(context.Background(), Request{Kind: KindGraphCycle})
	require.ErrorIs(t, err, ErrClosed)
}
