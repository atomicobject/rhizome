package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

const schema = `
CREATE TABLE IF NOT EXISTS people (
	sub        TEXT PRIMARY KEY,
	email      TEXT NOT NULL,
	hd         TEXT NOT NULL,
	status     TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	limits     TEXT
);
CREATE TABLE IF NOT EXISTS sessions (
	id                    TEXT PRIMARY KEY,
	sub                   TEXT NOT NULL REFERENCES people(sub),
	device_name           TEXT NOT NULL,
	client_version        TEXT NOT NULL,
	created_at            INTEGER NOT NULL,
	last_used_at          INTEGER NOT NULL,
	idle_expires_at       INTEGER NOT NULL,
	absolute_expires_at   INTEGER NOT NULL,
	access_hash           BLOB NOT NULL,
	access_expires_at     INTEGER NOT NULL,
	refresh_hash          BLOB NOT NULL,
	prev_refresh_hash     BLOB,
	prev_refresh_deadline INTEGER NOT NULL DEFAULT 0,
	generation            INTEGER NOT NULL,
	revoked_at            INTEGER NOT NULL DEFAULT 0,
	revoke_reason         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS sessions_sub ON sessions(sub);
CREATE TABLE IF NOT EXISTS runtime_config (
	id   INTEGER PRIMARY KEY CHECK (id = 1),
	body TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	at     INTEGER NOT NULL,
	actor  TEXT NOT NULL,
	action TEXT NOT NULL,
	target TEXT NOT NULL,
	detail TEXT NOT NULL
);`

