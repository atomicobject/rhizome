package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type sessionTracker struct {
	mu         sync.Mutex
	ctx        context.Context
	store      sessionDedupeStore
	sessionID  string
	dedupeHits int
	debug      bool
	deferMarks bool
	ensureOnce sync.Once
	ensureOK   bool
}

type sessionDedupeStore = semdb.SessionDedupeStore

type sessionReservation struct {
	ctx           context.Context
	store         sessionDedupeStore
	sessionID     string
	reservationID string
	allowed       map[string]string
}

const sessionReservationReleaseTimeout = 5 * time.Second

func resolveSessionTracker(ctx context.Context, args map[string]any, config Config, allowCreate bool) (*sessionTracker, string) {
	raw, _ := args["sessionId"].(string)
	sessionID := strings.TrimSpace(raw)
	if sessionID == "" && allowCreate {
		id, err := newSessionID()
		if err == nil {
			sessionID = id
		}
	}
	if sessionID == "" {
		return nil, ""
	}

	store := config.getSessionDedupeStore()
	tracker := &sessionTracker{ctx: ctx, store: store, sessionID: sessionID, debug: config.Debug}
	if store == nil {
		// Dedupe is opportunistic. Keep the session ID in responses even when the
		// store is unavailable so callers can reuse the same ID after runtime
		// initialization finishes.
		return tracker, sessionID
	}
	return tracker, sessionID
}

func (c Config) getSessionDedupeStore() semdb.SessionDedupeStore {
	if c.Runtime != nil {
		return c.Runtime.Snapshot().SessionStore
	}
	return c.SessionStore
}

func (t *sessionTracker) ensureLegacySession() bool {
	if t == nil || t.store == nil {
		return false
	}
	t.ensureOnce.Do(func() {
		t.ensureOK = t.store.EnsureSession(t.operationContext(), t.sessionID) == nil
	})
	return t.ensureOK
}

func (t *sessionTracker) operationContext() context.Context {
	if t != nil && t.ctx != nil {
		return t.ctx
	}
	return context.Background()
}

func (t *sessionTracker) Allow(key, fingerprint string) bool {
	if t == nil || t.sessionID == "" {
		return true
	}
	if key == "" || fingerprint == "" {
		return true
	}
	if t.store == nil {
		// Missing dedupe storage should never suppress context. The caller gets
		// duplicated text, not an empty context pack.
		return true
	}
	if t.deferMarks {
		// AllowSessionItem atomically records its fingerprint. Deferred requests
		// must only inspect prior delivery until their final body is encoded.
		if t.Seen(key, fingerprint) {
			t.mu.Lock()
			t.dedupeHits++
			t.mu.Unlock()
			return false
		}
		return true
	}
	if !t.ensureLegacySession() {
		return true
	}
	allowed, err := t.store.AllowSessionItem(t.operationContext(), t.sessionID, key, fingerprint)
	if err != nil {
		return true
	}
	if !allowed {
		t.mu.Lock()
		t.dedupeHits++
		t.mu.Unlock()
		return false
	}
	return true
}

func (t *sessionTracker) MarkSent(key, fingerprint string) {
	if t == nil || t.sessionID == "" {
		return
	}
	if t.deferMarks {
		return
	}
	if key == "" || fingerprint == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.store == nil {
		return
	}
	if !t.ensureLegacySession() {
		return
	}
	_ = t.store.MarkSessionItem(t.operationContext(), t.sessionID, key, fingerprint)
}

// deferredMarkingChild reads the same persisted session state but defers writes
// until final projection and budget trimming have determined emitted bodies.
func (t *sessionTracker) deferredMarkingChild() *sessionTracker {
	if t == nil {
		return nil
	}
	return &sessionTracker{
		ctx:        t.ctx,
		store:      t.store,
		sessionID:  t.sessionID,
		debug:      t.debug,
		deferMarks: true,
	}
}

func (t *sessionTracker) Reserve(items []actions.DedupeItem) actions.BatchDedupeReservation {
	allowed := make(map[string]string, len(items))
	allowAll := func() actions.BatchDedupeReservation {
		for _, item := range items {
			if item.Key != "" && item.Fingerprint != "" {
				allowed[item.Key] = item.Fingerprint
			}
		}
		return &sessionReservation{allowed: allowed}
	}
	if t == nil || t.store == nil || t.sessionID == "" || len(items) == 0 {
		return allowAll()
	}
	reservationID, err := newSessionID()
	if err != nil {
		return allowAll()
	}
	storeItems := make([]semdb.SessionItem, 0, len(items))
	for _, item := range items {
		storeItems = append(storeItems, semdb.SessionItem{Key: item.Key, Fingerprint: item.Fingerprint})
	}
	reserved, err := t.store.ReserveSessionItems(t.operationContext(), t.sessionID, reservationID, storeItems)
	if err != nil {
		return allowAll()
	}
	for _, item := range reserved {
		allowed[item.Key] = item.Fingerprint
	}
	t.mu.Lock()
	t.dedupeHits += len(items) - len(reserved)
	t.mu.Unlock()
	return &sessionReservation{
		ctx:           t.operationContext(),
		store:         t.store,
		sessionID:     t.sessionID,
		reservationID: reservationID,
		allowed:       allowed,
	}
}

func (r *sessionReservation) Allowed(key, fingerprint string) bool {
	return r != nil && r.allowed[key] == fingerprint
}

func (r *sessionReservation) Commit(items []actions.DedupeItem) {
	if r == nil || r.store == nil || r.sessionID == "" || r.reservationID == "" {
		return
	}
	emitted := make([]semdb.SessionItem, 0, len(items))
	for _, item := range items {
		if r.Allowed(item.Key, item.Fingerprint) {
			emitted = append(emitted, semdb.SessionItem{Key: item.Key, Fingerprint: item.Fingerprint})
		}
	}
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.store.CommitSessionItems(ctx, r.sessionID, r.reservationID, emitted); err != nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionCommitFailures, 1)
		// Persistence is opportunistic, but a failed commit must not leave this
		// request's ownership suppressing later context until the reservation TTL.
		// Keep request values (including diagnostics) while detaching cancellation,
		// and bound the fallback so a persistent database failure cannot delay the
		// response indefinitely.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionReservationReleaseTimeout)
		defer cancel()
		if releaseErr := r.store.ReleaseSessionReservations(releaseCtx, r.sessionID, r.reservationID); releaseErr != nil {
			indexingperf.AddCount(releaseCtx, indexingperf.AgentStartOpSessionReleaseFailures, 1)
		}
	}
}

func (t *sessionTracker) DedupeHits() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dedupeHits
}

func (t *sessionTracker) Seen(key, fingerprint string) bool {
	if t == nil || t.sessionID == "" {
		return false
	}
	if key == "" || fingerprint == "" {
		return false
	}
	if t.store == nil {
		return false
	}
	stored, ok, err := t.store.SessionItemFingerprint(context.Background(), t.sessionID, key)
	if err != nil || !ok {
		return false
	}
	return stored == fingerprint
}

func newSessionID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func fingerprintText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
