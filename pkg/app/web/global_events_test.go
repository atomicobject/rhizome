package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGlobalEventBrokerSubscribeAfterClose(t *testing.T) {
	b := newGlobalEventBroker()
	b.Close()
	events, unsubscribe := b.Subscribe()
	defer unsubscribe()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("closed broker delivered an event")
		}
	default:
		t.Fatal("subscription after close remains open")
	}
	unsubscribe()
	b.Close()
}

func TestGlobalEventBrokerDeliveryAndUnsubscribe(t *testing.T) {
	b := newGlobalEventBroker()
	defer b.Close()
	events, unsubscribe := b.Subscribe()
	b.Publish(GlobalEventIndexChanged, "updated")
	select {
	case event := <-events:
		if event.Kind != GlobalEventIndexChanged || event.Data != "updated" {
			t.Fatalf("unexpected event: %+v", event)
		}
	default:
		t.Fatal("event was not delivered")
	}
	unsubscribe()
	unsubscribe()
	b.Publish(GlobalEventIndexChanged, nil)
	if _, ok := <-events; ok {
		t.Fatal("unsubscribed channel remains open")
	}
}

func TestGlobalEventBrokerConcurrentCloseAndSubscribe(t *testing.T) {
	b := newGlobalEventBroker()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			events, unsubscribe := b.Subscribe()
			defer unsubscribe()
			b.Close()
			select {
			case _, ok := <-events:
				if ok {
					t.Error("closed broker delivered an event")
				}
			default:
				t.Error("subscription remains open after close")
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(b.subs) != 0 {
		t.Fatalf("closed broker retained %d subscriptions", len(b.subs))
	}
}

func TestGlobalEventsHandlerReturnsAfterBrokerClose(t *testing.T) {
	b := newGlobalEventBroker()
	b.Close()
	s := &Server{globalEvents: b}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleGlobalEvents(httptest.NewRecorder(), request)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("SSE request remained active after broker close")
	}
}

func TestGlobalEventsHoldTheRuntimeOnlyWhileTheStreamIsOpen(t *testing.T) {
	var held atomic.Int32
	b := newGlobalEventBroker()
	defer b.Close()
	s := &Server{globalEvents: b, cfg: Config{RuntimeControl: &RuntimeControl{Hold: func() func() {
		held.Add(1)
		return func() { held.Add(-1) }
	}}}}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleGlobalEvents(httptest.NewRecorder(), request)
	}()
	deadline := time.Now().Add(time.Second)
	for held.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if held.Load() != 1 {
		t.Fatal("an open UI event stream did not hold the runtime")
	}
	cancel()
	<-done
	if held.Load() != 0 {
		t.Fatal("a closed UI event stream still holds the runtime")
	}
}
