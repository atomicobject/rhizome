// Package ledger is the gateway's durable record of paid requests: one receipt
// per idempotency key and per-UTC-day counters for each person and for
// everyone. Every write is one SQLite BEGIN IMMEDIATE transaction, so a
// receipt and the budget it holds always change together.
package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// Receipt states.
const (
	stateReserved      = "reserved"       // admitted, not yet sent
	stateDispatched    = "dispatched"     // persisted before the first request byte
	stateNotDispatched = "not_dispatched" // nothing was written; reservation released
	stateSucceeded     = "succeeded"      // 2xx, settled
	stateRejected      = "rejected"       // definitive 3xx/4xx; credits released
	stateUnknown       = "unknown"        // anything ambiguous after dispatch
)

// global is the ledger_days sub for everyone's counters. Principals always
// have a non-empty sub.
const global = ""

// ErrPrecondition means the receipt was not in the state the operation
// expects. Nothing changed.
var ErrPrecondition = errors.New("ledger: receipt is not in the expected state")

// Outcome is how a dispatched receipt ended definitively.
type Outcome int

const (
	// NotDispatched releases the request, bytes, and credits: no request byte
	// was written, so the key may be admitted again.
	NotDispatched Outcome = iota
	// Succeeded records a 2xx response. Known actual credits replace the
	// reservation; unknown usage keeps it.
	Succeeded
	// Rejected records a definitive upstream rejection (3xx, 4xx, 429). Its
	// credits are released; the request and bytes stay counted, and the key
	// stays settled.
	Rejected
)

// Receipt identifies one logical paid request and what it reserves.
type Receipt struct {
	Sub, Key, Route, BodyHash, Model string
	InputBytes, Credits              int64
}

type Ledger struct {
	db  *sql.DB
	now func() time.Time
	// ponytail: one in-process writer lock; the spec allows one serving task.
	mu sync.Mutex
}

