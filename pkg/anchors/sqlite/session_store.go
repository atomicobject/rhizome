package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
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

// SessionItem is one stable session-dedupe key and its content fingerprint.
type SessionItem struct {
	Key         string
	Fingerprint string
}

// ReserveSessionItems atomically reserves unseen or changed items for one opaque owner.
// The returned items retain input order, with duplicate input keys collapsed.
func (s *Store) ReserveSessionItems(ctx context.Context, sessionID, reservationID string, items []SessionItem) ([]SessionItem, error) {
	return s.reserveSessionItemsAt(ctx, sessionID, reservationID, items, time.Now())
}

func (s *Store) reserveSessionItemsAt(ctx context.Context, sessionID, reservationID string, items []SessionItem, nowTime time.Time) ([]SessionItem, error) {
	if s == nil {
		return nil, errors.New("sqlite store is nil")
	}
	if sessionID == "" || reservationID == "" {
		return nil, errors.New("session id and reservation id are required")
	}
	items, err := normalizeSessionItems(items)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	now := nowTime.Unix()
	staleBefore := nowTime.Add(-SessionReservationTTL).Unix()
	futureAfter := nowTime.Add(SessionReservationFutureSkew).Unix()
	reserved := make([]SessionItem, 0, len(items))
	ctx = indexingperf.WithOp(ctx, "intel.reserve_session_items")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionReserve)
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, txErr := tx.ExecContext(ctx, `
			INSERT INTO mcp_sessions (session_id, created_at, last_seen_at)
			VALUES (?, ?, ?)
			ON CONFLICT(session_id) DO UPDATE SET last_seen_at = excluded.last_seen_at
		`, sessionID, now, now); txErr != nil {
			return txErr
		}
		for start := 0; start < len(items); start += sessionItemSQLChunkSize {
			end := min(start+sessionItemSQLChunkSize, len(items))
			chunk := items[start:end]
			cte, inputArgs := sessionItemsValuesCTE(chunk)
			args := append(inputArgs, sessionID, reservationID, now, sessionID, staleBefore, futureAfter)
			if _, txErr := tx.ExecContext(ctx, cte+`
				INSERT INTO mcp_session_item_reservations
					(session_id, item_key, fingerprint, reservation_id, reserved_at)
				SELECT ?, input.item_key, input.fingerprint, ?, ? FROM input
				WHERE NOT EXISTS (
					SELECT 1 FROM mcp_session_items
					WHERE session_id = ? AND item_key = input.item_key AND fingerprint = input.fingerprint
				)
				ON CONFLICT(session_id, item_key) DO UPDATE SET
					fingerprint = excluded.fingerprint,
					reservation_id = excluded.reservation_id,
					reserved_at = excluded.reserved_at
				WHERE mcp_session_item_reservations.reservation_id = excluded.reservation_id
				   OR mcp_session_item_reservations.reserved_at < ?
				   OR mcp_session_item_reservations.reserved_at > ?
			`, args...); txErr != nil {
				return txErr
			}
			queryArgs := append(inputArgs, sessionID, reservationID)
			rows, txErr := tx.QueryContext(ctx, cte+`
				SELECT input.item_key, input.fingerprint
				FROM input
				JOIN mcp_session_item_reservations reservation
				  ON reservation.session_id = ?
				 AND reservation.item_key = input.item_key
				 AND reservation.fingerprint = input.fingerprint
				 AND reservation.reservation_id = ?
				WHERE NOT EXISTS (
					SELECT 1 FROM mcp_session_items committed
					WHERE committed.session_id = reservation.session_id
					  AND committed.item_key = input.item_key
					  AND committed.fingerprint = input.fingerprint
				)
				ORDER BY input.ord
			`, queryArgs...)
			if txErr != nil {
				return txErr
			}
			for rows.Next() {
				var item SessionItem
				if txErr = rows.Scan(&item.Key, &item.Fingerprint); txErr != nil {
					_ = rows.Close()
					return txErr
				}
				reserved = append(reserved, item)
			}
			if txErr = rows.Close(); txErr != nil {
				return txErr
			}
			if txErr = rows.Err(); txErr != nil {
				return txErr
			}
		}
		return nil
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionReserves, 1)
	}
	return reserved, err
}

