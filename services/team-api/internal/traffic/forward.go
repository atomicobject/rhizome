package traffic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// maxErrorBodyBytes bounds a non-2xx upstream body, which is read whole so the
// key can be redacted before any of it is sent.
const maxErrorBodyBytes = 1 << 20

// passHeaders are the only upstream response headers passed to the caller.
var passHeaders = []string{"Content-Type", "Retry-After", "retry-after-ms", "x-typesafe-request-id"}

// newTransport ignores environment proxies. RoundTrip never follows redirects.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// send makes the single upstream attempt. wrote reports whether any request
// byte may have been written, which makes a failure an unknown outcome.
func (t *Traffic) send(ctx context.Context, method, url, key string, body []byte) (resp *http.Response, wrote bool, err error) {
	var written atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteHeaderField: func(string, []string) { written.Store(true) },
		WroteHeaders:     func() { written.Store(true) },
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), method, url, reader)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err = t.transport.RoundTrip(req)
	return resp, written.Load(), err
}

// relayResult is what the caller received from an upstream response.
type relayResult struct {
	status   int
	bytesOut int64
	complete bool   // the whole body reached the caller
	usage    []byte // the top-level "usage" object of a complete 2xx body
}

// relay passes an upstream response through. 2xx bodies stream with the
// response cap; other bodies are read whole (bounded) and the key redacted.
// A nil result means nothing was written and the caller owns the error.
func relay(w http.ResponseWriter, resp *http.Response, key string) (*relayResult, *gwError) {
	defer resp.Body.Close()
	res := &relayResult{status: resp.StatusCode}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes+1))
		if err != nil || len(data) > maxErrorBodyBytes {
			return nil, fail(wire.CodeOutcomeUnknown, "upstream error response could not be read")
		}
		if key != "" {
			data = bytes.ReplaceAll(data, []byte(key), []byte("[REDACTED]"))
		}
		copyHeaders(w, resp)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(resp.StatusCode)
		n, err := w.Write(data)
		res.bytesOut, res.complete = int64(n), err == nil
		return res, nil
	}
	copyHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	var scan usageScanner
	n, err := io.Copy(w, io.TeeReader(io.LimitReader(resp.Body, wire.MaxResponseBytes), &scan))
	res.bytesOut = n
	if err != nil {
		return res, nil
	}
	// The body is complete only at a clean end: one more byte means it was
	// oversized, and a read error (a reset or timeout right at the limit)
	// means it was cut off. Either way the caller aborts the response.
	if extra, err := io.ReadFull(resp.Body, make([]byte, 1)); extra > 0 || !errors.Is(err, io.EOF) {
		return res, nil
	}
	res.complete = true
	res.usage = scan.result()
	return res, nil
}

func copyHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, name := range passHeaders {
		if v := resp.Header.Values(name); len(v) > 0 {
			w.Header()[http.CanonicalHeaderKey(name)] = v
		}
	}
}

// maxUsageBytes bounds the captured usage object.
const maxUsageBytes = 4 << 10

// usageScanner finds the top-level "usage" object of a streamed JSON body
// without buffering the rest. Escaped keys, a non-object value, a repeated
// key, or an oversized value leave the usage unknown.
type usageScanner struct {
	depth               int
	inStr, esc, inKey   bool
	atKey, wantUsage    bool
	capturing, overflow bool
	key, value, usage   []byte
	found               int
}

func (s *usageScanner) Write(p []byte) (int, error) {
	for _, c := range p {
		s.step(c)
	}
	return len(p), nil
}

func (s *usageScanner) step(c byte) {
	if s.capturing {
		if len(s.value) == maxUsageBytes {
			s.capturing, s.overflow = false, true
		} else {
			s.value = append(s.value, c)
		}
	}
	if s.inStr {
		switch {
		case s.esc:
			s.esc = false
		case c == '\\':
			s.esc = true
		case c == '"':
			s.inStr = false
			s.inKey = false
			return
		}
		if s.inKey && len(s.key) < 16 {
			s.key = append(s.key, c)
		}
		return
	}
	switch c {
	case '"':
		s.inStr = true
		if s.depth == 1 && s.atKey {
			s.inKey, s.key = true, s.key[:0]
		}
	case '{', '[':
		if s.depth == 1 && c == '{' && s.wantUsage {
			s.capturing, s.value = true, append(s.value[:0], c)
		}
		s.wantUsage = false
		s.depth++
		s.atKey = s.depth == 1 && c == '{'
	case '}', ']':
		s.depth--
		if s.depth == 1 && s.capturing {
			s.capturing = false
			s.usage = append([]byte(nil), s.value...)
			s.found++
		}
	case ':':
		if s.depth == 1 {
			s.atKey, s.wantUsage = false, string(s.key) == "usage"
		}
	case ',':
		if s.depth == 1 {
			s.atKey, s.wantUsage = true, false
		}
	case ' ', '\t', '\n', '\r':
	default:
		if s.depth == 1 {
			s.wantUsage = false
		}
	}
}

func (s *usageScanner) result() []byte {
	if s.depth != 0 || s.found != 1 || s.overflow {
		return nil
	}
	return s.usage
}
