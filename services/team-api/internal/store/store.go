// Package store holds the gateway's identity state: people, sessions, the
// runtime configuration, and the admin audit log. Store is the narrow
// interface identity code uses. Each method is one atomic operation with
// explicit preconditions (a DynamoDB conditional write or transaction maps
// onto each), and every admin mutation commits with its audit entry. SQLite
// implements it for local runs; a deployed store must pass storetest.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrRevoked  = errors.New("store: session revoked")
	ErrExpired  = errors.New("store: session expired")
	// ErrStale means a compare-and-swap lost to a newer write.
	ErrStale = errors.New("store: stale generation")
)

type Person struct {
	Sub          string
	Email        string
	HostedDomain string
	Status       string
	CreatedAt    time.Time
	Limits       *wire.DailyLimits
}

// Session is one signed-in device. Only token hashes are stored.
type Session struct {
	ID                string
	Sub               string
	DeviceName        string
	ClientVersion     string
	CreatedAt         time.Time
	LastUsedAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	AccessHash        []byte
	AccessExpiresAt   time.Time
	RefreshHash       []byte
	// PrevRefreshHash is the refresh token rotated away most recently, usable
	// for recovery until PrevRefreshDeadline.
	PrevRefreshHash     []byte
	PrevRefreshDeadline time.Time
	Generation          int64
	RevokedAt           time.Time // zero when active
	RevokeReason        string
}

// Rotation replaces a session's token state. The absolute expiry is never
// part of it.
type Rotation struct {
	AccessHash          []byte
	AccessExpiresAt     time.Time
	RefreshHash         []byte
	PrevRefreshHash     []byte
	PrevRefreshDeadline time.Time
	LastUsedAt          time.Time
	IdleExpiresAt       time.Time
}

type Store interface {
	// UpsertPerson inserts an active person or updates the email and hosted
	// domain of an existing one. It never changes Status.
	UpsertPerson(ctx context.Context, sub, email, hd string, now time.Time) (Person, error)
	Person(ctx context.Context, sub string) (Person, error)
	People(ctx context.Context) ([]Person, error)
	// UpdatePerson applies only the set fields of u and appends audit in the
	// same commit. A missing person returns ErrNotFound and records nothing.
	UpdatePerson(ctx context.Context, sub string, u wire.AdminPersonUpdate, audit wire.AuditEntry) (Person, error)

	CreateSession(ctx context.Context, s Session) error
	// Session returns an active session, or ErrNotFound, ErrRevoked, or
	// ErrExpired. Expiry is decided here from now, not by cleanup.
	Session(ctx context.Context, id string, now time.Time) (Session, error)
	// Sessions lists every stored session of sub, or of everyone when sub is
	// empty, including revoked and expired ones, newest first.
	Sessions(ctx context.Context, sub string) ([]Session, error)
	// RotateSession applies r if the session is unrevoked and still at
	// generation, incrementing it; otherwise it returns ErrStale.
	RotateSession(ctx context.Context, id string, generation int64, r Rotation) error
	// RevokeSession marks a session revoked, appending audit (when not nil)
	// in the same commit. Revoking twice keeps the first time and reason.
	// Unknown IDs return ErrNotFound and record nothing.
	RevokeSession(ctx context.Context, id, reason string, now time.Time, audit *wire.AuditEntry) error
	// RevokePersonSessions revokes every unrevoked session of sub and appends
	// audit in the same commit. A missing person returns ErrNotFound.
	RevokePersonSessions(ctx context.Context, sub, reason string, now time.Time, audit wire.AuditEntry) (int, error)

	// InitRuntimeConfig stores defaults only if no runtime config exists yet.
	InitRuntimeConfig(ctx context.Context, defaults wire.AdminConfig) error
	// RuntimeConfig returns the stored config, or ErrNotFound before init.
	// AcceptedDomains and Admins are never stored.
	RuntimeConfig(ctx context.Context) (wire.AdminConfig, error)
	// UpdateRuntimeConfig applies only the set fields of u and appends audit
	// in the same commit.
	UpdateRuntimeConfig(ctx context.Context, u wire.AdminConfigUpdate, audit wire.AuditEntry) (wire.AdminConfig, error)

	// Audit returns at most limit entries, newest first.
	Audit(ctx context.Context, limit int) ([]wire.AuditEntry, error)
}
