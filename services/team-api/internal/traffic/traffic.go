// Package traffic serves the gateway's provider routes: it validates each paid
// call, admits it through concurrency slots and the budget ledger, forwards
// the original bytes with the server-held key, passes the response through,
// and settles the reservation.
package traffic

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/ledger"
)

const (
	DefaultVoyageBaseURL   = "https://api.voyageai.com"
	DefaultTypeSafeBaseURL = "https://api.typesafe.ai"
	defaultTypeSafeModel   = "jev-latest"
	userAgent              = "rhizome-team-api/1"
	// bodyReadTimeout bounds reading one paid request body (the plan's 30 s).
	bodyReadTimeout = 30 * time.Second
)

// Charge is a model's conservative reservation bound and settlement rate, in
// integer credits. The reservation is PerRequest + PerInput*inputs +
// PerKiBInput*ceil(body bytes/1024). Settlement converts actual tokens at
// PerKToken credits per 1,000 tokens, rounded up.
type Charge struct {
	PerRequest  int64 `json:"per_request" yaml:"per_request"`
	PerKiBInput int64 `json:"per_kib_input" yaml:"per_kib_input"`
	PerInput    int64 `json:"per_input" yaml:"per_input"`
	PerKToken   int64 `json:"per_ktoken" yaml:"per_ktoken"`
}

func (c Charge) reserve(bytes int64, inputs int) int64 {
	return c.PerRequest + c.PerInput*int64(inputs) + c.PerKiBInput*((bytes+1023)/1024)
}

func (c Charge) credits(tokens int64) int64 {
	return int64(math.Ceil(float64(tokens) * float64(c.PerKToken) / 1000))
}

// Default charges count one credit per provider token until X7 settles priced
// bounds. Input tokens are bounded by input bytes (no tokenizer emits more
// than one token per byte of JSON), so PerKiBInput is 1,024.
//   - voyage-4-lite: 8 credits per input covers per-input special tokens;
//     embeddings have no output tokens.
//   - jev-latest: 4,096 per request covers System One's prompt overhead and
//     512 per question covers its instructions and answer tokens. This
//     assumes the state is billed once per request, not once per question.
var (
	DefaultVoyageModels   = map[string]Charge{"voyage-4-lite": {PerKiBInput: 1024, PerInput: 8, PerKToken: 1000}}
	DefaultTypeSafeModels = map[string]Charge{defaultTypeSafeModel: {PerRequest: 4096, PerKiBInput: 1024, PerInput: 512, PerKToken: 1000}}
)

// Config configures the provider routes. Zero values take the defaults.
type Config struct {
	// Provider keys. A provider without a key answers service_disabled.
	VoyageKey, TypeSafeKey string
	// Upstream origins; the identity lane allows overrides only in dev mode.
	VoyageBaseURL, TypeSafeBaseURL string
	// Allowed models and their charges, merged over the defaults.
	VoyageModels, TypeSafeModels map[string]Charge
	// Concurrent paid calls per person (4) and in total (8).
	PersonSlots, GlobalSlots int
	// Longest total wait for both slots before rate_limited (5s).
	SlotWait time.Duration
	// Paid requests whose bodies are being read or held in memory at once
	// (8): a permit lasts from the start of the read until the request ends,
	// with the same bounded wait, separate from paid admission.
	BodyReaders int
	// Upstream deadlines (150s Voyage, 60s TypeSafe).
	VoyageTimeout, TypeSafeTimeout time.Duration
}

// Traffic serves the provider routes. Create one per process.
type Traffic struct {
	voyage, typesafe *provider
	modelsURL        string
	runtime          core.RuntimeSource
	ledger           *ledger.Ledger
	slots            *slots
	ingress          *gate
	transport        http.RoundTripper
	rec              *diagnostics.Recorder
	now              func() time.Time
}

type provider struct {
	route, url, key string
	timeout         time.Duration
	maxBody         int64
	models          map[string]Charge
	validate        func([]byte, map[string]Charge) (string, int, *gwError)
	enabled         func(core.Runtime) bool
	tokens          func(usage []byte) (int64, bool)
}