// Open opens (creating if needed) the gateway database at path with WAL,
// a busy timeout, foreign keys, and mode 0600, and creates the identity
// tables. Every transaction begins IMMEDIATE, so read-modify-write
// transactions take the write lock up front; the traffic side shares this
// handle and its ledger tables get the same behavior.
func Open(path string) (*sql.DB, error) {
	if strings.ContainsRune(path, '?') {
		return nil, fmt.Errorf("store: database path must not contain '?': %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	// SQLite gives the -wal and -shm files the main file's permissions.
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_txlock=immediate&_busy_timeout=10000&_foreign_keys=1&_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: create schema: %w", err)
	}
	return db, nil
}

// SQLite implements Store on a database from Open.
type SQLite struct{ db *sql.DB }

func NewSQLite(db *sql.DB) *SQLite { return &SQLite{db: db} }

// Times are stored as Unix nanoseconds; 0 means unset.
func ts(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromTS(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

type scanner interface{ Scan(...any) error }

const personCols = `sub, email, hd, status, created_at, limits`

func scanPerson(row scanner) (Person, error) {
	var p Person
	var created int64
	var limits sql.NullString
	if err := row.Scan(&p.Sub, &p.Email, &p.HostedDomain, &p.Status, &created, &limits); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Person{}, ErrNotFound
		}
		return Person{}, err
	}
	p.CreatedAt = fromTS(created)
	if limits.Valid {
		p.Limits = new(wire.DailyLimits)
		if err := json.Unmarshal([]byte(limits.String), p.Limits); err != nil {
			return Person{}, err
		}
	}
	return p, nil
}

func (s *SQLite) UpsertPerson(ctx context.Context, sub, email, hd string, now time.Time) (Person, error) {
	row := s.db.QueryRowContext(ctx, `INSERT INTO people (sub, email, hd, status, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(sub) DO UPDATE SET email = excluded.email, hd = excluded.hd
		RETURNING `+personCols, sub, email, hd, wire.PersonActive, ts(now))
	return scanPerson(row)
}

func (s *SQLite) Person(ctx context.Context, sub string) (Person, error) {
	return scanPerson(s.db.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE sub = ?`, sub))
}

func (s *SQLite) People(ctx context.Context) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+personCols+` FROM people ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdatePerson(ctx context.Context, sub string, u wire.AdminPersonUpdate, audit wire.AuditEntry) (Person, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Person{}, err
	}
	defer tx.Rollback()
	p, err := scanPerson(tx.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE sub = ?`, sub))
	if err != nil {
		return Person{}, err
	}
	if u.Status != nil {
		p.Status = *u.Status
	}
	if u.ClearLimits {
		p.Limits = nil
	}
	if u.Limits != nil {
		l := *u.Limits
		p.Limits = &l
	}
	var limits sql.NullString
	if p.Limits != nil {
		b, _ := json.Marshal(p.Limits)
		limits = sql.NullString{String: string(b), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE people SET status = ?, limits = ? WHERE sub = ?`, p.Status, limits, sub); err != nil {
		return Person{}, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return Person{}, err
	}
	return p, tx.Commit()
}

const sessionCols = `id, sub, device_name, client_version, created_at, last_used_at, idle_expires_at,
	absolute_expires_at, access_hash, access_expires_at, refresh_hash, prev_refresh_hash,
	prev_refresh_deadline, generation, revoked_at, revoke_reason`

func scanSession(row scanner) (Session, error) {
	var s Session
	var created, used, idle, abs, accessExp, prevDeadline, revoked int64
	err := row.Scan(&s.ID, &s.Sub, &s.DeviceName, &s.ClientVersion, &created, &used, &idle,
		&abs, &s.AccessHash, &accessExp, &s.RefreshHash, &s.PrevRefreshHash,
		&prevDeadline, &s.Generation, &revoked, &s.RevokeReason)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	s.CreatedAt, s.LastUsedAt, s.IdleExpiresAt = fromTS(created), fromTS(used), fromTS(idle)
	s.AbsoluteExpiresAt, s.AccessExpiresAt = fromTS(abs), fromTS(accessExp)
	s.PrevRefreshDeadline, s.RevokedAt = fromTS(prevDeadline), fromTS(revoked)
	return s, nil
}

func (s *SQLite) CreateSession(ctx context.Context, x Session) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.ID, x.Sub, x.DeviceName, x.ClientVersion, ts(x.CreatedAt), ts(x.LastUsedAt), ts(x.IdleExpiresAt),
		ts(x.AbsoluteExpiresAt), x.AccessHash, ts(x.AccessExpiresAt), x.RefreshHash, x.PrevRefreshHash,
		ts(x.PrevRefreshDeadline), x.Generation, ts(x.RevokedAt), x.RevokeReason)
	return err
}

func (s *SQLite) Session(ctx context.Context, id string, now time.Time) (Session, error) {
	x, err := scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
	if err != nil {
		return Session{}, err
	}
	if !x.RevokedAt.IsZero() {
		return Session{}, ErrRevoked
	}
	if !now.Before(x.AbsoluteExpiresAt) || !now.Before(x.IdleExpiresAt) {
		return Session{}, ErrExpired
	}
	return x, nil
}

func (s *SQLite) Sessions(ctx context.Context, sub string) ([]Session, error) {
	q, args := `SELECT `+sessionCols+` FROM sessions ORDER BY created_at DESC`, []any{}
	if sub != "" {
		q, args = `SELECT `+sessionCols+` FROM sessions WHERE sub = ? ORDER BY created_at DESC`, []any{sub}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *SQLite) RotateSession(ctx context.Context, id string, generation int64, r Rotation) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET access_hash = ?, access_expires_at = ?, refresh_hash = ?,
		prev_refresh_hash = ?, prev_refresh_deadline = ?, last_used_at = ?, idle_expires_at = ?, generation = generation + 1
		WHERE id = ? AND generation = ? AND revoked_at = 0`,
		r.AccessHash, ts(r.AccessExpiresAt), r.RefreshHash, r.PrevRefreshHash, ts(r.PrevRefreshDeadline),
		ts(r.LastUsedAt), ts(r.IdleExpiresAt), id, generation)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStale
	}
	return nil
}

func (s *SQLite) RevokeSession(ctx context.Context, id, reason string, now time.Time, audit *wire.AuditEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = ?, revoke_reason = ? WHERE id = ? AND revoked_at = 0`, ts(now), reason, id); err != nil {
		return err
	}
	if audit != nil {
		if err := appendAudit(ctx, tx, *audit); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) RevokePersonSessions(ctx context.Context, sub, reason string, now time.Time, audit wire.AuditEntry) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM people WHERE sub = ?`, sub).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	} else if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at = ?, revoke_reason = ? WHERE sub = ? AND revoked_at = 0`, ts(now), reason, sub)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}

