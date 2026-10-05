package agentchat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBrokerPersistedEventOverflowClosesSubscriber(t *testing.T) {
	broker := NewBroker()
	stream, unsubscribe := broker.Subscribe("agt-1")
	t.Cleanup(unsubscribe)
	for id := int64(1); id <= 32; id++ {
		broker.Publish(Event{ID: id, SessionID: "agt-1"})
	}
	broker.Publish(Event{ID: 33, SessionID: "agt-1"})

	var delivered int
	for range stream {
		delivered++
	}
	require.Equal(t, 32, delivered)
}

func TestBrokerDropsDiagnosticOnOverflowWithoutClosingSubscriber(t *testing.T) {
	broker := NewBroker()
	stream, unsubscribe := broker.Subscribe("agt-1")
	t.Cleanup(unsubscribe)
	for id := int64(1); id <= 32; id++ {
		broker.Publish(Event{ID: id, SessionID: "agt-1"})
	}
	broker.Publish(Event{ID: -1, SessionID: "agt-1", Type: "diagnostic"})

	_, ok := <-stream
	require.True(t, ok)
	broker.Publish(Event{ID: 33, SessionID: "agt-1"})
	for event := range stream {
		if event.ID == 33 {
			return
		}
	}
	t.Fatal("subscriber closed after diagnostic overflow")
}
