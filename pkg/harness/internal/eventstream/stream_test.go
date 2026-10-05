package eventstream

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestCloseDrainsQueuedEvents(t *testing.T) {
	stream := New()
	const count = 512
	for i := 0; i < count; i++ {
		require.True(t, stream.Emit(harness.Event{Kind: harness.EventAssistantTextDelta}))
	}
	require.True(t, stream.Emit(harness.Event{Kind: harness.EventTurnCompleted}))
	stream.Close()

	var events []harness.Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	require.Len(t, events, count+1)
	require.Equal(t, harness.EventTurnCompleted, events[len(events)-1].Kind)
	require.False(t, stream.Emit(harness.Event{}))
}

func TestAbortDrainsQueuedTerminalEvent(t *testing.T) {
	stream := New()
	require.True(t, stream.Emit(harness.Event{Kind: harness.EventError}))
	stream.Abort()
	require.Equal(t, harness.EventError, (<-stream.Events()).Kind)
	_, ok := <-stream.Events()
	require.False(t, ok)
}