// New migrates the ledger tables in db and marks receipts a previous process
// left in flight as unknown outcomes. rec may be nil; now defaults to
// time.Now and decides the UTC day.
func New(cfg Config, db *sql.DB, runtime core.RuntimeSource, rec *diagnostics.Recorder, now func() time.Time) (*Traffic, error) {
	if db == nil || runtime == nil {
		return nil, errors.New("traffic: db and runtime source are required")
	}
	if now == nil {
		now = time.Now
	}
	voyageBase, err := baseURL(cfg.VoyageBaseURL, DefaultVoyageBaseURL)
	if err != nil {
		return nil, err
	}
	typesafeBase, err := baseURL(cfg.TypeSafeBaseURL, DefaultTypeSafeBaseURL)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	l, err := ledger.Open(ctx, db, now)
	if err != nil {
		return nil, err
	}
	if err := l.Recover(ctx); err != nil {
		return nil, fmt.Errorf("traffic: recover receipts: %w", err)
	}
	t := &Traffic{
		voyage: &provider{
			route: wire.RouteVoyageEmbed, url: voyageBase + "/v1/embeddings", key: cfg.VoyageKey,
			timeout: or(cfg.VoyageTimeout, 150*time.Second), maxBody: wire.MaxVoyageRequestBytes,
			models: merge(DefaultVoyageModels, cfg.VoyageModels), validate: validateVoyage,
			enabled: func(rt core.Runtime) bool { return rt.VoyageEnabled },
			tokens:  voyageTokens,
		},
		typesafe: &provider{
			route: wire.RouteTypeSafeEval, url: typesafeBase + "/v1/systemone", key: cfg.TypeSafeKey,
			timeout: or(cfg.TypeSafeTimeout, 60*time.Second), maxBody: wire.MaxTypeSafeRequestBytes,
			models: merge(DefaultTypeSafeModels, cfg.TypeSafeModels), validate: validateTypeSafe,
			enabled: func(rt core.Runtime) bool { return rt.TypeSafeEnabled },
			tokens:  typesafeTokens,
		},
		modelsURL: typesafeBase + "/v1/models",
		runtime:   runtime,
		ledger:    l,
		slots:     newSlots(or(cfg.PersonSlots, 4), or(cfg.GlobalSlots, 8), or(cfg.SlotWait, 5*time.Second)),
		ingress:   newGate(or(cfg.BodyReaders, 8), or(cfg.SlotWait, 5*time.Second)),
		transport: newTransport(),
		rec:       rec,
		now:       now,
	}
	return t, nil
}

func or[T comparable](v, fallback T) T {
	var zero T
	if v == zero {
		return fallback
	}
	return v
}

func merge(defaults, extra map[string]Charge) map[string]Charge {
	out := make(map[string]Charge, len(defaults)+len(extra))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func baseURL(raw, fallback string) (string, error) {
	if raw == "" {
		return fallback, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("traffic: upstream base URL must be an http(s) origin")
	}
	return strings.TrimRight(raw, "/"), nil
}

// Routes registers the provider routes. Wrap mux with the auth middleware:
// these handlers require a core.Principal in the request context.
func (t *Traffic) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+wire.RouteVoyageEmbed, t.paid(t.voyage))
	mux.HandleFunc("POST "+wire.RouteTypeSafeEval, t.paid(t.typesafe))
	mux.HandleFunc("GET "+wire.RouteTypeSafeModel, t.models)
	mux.HandleFunc("/v1/providers/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, fail(wire.CodeNotFound, "unknown provider route or method"))
	})
}

// Usage implements core.UsageReader.
func (t *Traffic) Usage(ctx context.Context, date string) (wire.DailyUsage, map[string]wire.DailyUsage, error) {
	return t.ledger.Usage(ctx, date)
}

