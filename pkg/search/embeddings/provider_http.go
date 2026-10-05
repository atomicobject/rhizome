package embeddings

// Docs: [Embeddings - providers + configuration](docs/reference/guides/Embeddings - providers + configuration.md)

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

const defaultHTTPTimeout = 30 * time.Second

func withDefaultTimeout(client *http.Client) *http.Client {
	return withHTTPTimeout(client, defaultHTTPTimeout)
}

func withHTTPTimeout(client *http.Client, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	if client != nil {
		if client.Timeout == 0 {
			client.Timeout = timeout
		}
		return client
	}
	return &http.Client{Timeout: timeout}
}

func doWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	return doWithRetryProvider(client, req, "")
}

func doWithRetryProvider(client *http.Client, req *http.Request, provider string) (*http.Response, error) {
	const maxAttempts = 2
	const backoff = 200 * time.Millisecond
	var lastErr error
	var bodyBytes []byte
	hasBody := req != nil && req.Body != nil
	if hasBody {
		var err error
		if req.GetBody != nil {
			body, getErr := req.GetBody()
			if getErr != nil {
				return nil, getErr
			}
			bodyBytes, err = io.ReadAll(body)
			_ = body.Close()
		} else {
			bodyBytes, err = io.ReadAll(req.Body)
			_ = req.Body.Close()
		}
		if err != nil {
			return nil, err
		}
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptReq := req
		if hasBody {
			attemptReq = req.Clone(req.Context())
			attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			attemptReq.ContentLength = int64(len(bodyBytes))
			attemptReq.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(bodyBytes)), nil
			}
		}
		var resp *http.Response
		var err error
		if provider == "" {
			resp, err = client.Do(attemptReq)
		} else {
			resp, err = doProviderHTTP(client, attemptReq, provider)
		}
		if err != nil {
			if resp != nil && resp.Body != nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			lastErr = err
		} else if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			var msg string
			if resp.Body != nil {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
				msg = strings.TrimSpace(string(body))
				_ = resp.Body.Close()
			}
			if msg != "" {
				lastErr = fmt.Errorf("http status %d: %s", resp.StatusCode, msg)
			} else {
				lastErr = fmt.Errorf("http status %d", resp.StatusCode)
			}
		} else {
			return resp, nil
		}
		if attempt < maxAttempts {
			if provider != "" {
				reason := "http"
				if err != nil {
					reason = "network"
				}
				indexingperf.AddCount(req.Context(), "provider.request."+provider+".retry."+reason, 1)
				_ = providerBackoff(req.Context(), "provider.request."+provider, reason, backoff, func(_ context.Context, d time.Duration) error { time.Sleep(d); return nil })
			} else {
				time.Sleep(backoff)
			}
		}
	}
	return nil, lastErr
}