func (s *SQLite) InitRuntimeConfig(ctx context.Context, defaults wire.AdminConfig) error {
	body, err := encodeRuntime(defaults)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO runtime_config (id, body) VALUES (1, ?)`, body)
	return err
}

func (s *SQLite) RuntimeConfig(ctx context.Context) (wire.AdminConfig, error) {
	return readRuntime(s.db.QueryRowContext(ctx, `SELECT body FROM runtime_config WHERE id = 1`))
}

func (s *SQLite) UpdateRuntimeConfig(ctx context.Context, u wire.AdminConfigUpdate, audit wire.AuditEntry) (wire.AdminConfig, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wire.AdminConfig{}, err
	}
	defer tx.Rollback()
	c, err := readRuntime(tx.QueryRowContext(ctx, `SELECT body FROM runtime_config WHERE id = 1`))
	if err != nil {
		return wire.AdminConfig{}, err
	}
	set := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	set(&c.PaidCallsEnabled, u.PaidCallsEnabled)
	set(&c.VoyageEnabled, u.VoyageEnabled)
	set(&c.TypeSafeEnabled, u.TypeSafeEnabled)
	if u.EligibilityMode != nil {
		c.EligibilityMode = *u.EligibilityMode
	}
	if u.Allowlist != nil {
		c.Allowlist = append([]string{}, (*u.Allowlist)...)
	}
	if u.PersonLimits != nil {
		c.PersonLimits = *u.PersonLimits
	}
	if u.GlobalLimits != nil {
		c.GlobalLimits = *u.GlobalLimits
	}
	body, err := encodeRuntime(c)
	if err != nil {
		return wire.AdminConfig{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runtime_config SET body = ? WHERE id = 1`, body); err != nil {
		return wire.AdminConfig{}, err
	}
	if err := appendAudit(ctx, tx, audit); err != nil {
		return wire.AdminConfig{}, err
	}
	return c, tx.Commit()
}

func encodeRuntime(c wire.AdminConfig) (string, error) {
	c.AcceptedDomains, c.Admins = nil, nil
	b, err := json.Marshal(c)
	return string(b), err
}

func readRuntime(row *sql.Row) (wire.AdminConfig, error) {
	var body string
	if err := row.Scan(&body); errors.Is(err, sql.ErrNoRows) {
		return wire.AdminConfig{}, ErrNotFound
	} else if err != nil {
		return wire.AdminConfig{}, err
	}
	var c wire.AdminConfig
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		return wire.AdminConfig{}, err
	}
	c.AcceptedDomains, c.Admins = nil, nil
	if c.Allowlist == nil {
		c.Allowlist = []string{}
	}
	return c, nil
}

func appendAudit(ctx context.Context, tx *sql.Tx, e wire.AuditEntry) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit (at, actor, action, target, detail) VALUES (?, ?, ?, ?, ?)`,
		ts(e.At), e.Actor, e.Action, e.Target, e.Detail)
	return err
}

func (s *SQLite) Audit(ctx context.Context, limit int) ([]wire.AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at, actor, action, target, detail FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.AuditEntry{}
	for rows.Next() {
		var e wire.AuditEntry
		var at int64
		if err := rows.Scan(&at, &e.Actor, &e.Action, &e.Target, &e.Detail); err != nil {
			return nil, err
		}
		e.At = fromTS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