// CommitSessionItems commits only emitted items owned by reservationID, then releases
// every remaining reservation held by that owner for the session.
func (s *Store) CommitSessionItems(ctx context.Context, sessionID, reservationID string, emitted []SessionItem) error {
	if s == nil {
		return errors.New("sqlite store is nil")
	}
	if sessionID == "" || reservationID == "" {
		return errors.New("session id and reservation id are required")
	}
	emitted, err := normalizeSessionItems(emitted)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	ctx = indexingperf.WithOp(ctx, "intel.commit_session_items")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionCommit)
	err = s.withWriteTx(ctx, func(tx *sql.Tx) error {
		for start := 0; start < len(emitted); start += sessionItemSQLChunkSize {
			end := min(start+sessionItemSQLChunkSize, len(emitted))
			cte, inputArgs := sessionItemsValuesCTE(emitted[start:end])
			args := append(inputArgs, now, sessionID, reservationID)
			if _, txErr := tx.ExecContext(ctx, cte+`
				INSERT INTO mcp_session_items (session_id, item_key, fingerprint, sent_at)
				SELECT reservation.session_id, reservation.item_key, reservation.fingerprint, ?
				FROM input
				JOIN mcp_session_item_reservations reservation
				  ON reservation.session_id = ?
				 AND reservation.item_key = input.item_key
				 AND reservation.fingerprint = input.fingerprint
				 AND reservation.reservation_id = ?
				WHERE true
				ON CONFLICT(session_id, item_key) DO UPDATE SET
					fingerprint = excluded.fingerprint,
					sent_at = excluded.sent_at
			`, args...); txErr != nil {
				return txErr
			}
		}
		_, txErr := tx.ExecContext(ctx, `
			DELETE FROM mcp_session_item_reservations
			WHERE session_id = ? AND reservation_id = ?
		`, sessionID, reservationID)
		return txErr
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionCommits, 1)
	}
	return err
}

// ReleaseSessionReservations releases every reservation owned by reservationID
// for a session without committing any item. It is the bounded recovery path
// when an opportunistic commit fails after context has already been packed.
func (s *Store) ReleaseSessionReservations(ctx context.Context, sessionID, reservationID string) error {
	if s == nil {
		return errors.New("sqlite store is nil")
	}
	if sessionID == "" || reservationID == "" {
		return errors.New("session id and reservation id are required")
	}
	ctx = indexingperf.WithOp(ctx, "intel.release_session_items")
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			DELETE FROM mcp_session_item_reservations
			WHERE session_id = ? AND reservation_id = ?
		`, sessionID, reservationID)
		return err
	})
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionReleases, 1)
	}
	return err
}

func sessionItemsValuesCTE(items []SessionItem) (string, []any) {
	var query strings.Builder
	query.WriteString("WITH input(ord, item_key, fingerprint) AS (VALUES ")
	args := make([]any, 0, len(items)*3)
	for i, item := range items {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteString("(?,?,?)")
		args = append(args, i, item.Key, item.Fingerprint)
	}
	query.WriteString(") ")
	return query.String(), args
}

func normalizeSessionItems(items []SessionItem) ([]SessionItem, error) {
	result := make([]SessionItem, 0, len(items))
	seen := make(map[string]string, len(items))
	for _, item := range items {
		if item.Key == "" || item.Fingerprint == "" {
			return nil, errors.New("session item key and fingerprint are required")
		}
		if fingerprint, ok := seen[item.Key]; ok {
			if fingerprint != item.Fingerprint {
				return nil, errors.New("duplicate session item key has conflicting fingerprints")
			}
			continue
		}
		seen[item.Key] = item.Fingerprint
		result = append(result, item)
	}
	return result, nil
}

// SessionCleanupDue performs the ordinary read-only due check.
func (s *Store) SessionCleanupDue(ctx context.Context, now time.Time) (bool, error) {
	if s == nil {
		return false, errors.New("sqlite store is nil")
	}
	var nextDue int64
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionCleanup)
	err := s.db.QueryRowContext(ctx, `
		SELECT next_due_at FROM mcp_session_maintenance WHERE maintenance_key = 'cleanup'
	`).Scan(&nextDue)
	finish(err)
	return nextDue <= now.Unix(), err
}

// ClaimSessionCleanup conditionally advances cleanup's next due time.
func (s *Store) ClaimSessionCleanup(ctx context.Context, now, nextDue time.Time) (bool, error) {
	if s == nil {
		return false, errors.New("sqlite store is nil")
	}
	if nextDue.Before(now) {
		return false, errors.New("cleanup next due time must not precede claim time")
	}
	var claimed bool
	ctx = indexingperf.WithOp(ctx, "intel.claim_session_cleanup")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionCleanup)
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, txErr := tx.ExecContext(ctx, `
			UPDATE mcp_session_maintenance SET next_due_at = ?
			WHERE maintenance_key = 'cleanup' AND next_due_at <= ?
		`, nextDue.Unix(), now.Unix())
		if txErr != nil {
			return txErr
		}
		rows, txErr := res.RowsAffected()
		claimed = rows == 1
		return txErr
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		if claimed {
			indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionCleanups, 1)
		}
	}
	return claimed, err
}

// RelinquishSessionCleanup makes a failed cleanup eligible for retry, provided
// the caller still owns the claim represented by claimedUntil. The conditional
// update fences a stale failure path from resetting a successor's newer claim.
func (s *Store) RelinquishSessionCleanup(ctx context.Context, claimedUntil, retryAt time.Time) (bool, error) {
	if s == nil {
		return false, errors.New("sqlite store is nil")
	}
	if retryAt.After(claimedUntil) {
		return false, errors.New("cleanup retry time must not follow claimed deadline")
	}
	var relinquished bool
	ctx = indexingperf.WithOp(ctx, "intel.relinquish_session_cleanup")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionCleanup)
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, txErr := tx.ExecContext(ctx, `
			UPDATE mcp_session_maintenance SET next_due_at = ?
			WHERE maintenance_key = 'cleanup' AND next_due_at = ?
		`, retryAt.Unix(), claimedUntil.Unix())
		if txErr != nil {
			return txErr
		}
		rows, txErr := res.RowsAffected()
		relinquished = rows == 1
		return txErr
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
	}
	return relinquished, err
}

// EnsureSession creates the session if needed and updates last_seen_at.
func (s *Store) EnsureSession(ctx context.Context, sessionID string) error {
	if s == nil {
		return errors.New("sqlite store is nil")
	}
	if sessionID == "" {
		return errors.New("session id is required")
	}
	now := time.Now().Unix()
	ctx = indexingperf.WithOp(ctx, "intel.ensure_session")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionReserve)
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mcp_sessions (session_id, created_at, last_seen_at)
			VALUES (?, ?, ?)
			ON CONFLICT(session_id) DO UPDATE SET last_seen_at = excluded.last_seen_at
		`, sessionID, now, now)
		return err
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
	}
	return err
}