func (t *Traffic) paid(p *provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ev := begin(w, r, p.route)
		defer t.record(r.Context(), ev)
		if e := t.servePaid(w, r, p, ev); e != nil {
			ev.code = e.code
			writeError(w, e)
		}
	}
}

func (t *Traffic) servePaid(w http.ResponseWriter, r *http.Request, p *provider, ev *event) *gwError {
	ctx := r.Context()
	start := time.Now()
	pr, ok := core.PrincipalFrom(ctx)
	if !ok || pr.Sub == "" {
		return fail(wire.CodeAccessRefreshRequired, "authentication required")
	}
	ev.sub, ev.session = pr.Sub, pr.SessionID
	rt, err := t.runtime.Runtime(ctx)
	if err != nil || !rt.PaidCallsEnabled || !p.enabled(rt) || p.key == "" {
		return fail(wire.CodeServiceDisabled, "paid calls are disabled")
	}
	key := r.Header.Get(wire.HeaderIdempotencyKey)
	if !validKey(key) {
		return fail(wire.CodeBadRequest, "Idempotency-Key must be 8 to 128 characters of A-Z, a-z, 0-9, '.', '_', or '-'")
	}
	// The ingress permit is held for as long as the body stays buffered, so
	// at most BodyReaders paid bodies are in memory at once, and the read has
	// its own deadline so a stalled upload cannot hold a permit.
	done, ok := t.ingress.enter(ctx)
	if !ok {
		return &gwError{wire.CodeRateLimited, "too many paid requests are in progress", time.Second}
	}
	defer done()
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyReadTimeout))
	body, e := readBody(r, p.maxBody)
	if e != nil {
		return e
	}
	ev.bytesIn = int64(len(body))
	model, inputs, e := p.validate(body, p.models)
	if e != nil {
		return e
	}
	ev.model, ev.inputs = model, inputs
	charge := p.models[model]
	ev.validation = time.Since(start)

	waitStart := time.Now()
	release, ok := t.slots.acquire(ctx, pr.Sub)
	ev.wait = time.Since(waitStart)
	if !ok {
		return &gwError{wire.CodeRateLimited, "no paid-call slot became free", time.Second}
	}
	defer release()

	reserveStart := time.Now()
	// Recheck the switches: a request queued for a slot must not dispatch
	// after paid calls were switched off.
	if rt, err = t.runtime.Runtime(ctx); err != nil || !rt.PaidCallsEnabled || !p.enabled(rt) {
		return fail(wire.CodeServiceDisabled, "paid calls are disabled")
	}
	sum := sha256.Sum256(body)
	code, err := t.ledger.Reserve(ctx, ledger.Receipt{
		Sub: pr.Sub, Key: key, Route: p.route, BodyHash: hex.EncodeToString(sum[:]), Model: model,
		InputBytes: int64(len(body)), Credits: charge.reserve(int64(len(body)), inputs),
	}, pr.Limits, rt.GlobalLimits)
	if err == nil && code == "" {
		err = t.ledger.MarkDispatched(ctx, pr.Sub, key)
	}
	ev.reservation = time.Since(reserveStart)
	if err != nil {
		return &gwError{wire.CodeInternal, "gateway ledger unavailable; nothing was sent", 0}
	}
	if code != "" {
		return t.admissionError(code)
	}

	upstreamStart := time.Now()
	uctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	resp, wrote, err := t.send(uctx, http.MethodPost, p.url, p.key, body)
	if err != nil {
		ev.upstream = time.Since(upstreamStart)
		if !wrote {
			t.settle(ctx, ev, pr.Sub, key, ledger.NotDispatched, -1)
			return &gwError{wire.CodeUpstreamDown, "the provider could not be reached; nothing was sent", time.Second}
		}
		t.unknown(ctx, ev, pr.Sub, key)
		return fail(wire.CodeOutcomeUnknown, "the provider call failed after dispatch; its outcome is unknown")
	}
	ev.upstreamStatus = resp.StatusCode
	res, e := relay(w, resp, p.key)
	ev.upstream = time.Since(upstreamStart)
	if e != nil {
		t.unknown(ctx, ev, pr.Sub, key)
		return e
	}
	ev.bytesOut = res.bytesOut
	ok2xx := res.status >= 200 && res.status <= 299
	switch {
	case ok2xx && res.complete:
		credits := int64(-1)
		if tokens, ok := p.tokens(res.usage); ok {
			credits = charge.credits(tokens)
		}
		t.settle(ctx, ev, pr.Sub, key, ledger.Succeeded, credits)
	case ok2xx:
		// A cut-off 2xx stream: keep the reservation and reset the
		// connection so the caller cannot mistake it for a whole body.
		t.unknown(ctx, ev, pr.Sub, key)
		panic(http.ErrAbortHandler)
	case res.status >= 500:
		t.unknown(ctx, ev, pr.Sub, key)
	default:
		t.settle(ctx, ev, pr.Sub, key, ledger.Rejected, -1)
	}
	return nil
}