// Open creates the ledger_ tables if needed.
func Open(ctx context.Context, db *sql.DB, now func() time.Time) (*Ledger, error) {
	if now == nil {
		now = time.Now
	}
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS ledger_days (
	day TEXT NOT NULL,
	sub TEXT NOT NULL,
	requests INTEGER NOT NULL DEFAULT 0,
	input_bytes INTEGER NOT NULL DEFAULT 0,
	reserved_credits INTEGER NOT NULL DEFAULT 0,
	settled_credits INTEGER NOT NULL DEFAULT 0,
	unknown INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (day, sub)
);
CREATE TABLE IF NOT EXISTS ledger_receipts (
	sub TEXT NOT NULL,
	idem_key TEXT NOT NULL,
	route TEXT NOT NULL,
	body_hash TEXT NOT NULL,
	model TEXT NOT NULL,
	day TEXT NOT NULL,
	state TEXT NOT NULL,
	input_bytes INTEGER NOT NULL,
	reserved_credits INTEGER NOT NULL,
	settled_credits INTEGER,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (sub, idem_key)
);`)
	if err != nil {
		return nil, fmt.Errorf("ledger: migrate: %w", err)
	}
	return &Ledger{db: db, now: now}, nil
}

// Recover marks receipts left reserved or dispatched by a previous process as
// unknown outcomes, keeping their reservations. Call it once at startup,
// before serving; never while this process has requests in flight.
func (l *Ledger) Recover(ctx context.Context) error {
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		if _, err := c.ExecContext(ctx, `
UPDATE ledger_days SET unknown = unknown + (
	SELECT count(*) FROM ledger_receipts r
	WHERE r.day = ledger_days.day AND r.state IN (?, ?)
	AND (ledger_days.sub = ? OR ledger_days.sub = r.sub))`, stateReserved, stateDispatched, global); err != nil {
			return err
		}
		_, err := c.ExecContext(ctx, `UPDATE ledger_receipts SET state = ?, updated_at = ? WHERE state IN (?, ?)`,
			stateUnknown, l.stamp(), stateReserved, stateDispatched)
		return err
	})
}

// Day is the current UTC day, YYYY-MM-DD.
func (l *Ledger) Day() string { return l.now().UTC().Format(time.DateOnly) }

func (l *Ledger) stamp() string { return l.now().UTC().Format(time.RFC3339Nano) }

type codeErr struct{ code wire.ErrorCode }

func (e codeErr) Error() string { return string(e.code) }

// Reserve inserts the receipt and adds its request, bytes, and credits to the
// person's and the global day counters, in one transaction. A non-empty code
// means nothing was admitted: an idempotency duplicate or conflict, or
// budget_exhausted when any limit would be exceeded. A key whose earlier
// attempt was never dispatched is admitted again.
func (l *Ledger) Reserve(ctx context.Context, r Receipt, person, everyone wire.DailyLimits) (wire.ErrorCode, error) {
	err := l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		var route, hash, state string
		err := c.QueryRowContext(ctx, `SELECT route, body_hash, state FROM ledger_receipts WHERE sub = ? AND idem_key = ?`,
			r.Sub, r.Key).Scan(&route, &hash, &state)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		case route != r.Route || hash != r.BodyHash:
			return codeErr{wire.CodeIdempotencyClash}
		case state == stateReserved || state == stateDispatched:
			return codeErr{wire.CodeInProgress}
		case state == stateSucceeded || state == stateRejected:
			return codeErr{wire.CodeResultLost}
		case state == stateUnknown:
			return codeErr{wire.CodeOutcomeUnknown}
		}
		day := l.Day()
		for _, scope := range []struct {
			sub    string
			limits wire.DailyLimits
		}{{r.Sub, person}, {global, everyone}} {
			var used wire.DailyUsage
			err := c.QueryRowContext(ctx, `SELECT requests, input_bytes, reserved_credits FROM ledger_days WHERE day = ? AND sub = ?`,
				day, scope.sub).Scan(&used.Requests, &used.InputBytes, &used.ReservedCredits)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if used.Requests+1 > scope.limits.Requests || used.InputBytes+r.InputBytes > scope.limits.InputBytes ||
				used.ReservedCredits+r.Credits > scope.limits.ChargeCredits {
				return codeErr{wire.CodeBudgetExhausted}
			}
			if _, err := c.ExecContext(ctx, `
INSERT INTO ledger_days (day, sub, requests, input_bytes, reserved_credits) VALUES (?, ?, 1, ?, ?)
ON CONFLICT (day, sub) DO UPDATE SET requests = requests + 1,
	input_bytes = input_bytes + excluded.input_bytes,
	reserved_credits = reserved_credits + excluded.reserved_credits`,
				day, scope.sub, r.InputBytes, r.Credits); err != nil {
				return err
			}
		}
		now := l.stamp()
		_, err = c.ExecContext(ctx, `
INSERT INTO ledger_receipts (sub, idem_key, route, body_hash, model, day, state, input_bytes, reserved_credits, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (sub, idem_key) DO UPDATE SET model = excluded.model, day = excluded.day, state = excluded.state,
	input_bytes = excluded.input_bytes, reserved_credits = excluded.reserved_credits, updated_at = excluded.updated_at`,
			r.Sub, r.Key, r.Route, r.BodyHash, r.Model, day, stateReserved, r.InputBytes, r.Credits, now, now)
		return err
	})
	var ce codeErr
	if errors.As(err, &ce) {
		return ce.code, nil
	}
	return "", err
}

// MarkDispatched moves a reserved receipt to dispatched. Call it before
// writing any request byte. Any other state returns ErrPrecondition.
func (l *Ledger) MarkDispatched(ctx context.Context, sub, key string) error {
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		_, err := l.transition(ctx, c, sub, key, stateReserved, stateDispatched, nil)
		return err
	})
}

// MarkUnknown records an ambiguous result for a dispatched receipt and keeps
// its full reservation. Any other state returns ErrPrecondition.
func (l *Ledger) MarkUnknown(ctx context.Context, sub, key string) error {
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		r, err := l.transition(ctx, c, sub, key, stateDispatched, stateUnknown, nil)
		if err != nil {
			return err
		}
		return l.adjust(ctx, c, r.day, sub, `unknown = unknown + 1`)
	})
}

// Settle applies a dispatched receipt's definitive outcome. A receipt that is
// not dispatched, including one already settled, returns ErrPrecondition and
// changes nothing, so nothing is refunded twice. actualCredits < 0 means
// usage is unknown; it only matters for Succeeded.
func (l *Ledger) Settle(ctx context.Context, sub, key string, outcome Outcome, actualCredits int64) error {
	to, ok := map[Outcome]string{NotDispatched: stateNotDispatched, Succeeded: stateSucceeded, Rejected: stateRejected}[outcome]
	if !ok {
		return fmt.Errorf("ledger: unknown outcome %d", outcome)
	}
	var settled *int64
	if outcome == Succeeded && actualCredits >= 0 {
		settled = &actualCredits
	}
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		r, err := l.transition(ctx, c, sub, key, stateDispatched, to, settled)
		if err != nil {
			return err
		}
		switch {
		case outcome == NotDispatched:
			return l.adjust(ctx, c, r.day, sub,
				`requests = requests - 1, input_bytes = input_bytes - ?, reserved_credits = reserved_credits - ?`, r.bytes, r.reserved)
		case outcome == Rejected:
			return l.adjust(ctx, c, r.day, sub, `reserved_credits = reserved_credits - ?`, r.reserved)
		case settled != nil:
			return l.adjust(ctx, c, r.day, sub,
				`reserved_credits = reserved_credits + ?, settled_credits = settled_credits + ?`, actualCredits-r.reserved, actualCredits)
		}
		return nil
	})
}

// held is what a receipt holds against its admission day.
type held struct {
	day             string
	bytes, reserved int64
}

// transition moves one receipt between states, or returns ErrPrecondition.
func (l *Ledger) transition(ctx context.Context, c *sql.Conn, sub, key, from, to string, settled *int64) (held, error) {
	var r held
	err := c.QueryRowContext(ctx, `SELECT day, input_bytes, reserved_credits FROM ledger_receipts WHERE sub = ? AND idem_key = ? AND state = ?`,
		sub, key, from).Scan(&r.day, &r.bytes, &r.reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrPrecondition
	}
	if err != nil {
		return r, err
	}
	_, err = c.ExecContext(ctx, `UPDATE ledger_receipts SET state = ?, settled_credits = ?, updated_at = ? WHERE sub = ? AND idem_key = ?`,
		to, settled, l.stamp(), sub, key)
	return r, err
}

// adjust updates the person's and the global counters on the receipt's
// admission day, which may be before today.
func (l *Ledger) adjust(ctx context.Context, c *sql.Conn, day, sub, set string, args ...any) error {
	_, err := c.ExecContext(ctx, `UPDATE ledger_days SET `+set+` WHERE day = ? AND sub IN (?, ?)`, append(args, day, sub, global)...)
	return err
}

// Usage implements core.UsageReader for one UTC day (YYYY-MM-DD).
func (l *Ledger) Usage(ctx context.Context, date string) (wire.DailyUsage, map[string]wire.DailyUsage, error) {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return wire.DailyUsage{}, nil, fmt.Errorf("ledger: date must be YYYY-MM-DD")
	}
	rows, err := l.db.QueryContext(ctx, `SELECT sub, requests, input_bytes, reserved_credits, settled_credits, unknown FROM ledger_days WHERE day = ?`, date)
	if err != nil {
		return wire.DailyUsage{}, nil, err
	}
	defer rows.Close()
	var everyone wire.DailyUsage
	people := map[string]wire.DailyUsage{}
	for rows.Next() {
		var sub string
		var u wire.DailyUsage
		if err := rows.Scan(&sub, &u.Requests, &u.InputBytes, &u.ReservedCredits, &u.SettledCredits, &u.Unknown); err != nil {
			return wire.DailyUsage{}, nil, err
		}
		if sub == global {
			everyone = u
		} else {
			people[sub] = u
		}
	}
	return everyone, people, rows.Err()
}

// tx runs fn in one BEGIN IMMEDIATE transaction on a dedicated connection.
// Ledger writes ignore caller cancellation so a receipt is never half applied.
func (l *Ledger) tx(ctx context.Context, fn func(context.Context, *sql.Conn) error) error {
	ctx = context.WithoutCancel(ctx)
	l.mu.Lock()
	defer l.mu.Unlock()
	c, err := l.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	if err := fn(ctx, c); err != nil {
		_, _ = c.ExecContext(ctx, "ROLLBACK")
		return err
	}
	if _, err := c.ExecContext(ctx, "COMMIT"); err != nil {
		_, _ = c.ExecContext(ctx, "ROLLBACK")
		return err
	}
	return nil
}
