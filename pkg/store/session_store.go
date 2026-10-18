package store

import (
	"context"
	"time"
)

// SessionDedupeStore is the narrow persistence boundary used by agent session
// tracking. Indexed read-only runtimes expose only this interface from their
// separate existing-write connection.
type SessionDedupeStore interface {
	EnsureSession(context.Context, string) error
	AllowSessionItem(context.Context, string, string, string) (bool, error)
	MarkSessionItem(context.Context, string, string, string) error
	ReserveSessionItems(context.Context, string, string, []SessionItem) ([]SessionItem, error)
	CommitSessionItems(context.Context, string, string, []SessionItem) error
	ReleaseSessionReservations(context.Context, string, string) error
	SessionItemFingerprint(context.Context, string, string) (string, bool, error)
}

// SessionDedupeHandle is an owned session-dedupe connection.
type SessionDedupeHandle interface {
	SessionDedupeStore
	Close() error
}

const sessionDedupeOperationTimeout = 250 * time.Millisecond

type boundedSessionDedupeHandle struct {
	store *Store
}

func newBoundedSessionDedupeHandle(store *Store) SessionDedupeHandle {
	return &boundedSessionDedupeHandle{store: store}
}

func (h *boundedSessionDedupeHandle) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, sessionDedupeOperationTimeout)
}

func (h *boundedSessionDedupeHandle) EnsureSession(ctx context.Context, sessionID string) error {
	ctx, cancel := h.bounded(ctx)
	defer cancel()
	return h.store.EnsureSession(ctx, sessionID)
}

func (h *boundedSessionDedupeHandle) AllowSessionItem(ctx context.Context, sessionID, kind, itemKey string) (bool, error) {
	ctx, cancel := h.bounded(ctx)
	defer cancel()
	return h.store.AllowSessionItem(ctx, sessionID, kind, itemKey)
}

func (h *boundedSessionDedupeHandle) MarkSessionItem(ctx context.Context, sessionID, kind, itemKey string) error {
	ctx, cancel := h.bounded(ctx)
	defer cancel()
	return h.store.MarkSessionItem(ctx, sessionID, kind, itemKey)
}

func (h *boundedSessionDedupeHandle) ReserveSessionItems(ctx context.Context, sessionID, reservationID string, items []SessionItem) ([]SessionItem, error) {
	ctx, cancel := h.bounded(ctx)
	defer cancel()
	return h.store.ReserveSessionItems(ctx, sessionID, reservationID, items)
}

func (h *boundedSessionDedupeHandle) CommitSessionItems(ctx context.Context, sessionID, reservationID string, items []SessionItem) error {
	ctx, cancel := h.bounded(ctx)
	defer cancel()
	return h.store.CommitSessionItems(ctx, sessionID, reservationID, items)
}

func (h *boundedSessionDedupeHandle) ReleaseSessionReservations(ctx context.Context, sessionID, reservationID string) error {
	ctx, cancel := h.bounded(ctx)
	defer cancel()
	return h.store.ReleaseSessionReservations(ctx, sessionID, reservationID)
}

func (h *boundedSessionDedupeHandle) SessionItemFingerprint(ctx context.Context, sessionID, itemKey string) (string, bool, error) {
	ctx, cancel := h.bounded(ctx)
	defer cancel()
	return h.store.SessionItemFingerprint(ctx, sessionID, itemKey)
}

func (h *boundedSessionDedupeHandle) Close() error {
	return h.store.Close()
}

// DefaultSessionRetention is the default age cutoff for removing stale stored sessions.
const DefaultSessionRetention = 30 * 24 * time.Hour

// SessionReservationTTL bounds how long an abandoned reservation suppresses an item.
const SessionReservationTTL = 45 * time.Second

// SessionReservationFutureSkew bounds tolerated clock skew before a reservation
// timestamp is treated as abandoned. This prevents clock rollback from creating
// an effectively unbounded reservation.
const SessionReservationFutureSkew = 5 * time.Second

const sessionItemSQLChunkSize = 200