func (t *Traffic) admissionError(code wire.ErrorCode) *gwError {
	switch code {
	case wire.CodeBudgetExhausted:
		now := t.now().UTC()
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		return &gwError{code, "the daily limit is reached until 00:00 UTC", midnight.Sub(now)}
	case wire.CodeInProgress:
		return &gwError{code, "a request with this Idempotency-Key is in progress", time.Second}
	case wire.CodeResultLost:
		return fail(code, "this Idempotency-Key already completed; its result is not stored")
	case wire.CodeOutcomeUnknown:
		return fail(code, "this Idempotency-Key was dispatched and its outcome is unknown")
	default: // CodeIdempotencyClash
		return fail(code, "this Idempotency-Key was used for a different request")
	}
}

func (t *Traffic) settle(ctx context.Context, ev *event, sub, key string, o ledger.Outcome, credits int64) {
	start := time.Now()
	t.settled(ctx, ev, t.ledger.Settle(ctx, sub, key, o, credits))
	ev.settlement += time.Since(start)
}

// unknown keeps the full reservation of an ambiguous dispatched call.
func (t *Traffic) unknown(ctx context.Context, ev *event, sub, key string) {
	start := time.Now()
	t.settled(ctx, ev, t.ledger.MarkUnknown(ctx, sub, key))
	ev.settlement += time.Since(start)
}

func (t *Traffic) settled(ctx context.Context, ev *event, err error) {
	if err != nil && t.rec != nil {
		// The receipt stays dispatched: duplicates see request_in_progress
		// until restart recovery marks it unknown.
		t.rec.Event(ctx, slog.LevelError, "teamapi.traffic", "teamapi.settle_failed", "settlement failed",
			slog.String("request_id", ev.requestID))
	}
}

// models forwards the free TypeSafe model list. It needs a principal but no
// idempotency key, slot, or budget.
func (t *Traffic) models(w http.ResponseWriter, r *http.Request) {
	ev := begin(w, r, wire.RouteTypeSafeModel)
	defer t.record(r.Context(), ev)
	e := func() *gwError {
		pr, ok := core.PrincipalFrom(r.Context())
		if !ok || pr.Sub == "" {
			return fail(wire.CodeAccessRefreshRequired, "authentication required")
		}
		ev.sub, ev.session = pr.Sub, pr.SessionID
		if t.typesafe.key == "" {
			return fail(wire.CodeServiceDisabled, "TypeSafe is not configured")
		}
		start := time.Now()
		defer func() { ev.upstream = time.Since(start) }()
		ctx, cancel := context.WithTimeout(r.Context(), t.typesafe.timeout)
		defer cancel()
		resp, _, err := t.send(ctx, http.MethodGet, t.modelsURL, t.typesafe.key, nil)
		if err != nil {
			return &gwError{wire.CodeUpstreamDown, "the provider could not be reached", time.Second}
		}
		ev.upstreamStatus = resp.StatusCode
		res, e := relay(w, resp, t.typesafe.key)
		if e != nil {
			return &gwError{wire.CodeUpstreamDown, "the provider response could not be read", time.Second}
		}
		ev.bytesOut = res.bytesOut
		if res.status >= 200 && res.status <= 299 && !res.complete {
			// Reset the connection, as the paid routes do, so a cut-off list
			// cannot look whole.
			panic(http.ErrAbortHandler)
		}
		return nil
	}()
	if e != nil {
		ev.code = e.code
		writeError(w, e)
	}
}

