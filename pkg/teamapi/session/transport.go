package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// Transport authorizes requests to the session origin. It refuses any other
// origin and any redirect, returns gateway-originated errors as
// *wire.GatewayError, replays once after access_refresh_required when the
// request body can be rebuilt, and deletes the record when the gateway ends
// the session.
func (a *Account) Transport() http.RoundTripper { return transport{a} }

// HTTPClient is a client over Transport that also refuses redirects.
func (a *Account) HTTPClient() *http.Client {
	return &http.Client{Transport: a.Transport(), CheckRedirect: refuseRedirect}
}

type transport struct{ a *Account }

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	a, ctx := t.a, req.Context()
	if got := requestOrigin(req.URL); got != a.origin {
		closeBody(req.Body)
		return nil, fmt.Errorf("%w: %s", ErrForeignOrigin, got)
	}
	rec, err := a.current(ctx)
	if err != nil {
		closeBody(req.Body)
		return nil, err
	}
	resp, err := a.send(req, req.Body, rec.AccessToken)
	var gerr *wire.GatewayError
	if !errors.As(err, &gerr) {
		return resp, err
	}
	if gerr.Code == wire.CodeAccessRefreshRequired {
		if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
			return nil, gerr
		}
		if rec, err = a.refresh(ctx, rec.AccessToken); err != nil {
			return nil, err
		}
		body := req.Body
		if req.GetBody != nil {
			if body, err = req.GetBody(); err != nil {
				return nil, err
			}
		}
		event(ctx, slog.LevelInfo, "gateway.replay", slog.String("route", req.URL.Path))
		resp, err = a.send(req, body, rec.AccessToken)
		if !errors.As(err, &gerr) {
			return resp, err
		}
	}
	if errors.Is(gerr, wire.ErrSignInRequired) {
		a.clear(ctx, rec.Generation, rec.SessionID)
	}
	return nil, gerr
}

func (a *Account) send(req *http.Request, body io.ReadCloser, token string) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.Body = body
	// net/http silently replays a request that carries Idempotency-Key and a
	// GetBody when a reused connection fails. The caller would never learn
	// that an attempt may have been dispatched, and could renew the key after
	// a later rejection and pay twice. Without GetBody the failure surfaces
	// as an ambiguous error; this transport's own replay uses req.GetBody.
	out.GetBody = nil
	out.Header.Set("Authorization", "Bearer "+token)
	if a.userAgent != "" {
		out.Header.Set("User-Agent", a.userAgent)
	}
	resp, err := a.base.RoundTrip(out)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		closeBody(resp.Body)
		return nil, ErrRedirect
	}
	if err := ResponseError(resp); err != nil {
		var gerr *wire.GatewayError
		if errors.As(err, &gerr) {
			event(req.Context(), slog.LevelWarn, "gateway.error", slog.String("code", string(gerr.Code)),
				slog.Int("status", gerr.Status), slog.String("route", req.URL.Path))
		}
		return nil, err
	}
	return resp, nil
}

// ResponseError returns a *wire.GatewayError for a response that carries
// wire.HeaderGatewayError, closing its body, or nil for any other response.
func ResponseError(resp *http.Response) error {
	header := resp.Header.Get(wire.HeaderGatewayError)
	if header == "" {
		return nil
	}
	defer closeBody(resp.Body)
	var body wire.ErrorBody
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	gerr := &wire.GatewayError{Code: body.Error.Code, Status: resp.StatusCode, Message: body.Error.Message}
	if gerr.Code == "" {
		gerr.Code = wire.ErrorCode(header)
	}
	if value := resp.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			gerr.RetryAfter = time.Duration(seconds) * time.Second
		} else if at, err := http.ParseTime(value); err == nil {
			gerr.RetryAfter = max(time.Until(at), 0)
		}
	}
	return gerr
}

// Call sends one JSON request and decodes a 2xx body into out (if not nil).
// token, when set, becomes the bearer credential; leave it empty with a
// client from Account.HTTPClient, which adds its own.
func Call(ctx context.Context, client *http.Client, method, url, token, userAgent string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		var gerr *wire.GatewayError
		if errors.As(err, &gerr) {
			return gerr // drop *url.Error, whose text repeats the URL
		}
		return err
	}
	defer closeBody(resp.Body)
	if err := ResponseError(resp); err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("team API %s %s: HTTP %d", method, req.URL.Path, resp.StatusCode)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	// An empty successful body leaves out untouched. io.EOF here means the
	// gateway answered with no body, not a lost connection: that fails earlier
	// or as an unexpected EOF.
	if err := json.NewDecoder(io.LimitReader(resp.Body, wire.MaxResponseBytes)).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("team API %s %s: malformed response: %w", method, req.URL.Path, err)
	}
	return nil
}

func closeBody(body io.ReadCloser) {
	if body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(body, 64<<10))
		_ = body.Close()
	}
}