// SessionItemFingerprint returns the stored fingerprint for an item, if present.
func (s *Store) SessionItemFingerprint(ctx context.Context, sessionID, itemKey string) (string, bool, error) {
	if s == nil {
		return "", false, errors.New("sqlite store is nil")
	}
	if sessionID == "" || itemKey == "" {
		return "", false, errors.New("session id and item key are required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT fingerprint
		FROM mcp_session_items
		WHERE session_id = ? AND item_key = ?
	`, sessionID, itemKey)
	var fingerprint string
	if err := row.Scan(&fingerprint); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if fingerprint == "" {
		return "", false, nil
	}
	return fingerprint, true, nil
}

// MarkSessionItem records that an item has been sent for a session.
func (s *Store) MarkSessionItem(ctx context.Context, sessionID, itemKey, fingerprint string) error {
	if s == nil {
		return errors.New("sqlite store is nil")
	}
	if sessionID == "" || itemKey == "" || fingerprint == "" {
		return errors.New("session id, item key, and fingerprint are required")
	}
	now := time.Now().Unix()
	ctx = indexingperf.WithOp(ctx, "intel.mark_session_item")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionCommit)
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO mcp_session_items (session_id, item_key, fingerprint, sent_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(session_id, item_key) DO UPDATE SET
				fingerprint = excluded.fingerprint,
				sent_at = excluded.sent_at
		`, sessionID, itemKey, fingerprint, now)
		return err
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionCommits, 1)
	}
	return err
}

// AllowSessionItem records an item if it is new or changed, returning true when allowed.
// Returns false when the stored fingerprint already matches (duplicate).
func (s *Store) AllowSessionItem(ctx context.Context, sessionID, itemKey, fingerprint string) (bool, error) {
	if s == nil {
		return false, errors.New("sqlite store is nil")
	}
	if sessionID == "" || itemKey == "" || fingerprint == "" {
		return false, errors.New("session id, item key, and fingerprint are required")
	}
	now := time.Now().Unix()
	var allowed bool
	ctx = indexingperf.WithOp(ctx, "intel.allow_session_item")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionReserve)
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO mcp_session_items (session_id, item_key, fingerprint, sent_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(session_id, item_key) DO UPDATE SET
				fingerprint = excluded.fingerprint,
				sent_at = excluded.sent_at
			WHERE mcp_session_items.fingerprint != excluded.fingerprint
		`, sessionID, itemKey, fingerprint, now)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		allowed = rows > 0
		return nil
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionReserves, 1)
	}
	return allowed, err
}

// CleanupSessions removes sessions (and their items) last seen before the cutoff.
func (s *Store) CleanupSessions(ctx context.Context, olderThan time.Time) (int64, error) {
	if s == nil {
		return 0, errors.New("sqlite store is nil")
	}
	cutoff := olderThan.Unix()
	var removed int64
	ctx = indexingperf.WithOp(ctx, "intel.cleanup_sessions")
	finish := indexingperf.StartSpan(ctx, indexingperf.AgentStartPhaseSessionCleanup)
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM mcp_session_item_reservations
			WHERE session_id IN (
				SELECT session_id FROM mcp_sessions WHERE last_seen_at < ?
			)
		`, cutoff); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM mcp_session_items
			WHERE session_id IN (
				SELECT session_id FROM mcp_sessions WHERE last_seen_at < ?
			)
		`, cutoff); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			DELETE FROM mcp_sessions
			WHERE last_seen_at < ?
		`, cutoff)
		if err != nil {
			return err
		}
		removed, err = res.RowsAffected()
		return err
	})
	finish(err)
	if err == nil {
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpTransactions, 1)
		indexingperf.AddCount(ctx, indexingperf.AgentStartOpSessionCleanups, 1)
	}
	return removed, err
}