func voyageTokens(usage []byte) (int64, bool) {
	var u struct {
		Total *int64 `json:"total_tokens"`
	}
	if usage == nil || json.Unmarshal(usage, &u) != nil || u.Total == nil || *u.Total < 0 {
		return 0, false
	}
	return *u.Total, true
}

func typesafeTokens(usage []byte) (int64, bool) {
	var u struct {
		Input  *int64 `json:"input_tokens"`
		Output *int64 `json:"output_tokens"`
	}
	if usage == nil || json.Unmarshal(usage, &u) != nil || u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output < 0 {
		return 0, false
	}
	return *u.Input + *u.Output, true
}

func writeError(w http.ResponseWriter, e *gwError) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set(wire.HeaderGatewayError, string(e.code))
	if e.retry > 0 {
		h.Set("Retry-After", strconv.FormatInt(int64(math.Ceil(e.retry.Seconds())), 10))
	}
	w.WriteHeader(e.code.Status())
	retryable := (&wire.GatewayError{Code: e.code}).Retryable()
	_ = json.NewEncoder(w).Encode(wire.ErrorBody{Error: wire.ErrorDetail{Code: e.code, Message: e.msg, Retryable: retryable}})
}

// event is the one diagnostics record per provider request. It holds only
// identities, counts, sizes, statuses, and timings.
type event struct {
	requestID, sub, session, route, model, client string
	inputs                                        int
	bytesIn, bytesOut                             int64
	upstreamStatus                                int
	code                                          wire.ErrorCode
	validation, wait, reservation, upstream       time.Duration
	settlement                                    time.Duration
	start                                         time.Time
}

func begin(w http.ResponseWriter, r *http.Request, route string) *event {
	id := w.Header().Get(wire.HeaderRequestID)
	if id == "" {
		var b [16]byte
		_, _ = rand.Read(b[:])
		id = hex.EncodeToString(b[:])
		w.Header().Set(wire.HeaderRequestID, id)
	}
	return &event{requestID: id, route: route, client: clientVersion(r.UserAgent()), start: time.Now()}
}

// clientVersion extracts <version> from a "rzm/<version>" User-Agent.
func clientVersion(ua string) string {
	v, ok := strings.CutPrefix(ua, "rzm/")
	if !ok {
		return ""
	}
	v, _, _ = strings.Cut(v, " ")
	if len(v) > 64 {
		v = v[:64]
	}
	return v
}

func (t *Traffic) record(ctx context.Context, ev *event) {
	if t.rec == nil {
		return
	}
	ms := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	metrics, _ := json.Marshal(map[string]any{
		"input_count": ev.inputs, "bytes_in": ev.bytesIn, "bytes_out": ev.bytesOut,
		"duration_ms": map[string]float64{
			"validation": ms(ev.validation), "admission_wait": ms(ev.wait), "reservation": ms(ev.reservation),
			"upstream": ms(ev.upstream), "settlement": ms(ev.settlement), "total": ms(time.Since(ev.start)),
		},
	})
	t.rec.EventMetrics(context.WithoutCancel(ctx), slog.LevelInfo, "teamapi.traffic", "teamapi.provider_request", "", metrics,
		slog.String("request_id", ev.requestID), slog.String("sub", ev.sub), slog.String("session_id", ev.session),
		slog.String("route", ev.route), slog.String("model", ev.model), slog.Int("upstream_status", ev.upstreamStatus),
		slog.String("gateway_code", string(ev.code)), slog.String("client_version", ev.client))
}
