// Package fakeupstream provides deterministic stand-ins for Voyage and
// TypeSafe for local runs and end-to-end tests. Markers in the request text,
// which the gateway forwards unchanged, inject faults:
//
//	__fault:slow:<ms>__         delay the response
//	__fault:status:<code>__     reply with that status after reading the request
//	__fault:drop__              read the request, then close the connection
//	__fault:retry_after:<s>__   reply 429 with Retry-After
//
// Both fakes count the requests they receive per path; Stats and GET /_stats
// report the counts so tests can assert there was no duplicate dispatch.
package fakeupstream

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// StatsPath serves the per-path request counts as JSON. It is not counted.
const StatsPath = "/_stats"

const maxBody = 64 << 20

type Options struct {
	// Key is the bearer token the fake requires.
	Key string
	// Delay is added to every counted request.
	Delay time.Duration
}

var faultPattern = regexp.MustCompile(`__fault:(slow|status|retry_after):(\d+)__|__fault:drop__`)

// fake holds what both providers share: auth, counting, and fault injection.
type fake struct {
	opts   Options
	mu     sync.Mutex
	counts map[string]int
}

func newFake(opts Options) fake { return fake{opts: opts, counts: map[string]int{}} }

// Stats returns a copy of the request counts per path.
func (f *fake) Stats() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]int, len(f.counts))
	for k, v := range f.counts {
		out[k] = v
	}
	return out
}

// begin counts the request, reads its body, checks the bearer key, and applies
// fault markers. It returns the body and false when it already responded.
func (f *fake) begin(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	f.mu.Lock()
	f.counts[r.URL.Path]++
	f.mu.Unlock()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return nil, false
	}
	if r.Header.Get("Authorization") != "Bearer "+f.opts.Key || f.opts.Key == "" {
		writeError(w, http.StatusUnauthorized, "invalid API key")
		return nil, false
	}
	if !sleep(r, f.opts.Delay) {
		return nil, false
	}
	var status, retryAfter int
	drop := false
	for _, m := range faultPattern.FindAllStringSubmatch(string(body), -1) {
		n, _ := strconv.Atoi(m[2])
		switch m[1] {
		case "slow":
			if !sleep(r, time.Duration(n)*time.Millisecond) {
				return nil, false
			}
		case "status":
			status = n
		case "retry_after":
			retryAfter = n
		default:
			drop = true
		}
	}
	switch {
	case drop:
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return nil, false
			}
		}
		panic(http.ErrAbortHandler) // HTTP/2: abort the stream without a response
	case retryAfter > 0:
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeError(w, http.StatusTooManyRequests, "fault injected: rate limited")
		return nil, false
	case status >= 100 && status <= 999:
		writeError(w, status, "fault injected")
		return nil, false
	}
	return body, true
}

// serveStats answers GET /_stats and reports whether it did.
func (f *fake) serveStats(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != StatsPath {
		return false
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	writeJSON(w, http.StatusOK, f.Stats())
	return true
}

func sleep(r *http.Request, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.Context().Done():
		return false
	}
}

func route(w http.ResponseWriter, r *http.Request, method, path string) bool {
	if r.URL.Path != path {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	if r.Method != method {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

func tokens(s string) int { return (len(s) + 3) / 4 }
