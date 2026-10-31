package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An unauthenticated client that sends headers and then stalls its body must
// not hold the connection open: the read gives up at the deadline.
func TestDecodeJSONGivesUpOnAStalledBody(t *testing.T) {
	old := bodyReadTimeout
	bodyReadTimeout = 200 * time.Millisecond
	t.Cleanup(func() { bodyReadTimeout = old })
	done := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		done <- DecodeJSON(w, r, &v)
	}))
	t.Cleanup(srv.Close)
	body, stall := io.Pipe()
	t.Cleanup(func() { _ = stall.Close() })
	go func() { _, _ = stall.Write([]byte(`{"refresh_token":`)) }() // never finished
	req, _ := http.NewRequest(http.MethodPost, srv.URL, body)
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a stalled body decoded successfully")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the body read never timed out")
	}
}
