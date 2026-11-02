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

type SQLite struct {
	db  *sql.DB
	now func() time.Time
	// SQLite serializes separate instances with BEGIN IMMEDIATE as well.
	mu    sync.Mutex
	owner string
}

// Open creates the ledger_ tables if needed.
func Open(ctx context.Context, db *sql.DB, now func() time.Time) (*SQLite, error) {
	if now == nil {
		now = time.Now
	}
	l := &SQLite{db: db, now: now, owner: newOwner()}
	err := l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		_, err := c.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS ledger_owners (owner TEXT PRIMARY KEY, expires_at INTEGER NOT NULL);
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
	owner TEXT NOT NULL DEFAULT '',
	input_bytes INTEGER NOT NULL,
	reserved_credits INTEGER NOT NULL,
	settled_credits INTEGER,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (sub, idem_key)
);`)
		if err != nil {
			return fmt.Errorf("ledger: migrate: %w", err)
		}
		// Upgrade databases created before lease ownership was introduced.
		rows, err := c.QueryContext(ctx, "PRAGMA table_info(ledger_receipts)")
		if err != nil {
			return err
		}
		columns := map[string]bool{}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				rows.Close()
				return err
			}
			columns[name] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, col := range []struct{ name, ddl string }{
			{"owner", "TEXT NOT NULL DEFAULT ''"},
			{"provider", "TEXT NOT NULL DEFAULT ''"},
			{"price_version", "TEXT NOT NULL DEFAULT ''"},
			{"reserved_nanodollars", "INTEGER NOT NULL DEFAULT 0"},
			{"settled_nanodollars", "INTEGER"},
			{"provider_limit_nanodollars", "INTEGER NOT NULL DEFAULT 0"},
			{"price_nanodollars_per_token", "INTEGER NOT NULL DEFAULT 0"},
			{"max_billable_tokens", "INTEGER NOT NULL DEFAULT 0"},
			{"accounting_version", "INTEGER NOT NULL DEFAULT 0"},
		} {
			if !columns[col.name] {
				if _, err := c.ExecContext(ctx, "ALTER TABLE ledger_receipts ADD COLUMN "+col.name+" "+col.ddl); err != nil {
					return err
				}
			}
		}
		if err := migrateProviderSQLite(ctx, c); err != nil {
			return err
		}
		_, err = c.ExecContext(ctx, "INSERT INTO ledger_owners (owner, expires_at) VALUES (?, ?)", l.owner, now().Add(LeaseDuration).UnixNano())
		return err
	})
	return l, err
}

// Heartbeat extends a live lease. An expired lease cannot be resurrected.
func (l *SQLite) Heartbeat(ctx context.Context) error {
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		now := l.now()
		res, err := c.ExecContext(ctx, "UPDATE ledger_owners SET expires_at = ? WHERE owner = ? AND expires_at > ?", now.Add(LeaseDuration).UnixNano(), l.owner, now.UnixNano())
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrPrecondition
		}
		return nil
	})
}

// Recover fences expired owners before changing their unfinished receipts.
func (l *SQLite) Recover(ctx context.Context) error {
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		if _, err := c.ExecContext(ctx, "UPDATE ledger_owners SET expires_at = 0 WHERE expires_at <= ?", l.now().UnixNano()); err != nil {
			return err
		}
		if err := l.recoverProviders(ctx, c); err != nil {
			return err
		}
		const abandoned = `state IN ('reserved', 'dispatched') AND NOT EXISTS (SELECT 1 FROM ledger_owners o WHERE o.owner = r.owner AND o.expires_at > 0)`
		if _, err := c.ExecContext(ctx, `UPDATE ledger_days SET unknown = unknown + (SELECT count(*) FROM ledger_receipts r WHERE r.day = ledger_days.day AND (ledger_days.sub = '' OR ledger_days.sub = r.sub) AND `+abandoned+`)`); err != nil {
			return err
		}
		_, err := c.ExecContext(ctx, `UPDATE ledger_receipts AS r SET state = ?, updated_at = ? WHERE `+abandoned, stateUnknown, l.stamp())
		return err
	})
}

func (l *SQLite) live(ctx context.Context, c *sql.Conn) error {
	var one int
	err := c.QueryRowContext(ctx, "SELECT 1 FROM ledger_owners WHERE owner = ? AND expires_at > ?", l.owner, l.now().UnixNano()).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPrecondition
	}
	return err
}

// Day is the current UTC day, YYYY-MM-DD.
func (l *SQLite) Day() string { return l.now().UTC().Format(time.DateOnly) }

func (l *SQLite) stamp() string { return l.now().UTC().Format(time.RFC3339Nano) }

// Reserve inserts the receipt and adds its request, bytes, and credits to the
// person's and the global day counters, in one transaction. A non-empty code
// means nothing was admitted: an idempotency duplicate or conflict, or
// budget_exhausted when any limit would be exceeded. A key whose earlier
// attempt was never dispatched is admitted again.
func (l *SQLite) Reserve(ctx context.Context, r Receipt, person, everyone wire.DailyLimits) (wire.ErrorCode, error) {
	if err := validReceipt(r); err != nil {
		return "", err
	}
	if r.Provider != "" {
		r.AccountingVersion = providerAccountingVersion
	}
	err := l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		if err := l.live(ctx, c); err != nil {
			return err
		}
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
		if err := l.reserveProvider(ctx, c, day, r); err != nil {
			return err
		}
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
			if exceeds(used, r, scope.limits) {
				return codeErr{wire.CodeBudgetExhausted}
			}
			if _, err := c.ExecContext(ctx, `
