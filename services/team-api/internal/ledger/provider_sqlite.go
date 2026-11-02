package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

func migrateProviderSQLite(ctx context.Context, c *sql.Conn) error {
	rows, err := c.QueryContext(ctx, "PRAGMA table_info(ledger_days)")
	if err != nil {
		return err
	}
	hasBudgeted := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		hasBudgeted = hasBudgeted || name == "budgeted_requests"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !hasBudgeted {
		if _, err := c.ExecContext(ctx, "ALTER TABLE ledger_days ADD COLUMN budgeted_requests INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	_, err = c.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ledger_provider_days (
	day TEXT NOT NULL, provider TEXT NOT NULL, sub TEXT NOT NULL,
	requests INTEGER NOT NULL DEFAULT 0, reserved_nanodollars INTEGER NOT NULL DEFAULT 0,
	settled_nanodollars INTEGER NOT NULL DEFAULT 0, unknown INTEGER NOT NULL DEFAULT 0,
	fenced INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(day, provider, sub));
	CREATE TABLE IF NOT EXISTS ledger_provider_fences (
	provider TEXT PRIMARY KEY, fenced INTEGER NOT NULL)`)
	return err
}

func providerRequest(r Receipt) int64 {
	if budgeted(r) {
		return 1
	}
	return 0
}

func readProviderSQLite(ctx context.Context, c *sql.Conn, day, provider, sub string) (providerCounter, error) {
	var u providerCounter
	err := c.QueryRowContext(ctx, `SELECT requests, reserved_nanodollars, settled_nanodollars, unknown, fenced FROM ledger_provider_days WHERE day = ? AND provider = ? AND sub = ?`, day, provider, sub).Scan(&u.Requests, &u.ReservedNanodollars, &u.SettledNanodollars, &u.Unknown, &u.Fenced)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return u, err
}

func writeProviderSQLite(ctx context.Context, c *sql.Conn, day, provider, sub string, u providerCounter) error {
	_, err := c.ExecContext(ctx, `INSERT INTO ledger_provider_days (day, provider, sub, requests, reserved_nanodollars, settled_nanodollars, unknown, fenced)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(day, provider, sub) DO UPDATE SET
	requests = excluded.requests, reserved_nanodollars = excluded.reserved_nanodollars,
	settled_nanodollars = excluded.settled_nanodollars, unknown = excluded.unknown, fenced = excluded.fenced`, day, provider, sub, u.Requests, u.ReservedNanodollars, u.SettledNanodollars, u.Unknown, u.Fenced)
	return err
}

func (l *SQLite) reserveProvider(ctx context.Context, c *sql.Conn, day string, r Receipt) error {
	if !budgeted(r) {
		return nil
	}
	var fenced bool
	err := c.QueryRowContext(ctx, "SELECT fenced FROM ledger_provider_fences WHERE provider = ?", r.Provider).Scan(&fenced)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if fenced {
		return codeErr{wire.CodeBudgetExhausted}
	}
	var requests, budgetedRequests int64
	err = c.QueryRowContext(ctx, "SELECT requests, budgeted_requests FROM ledger_days WHERE day = ? AND sub = ''", day).Scan(&requests, &budgetedRequests)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if requests != budgetedRequests {
		return codeErr{wire.CodeBudgetExhausted}
	}
	for _, sub := range []string{global, r.Sub} {
		u, err := readProviderSQLite(ctx, c, day, r.Provider, sub)
		if err != nil {
			return err
		}
		if sub == global && (u.Fenced || r.ReservedNanodollars > r.ProviderLimitNanodollars-u.ReservedNanodollars) {
			return codeErr{wire.CodeBudgetExhausted}
		}
		u.Requests++
		u.ReservedNanodollars += r.ReservedNanodollars
		if err := writeProviderSQLite(ctx, c, day, r.Provider, sub, u); err != nil {
			return err
		}
	}
	return nil
}

func (l *SQLite) adjustProvider(ctx context.Context, c *sql.Conn, day, sub string, r Receipt, state string, actual []int64) error {
	adjust, _ := providerAdjustment(r, state, actual)
	if adjust == nil {
		return nil
	}
	for _, scope := range []string{global, sub} {
		u, err := readProviderSQLite(ctx, c, day, r.Provider, scope)
		if err != nil {
			return err
		}
		if u.Requests <= 0 || u.ReservedNanodollars < r.ReservedNanodollars {
			return errors.New("ledger: provider receipt counter missing or inconsistent")
		}
		adjust(&u)
		if scope == global && u.Fenced {
			if _, err := c.ExecContext(ctx, `INSERT INTO ledger_provider_fences (provider, fenced) VALUES (?, 1)
			ON CONFLICT(provider) DO UPDATE SET fenced = 1`, r.Provider); err != nil {
				return err
			}
		}
		if err := writeProviderSQLite(ctx, c, day, r.Provider, scope, u); err != nil {
			return err
		}
	}
	return nil
}

func (l *SQLite) recoverProviders(ctx context.Context, c *sql.Conn) error {
	rows, err := c.QueryContext(ctx, `SELECT r.day, r.sub, r.provider, r.price_version, r.reserved_nanodollars, r.accounting_version FROM ledger_receipts r
	WHERE r.state IN ('reserved','dispatched') AND NOT EXISTS (SELECT 1 FROM ledger_owners o WHERE o.owner = r.owner AND o.expires_at > 0)`)
	if err != nil {
		return err
	}
	type abandoned struct {
		day, sub string
		r        Receipt
	}
	var abandonedReceipts []abandoned
	for rows.Next() {
		var a abandoned
		if err := rows.Scan(&a.day, &a.sub, &a.r.Provider, &a.r.PriceVersion, &a.r.ReservedNanodollars, &a.r.AccountingVersion); err != nil {
			rows.Close()
			return err
		}
		abandonedReceipts = append(abandonedReceipts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range abandonedReceipts {
		if err := l.adjustProvider(ctx, c, a.day, a.sub, a.r, stateUnknown, nil); err != nil {
			return err
		}
	}
	return nil
}

func (l *SQLite) ProviderUsage(ctx context.Context, date string) (map[string]wire.ProviderUsage, map[string]map[string]wire.ProviderUsage, error) {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return nil, nil, fmt.Errorf("ledger: date must be YYYY-MM-DD")
	}
	rows, err := l.db.QueryContext(ctx, `SELECT provider, sub, requests, reserved_nanodollars, settled_nanodollars, unknown, fenced FROM ledger_provider_days WHERE day = ?`, date)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	everyone := map[string]wire.ProviderUsage{}
	people := map[string]map[string]wire.ProviderUsage{}
	for rows.Next() {
		var provider, sub string
		var u wire.ProviderUsage
		if err := rows.Scan(&provider, &sub, &u.Requests, &u.ReservedNanodollars, &u.SettledNanodollars, &u.Unknown, &u.Fenced); err != nil {
			return nil, nil, err
		}
		if sub == global {
			everyone[provider] = u
		} else {
			if people[sub] == nil {
				people[sub] = map[string]wire.ProviderUsage{}
			}
			people[sub][provider] = u
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	// Overlay current persistent admission status on the requested date's
	// accounting totals, including dates without any provider-day row.
	fences, err := l.db.QueryContext(ctx, "SELECT provider FROM ledger_provider_fences WHERE fenced = 1")
	if err != nil {
		return nil, nil, err
	}
	defer fences.Close()
	for fences.Next() {
		var provider string
		if err := fences.Scan(&provider); err != nil {
			return nil, nil, err
		}
		u := everyone[provider]
		u.Fenced = true
		everyone[provider] = u
	}
	return everyone, people, fences.Err()
}
