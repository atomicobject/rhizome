package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// Credentials and URL authentication/query/fragment never participate in identity.
func providerFingerprint(provider, model, endpoint string, dimensions int) string {
	if parsed, err := url.Parse(endpoint); err == nil {
		parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
		endpoint = parsed.String()
	} else {
		endpoint = "invalid-endpoint"
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{provider, model, endpoint, strconv.Itoa(dimensions)}, "\x00")))
	return hex.EncodeToString(digest[:])
}

// doProviderHTTP records physical attempts and finite failure classes. It never
// records request arguments, endpoint URLs, response bodies, or error strings.
// attempt_latency measures Do until response headers or transport failure; body
// reads have separate latency and byte measurements.
func doProviderHTTP(client *http.Client, req *http.Request, provider string) (*http.Response, error) {
	ctx := req.Context()
	prefix := "provider.request." + provider
	indexingperf.AddCount(ctx, prefix+".attempt", 1)
	indexingperf.AddBytes(ctx, prefix+".sent", max(req.ContentLength, 0))
	started := time.Now()
	resp, err := client.Do(req)
	indexingperf.ObserveLatency(ctx, prefix+".attempt_latency", time.Since(started))
	if resp != nil {
		status := "other"
		if resp.StatusCode >= 100 && resp.StatusCode <= 599 {
			status = strconv.Itoa(resp.StatusCode)
		}
		indexingperf.AddCount(ctx, prefix+".status."+status, 1)
		if resp.Body != nil {
			resp.Body = &observedProviderBody{ReadCloser: resp.Body, ctx: ctx, prefix: prefix}
		}
	}
	if err != nil {
		indexingperf.AddCount(ctx, prefix+".transport."+providerErrorClass(err), 1)
	}
	return resp, err
}

func providerErrorClass(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	if isTransientNetworkError(err) {
		return "transient"
	}
	return "other"
}

type observedProviderBody struct {
	io.ReadCloser
	ctx    context.Context
	prefix string
}

func (b *observedProviderBody) Read(p []byte) (int, error) {
	started := time.Now()
	n, err := b.ReadCloser.Read(p)
	indexingperf.ObserveLatency(b.ctx, b.prefix+".body_read", time.Since(started))
	if err != nil && !errors.Is(err, io.EOF) {
		indexingperf.AddCount(b.ctx, b.prefix+".body_read.error."+providerErrorClass(err), 1)
	}
	indexingperf.AddBytes(b.ctx, b.prefix+".received", int64(n))
	return n, err
}

func observeProviderOutcome(ctx context.Context, provider string, err error) {
	outcome := "ok"
	if err != nil {
		outcome = providerErrorClass(err)
		if outcome == "other" {
			outcome = "error"
		}
	}
	indexingperf.AddCount(ctx, "provider.logical."+provider+".outcome."+outcome, 1)
}

func providerBackoff(ctx context.Context, prefix, reason string, delay time.Duration, sleep func(context.Context, time.Duration) error) error {
	indexingperf.AddCount(ctx, prefix+".backoff.reason."+reason, 1)
	indexingperf.ObserveLatency(ctx, prefix+".backoff_scheduled", delay)
	started := time.Now()
	err := sleep(ctx, delay)
	indexingperf.ObserveLatency(ctx, prefix+".backoff", time.Since(started))
	if err != nil {
		indexingperf.AddCount(ctx, prefix+".backoff.interrupted."+providerErrorClass(err), 1)
	}
	return err
}