INSERT INTO ledger_days (day, sub, requests, input_bytes, reserved_credits, budgeted_requests) VALUES (?, ?, 1, ?, ?, ?)
ON CONFLICT (day, sub) DO UPDATE SET requests = requests + 1,
	input_bytes = input_bytes + excluded.input_bytes,
	reserved_credits = reserved_credits + excluded.reserved_credits,
    budgeted_requests = budgeted_requests + excluded.budgeted_requests`,
				day, scope.sub, r.InputBytes, r.Credits, providerRequest(r)); err != nil {
				return err
			}
		}
		now := l.stamp()
		_, err = c.ExecContext(ctx, `
INSERT INTO ledger_receipts (sub, idem_key, route, body_hash, model, day, state, input_bytes, reserved_credits, created_at, updated_at, owner, provider, price_version, reserved_nanodollars, price_nanodollars_per_token, max_billable_tokens, accounting_version, provider_limit_nanodollars)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (sub, idem_key) DO UPDATE SET model = excluded.model, day = excluded.day, state = excluded.state,
	input_bytes = excluded.input_bytes, reserved_credits = excluded.reserved_credits, updated_at = excluded.updated_at, owner = excluded.owner,
    settled_credits = NULL, settled_nanodollars = NULL, provider_limit_nanodollars = excluded.provider_limit_nanodollars, provider = excluded.provider, price_version = excluded.price_version,
    reserved_nanodollars = excluded.reserved_nanodollars, price_nanodollars_per_token = excluded.price_nanodollars_per_token,
    max_billable_tokens = excluded.max_billable_tokens, accounting_version = excluded.accounting_version`,
			r.Sub, r.Key, r.Route, r.BodyHash, r.Model, day, stateReserved, r.InputBytes, r.Credits, now, now, l.owner, r.Provider, r.PriceVersion, r.ReservedNanodollars, r.PriceNanodollarsPerToken, r.MaxBillableTokens, r.AccountingVersion, r.ProviderLimitNanodollars)
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
func (l *SQLite) MarkDispatched(ctx context.Context, sub, key string) error {
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		_, err := l.transition(ctx, c, sub, key, stateReserved, stateDispatched, nil)
		return err
	})
}

// MarkUnknown records an ambiguous result for a dispatched receipt and keeps
// its full reservation. Any other state returns ErrPrecondition.
func (l *SQLite) MarkUnknown(ctx context.Context, sub, key string) error {
	return l.tx(ctx, func(ctx context.Context, c *sql.Conn) error {
		r, err := l.transition(ctx, c, sub, key, stateDispatched, stateUnknown, nil)
		if err != nil {
			return err
		}
		if err := l.adjustProvider(ctx, c, r.day, sub, r.Receipt, stateUnknown, nil); err != nil {
			return err
		}
		return l.adjust(ctx, c, r.day, sub, `unknown = unknown + 1`)
	})
}

// Settle applies a dispatched receipt's definitive outcome. A receipt that is
// not dispatched, including one already settled, returns ErrPrecondition and
// changes nothing, so nothing is refunded twice. actualCredits < 0 means
// usage is unknown; it only matters for Succeeded.
func (l *SQLite) Settle(ctx context.Context, sub, key string, outcome Outcome, actualCredits int64, actualNanodollars ...int64) error {
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
		if _, err := c.ExecContext(ctx, "UPDATE ledger_receipts SET settled_nanodollars = ? WHERE sub = ? AND idem_key = ?", settledCurrency(r.Receipt, to, actualNanodollars), sub, key); err != nil {
			return err
		}
		if err := l.adjustProvider(ctx, c, r.day, sub, r.Receipt, to, actualNanodollars); err != nil {
			return err
		}
		_, unknown := providerAdjustment(r.Receipt, to, actualNanodollars)
		if unknown {
			if _, err := c.ExecContext(ctx, "UPDATE ledger_receipts SET state = ?, settled_credits = NULL WHERE sub = ? AND idem_key = ?", stateUnknown, sub, key); err != nil {
				return err
			}
			return l.adjust(ctx, c, r.day, sub, `unknown = unknown + 1`)
		}
		switch {
		case outcome == NotDispatched:
			return l.adjust(ctx, c, r.day, sub,
				`requests = requests - 1, input_bytes = input_bytes - ?, reserved_credits = reserved_credits - ?, budgeted_requests = budgeted_requests - ?`, r.bytes, r.reserved, providerRequest(r.Receipt))
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
	Receipt
	day             string
	bytes, reserved int64
}

// transition moves one receipt between states, or returns ErrPrecondition.
func (l *SQLite) transition(ctx context.Context, c *sql.Conn, sub, key, from, to string, settled *int64) (held, error) {
	var r held
	if err := l.live(ctx, c); err != nil {
		return r, err
	}
	err := c.QueryRowContext(ctx, `SELECT day, input_bytes, reserved_credits, provider, price_version, reserved_nanodollars, price_nanodollars_per_token, max_billable_tokens, accounting_version FROM ledger_receipts WHERE sub = ? AND idem_key = ? AND state = ? AND owner = ?`,
		sub, key, from, l.owner).Scan(&r.day, &r.bytes, &r.reserved, &r.Provider, &r.PriceVersion, &r.ReservedNanodollars, &r.PriceNanodollarsPerToken, &r.MaxBillableTokens, &r.AccountingVersion)
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
func (l *SQLite) adjust(ctx context.Context, c *sql.Conn, day, sub, set string, args ...any) error {
	_, err := c.ExecContext(ctx, `UPDATE ledger_days SET `+set+` WHERE day = ? AND sub IN (?, ?)`, append(args, day, sub, global)...)
	return err
}

// Usage implements core.UsageReader for one UTC day (YYYY-MM-DD).
func (l *SQLite) Usage(ctx context.Context, date string) (wire.DailyUsage, map[string]wire.DailyUsage, error) {
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
func (l *SQLite) tx(ctx context.Context, fn func(context.Context, *sql.Conn) error) error {
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
