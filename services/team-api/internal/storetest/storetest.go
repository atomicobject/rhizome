// Package storetest is the contract every store.Store implementation must
// pass. The SQLite store runs it; a deployed store must run it too.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// Run runs the contract against fresh, empty stores from open. Every
// time-dependent operation takes its clock as an argument, so the suite
// drives grace and expiry with fixed times.
func Run(t *testing.T, open func(t *testing.T) store.Store) {
	t.Run("UpsertNeverOverwritesDisabled", func(t *testing.T) { upsertKeepsDisabled(t, open(t)) })
	t.Run("PersonPartialUpdate", func(t *testing.T) { personPartialUpdate(t, open(t)) })
	t.Run("RotationCompareAndSwap", func(t *testing.T) { rotationCAS(t, open(t)) })
	t.Run("RevokedStaysRevoked", func(t *testing.T) { revokedStaysRevoked(t, open(t)) })
	t.Run("ExpiryEnforcedOnRead", func(t *testing.T) { expiryOnRead(t, open(t)) })
	t.Run("RuntimeConfigPartialUpdate", func(t *testing.T) { runtimePartial(t, open(t)) })
	t.Run("AuditCommitsWithMutation", func(t *testing.T) { auditWithMutation(t, open(t)) })
}

func entry(action string) wire.AuditEntry {
	return wire.AuditEntry{At: t0, Actor: "admin@example.test", Action: action, Target: "t", Detail: "d"}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newSession(t *testing.T, s store.Store, id, sub string) store.Session {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Person(ctx, sub); errors.Is(err, store.ErrNotFound) {
		_, err = s.UpsertPerson(ctx, sub, sub+"@example.test", "example.test", t0)
		must(t, err)
	}
	x := store.Session{
		ID: id, Sub: sub, DeviceName: "laptop", ClientVersion: "1.0.0",
		CreatedAt: t0, LastUsedAt: t0,
		IdleExpiresAt: t0.Add(14 * 24 * time.Hour), AbsoluteExpiresAt: t0.Add(30 * 24 * time.Hour),
		AccessHash: []byte("access-0"), AccessExpiresAt: t0.Add(10 * time.Minute),
		RefreshHash: []byte("refresh-0"),
	}
	must(t, s.CreateSession(ctx, x))
	return x
}

func upsertKeepsDisabled(t *testing.T, s store.Store) {
	ctx := context.Background()
	p, err := s.UpsertPerson(ctx, "sub-1", "a@example.test", "example.test", t0)
	must(t, err)
	if p.Status != wire.PersonActive || !p.CreatedAt.Equal(t0) {
		t.Fatalf("new person = %+v", p)
	}
	disabled := wire.PersonDisabled
	_, err = s.UpdatePerson(ctx, "sub-1", wire.AdminPersonUpdate{Status: &disabled}, entry("person.update"))
	must(t, err)
	p, err = s.UpsertPerson(ctx, "sub-1", "renamed@example.test", "example.test", t0.Add(time.Hour))
	must(t, err)
	if p.Status != wire.PersonDisabled || p.Email != "renamed@example.test" || !p.CreatedAt.Equal(t0) {
		t.Fatalf("upsert after disable = %+v", p)
	}
	if p, _ = s.Person(ctx, "sub-1"); p.Status != wire.PersonDisabled {
		t.Fatalf("stored status = %q", p.Status)
	}
}

func personPartialUpdate(t *testing.T, s store.Store) {
	ctx := context.Background()
	_, err := s.UpsertPerson(ctx, "sub-1", "a@example.test", "example.test", t0)
	must(t, err)
	limits := wire.DailyLimits{Requests: 5, InputBytes: 6, ChargeCredits: 7}
	p, err := s.UpdatePerson(ctx, "sub-1", wire.AdminPersonUpdate{Limits: &limits}, entry("person.update"))
	must(t, err)
	if p.Status != wire.PersonActive || p.Limits == nil || *p.Limits != limits {
		t.Fatalf("limits update = %+v", p)
	}
	p, err = s.UpdatePerson(ctx, "sub-1", wire.AdminPersonUpdate{ClearLimits: true}, entry("person.update"))
	must(t, err)
	if p.Limits != nil {
		t.Fatalf("limits not cleared: %+v", p)
	}
	if _, err := s.UpdatePerson(ctx, "missing", wire.AdminPersonUpdate{}, entry("person.update")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update of missing person = %v", err)
	}
	people, err := s.People(ctx)
	must(t, err)
	if len(people) != 1 || people[0].Sub != "sub-1" {
		t.Fatalf("people = %+v", people)
	}
}

func rotationCAS(t *testing.T, s store.Store) {
	ctx := context.Background()
	x := newSession(t, s, "sess-1", "sub-1")
	r := store.Rotation{
		AccessHash: []byte("access-1"), AccessExpiresAt: t0.Add(20 * time.Minute),
		RefreshHash: []byte("refresh-1"), PrevRefreshHash: x.RefreshHash, PrevRefreshDeadline: t0.Add(2 * time.Minute),
		LastUsedAt: t0.Add(time.Minute), IdleExpiresAt: t0.Add(15 * 24 * time.Hour),
	}
	must(t, s.RotateSession(ctx, x.ID, x.Generation, r))
	stale := r
	stale.RefreshHash = []byte("refresh-stale")
	if err := s.RotateSession(ctx, x.ID, x.Generation, stale); !errors.Is(err, store.ErrStale) {
		t.Fatalf("stale rotation = %v, want ErrStale", err)
	}
	got, err := s.Session(ctx, x.ID, t0.Add(time.Minute))
	must(t, err)
	if got.Generation != x.Generation+1 || !bytes.Equal(got.RefreshHash, r.RefreshHash) ||
		!bytes.Equal(got.PrevRefreshHash, x.RefreshHash) || !got.PrevRefreshDeadline.Equal(r.PrevRefreshDeadline) ||
		!got.AbsoluteExpiresAt.Equal(x.AbsoluteExpiresAt) || !got.IdleExpiresAt.Equal(r.IdleExpiresAt) {
		t.Fatalf("rotated session = %+v", got)
	}
}

func revokedStaysRevoked(t *testing.T, s store.Store) {
	ctx := context.Background()
	x := newSession(t, s, "sess-1", "sub-1")
	other := newSession(t, s, "sess-2", "sub-1")
	third := newSession(t, s, "sess-3", "sub-2")
	must(t, s.RevokeSession(ctx, x.ID, "logout", t0, nil))
	must(t, s.RevokeSession(ctx, x.ID, "admin", t0.Add(time.Hour), nil))
	if _, err := s.Session(ctx, x.ID, t0); !errors.Is(err, store.ErrRevoked) {
		t.Fatalf("revoked read = %v", err)
	}
	if err := s.RotateSession(ctx, x.ID, x.Generation, store.Rotation{AccessHash: []byte("a"), RefreshHash: []byte("r")}); !errors.Is(err, store.ErrStale) {
		t.Fatalf("rotation of revoked session = %v, want ErrStale", err)
	}
	if err := s.RevokeSession(ctx, "missing", "admin", t0, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke missing = %v", err)
	}
	all, err := s.Sessions(ctx, "sub-1")
	must(t, err)
	for _, got := range all {
		if got.ID == x.ID && (!got.RevokedAt.Equal(t0) || got.RevokeReason != "logout") {
			t.Fatalf("second revoke changed the first: %+v", got)
		}
	}
	n, err := s.RevokePersonSessions(ctx, "sub-1", "admin", t0, entry("person.revoke_sessions"))
	must(t, err)
	if n != 1 {
		t.Fatalf("revoked %d sessions, want only the one still active", n)
	}
	if _, err := s.Session(ctx, other.ID, t0); !errors.Is(err, store.ErrRevoked) {
		t.Fatalf("person revoke missed a session: %v", err)
	}
	if _, err := s.Session(ctx, third.ID, t0); err != nil {
		t.Fatalf("person revoke touched another person: %v", err)
	}
	if everyone, _ := s.Sessions(ctx, ""); len(everyone) != 3 {
		t.Fatalf("Sessions(\"\") = %d sessions, want 3", len(everyone))
	}
}

func expiryOnRead(t *testing.T, s store.Store) {
	ctx := context.Background()
	x := newSession(t, s, "sess-1", "sub-1")
	if _, err := s.Session(ctx, x.ID, x.IdleExpiresAt.Add(-time.Second)); err != nil {
		t.Fatalf("live session: %v", err)
	}
	if _, err := s.Session(ctx, x.ID, x.IdleExpiresAt); !errors.Is(err, store.ErrExpired) {
		t.Fatalf("idle-expired read = %v", err)
	}
	// Push idle expiry past the absolute limit; the absolute limit still wins.
	must(t, s.RotateSession(ctx, x.ID, 0, store.Rotation{
		AccessHash: []byte("a"), RefreshHash: []byte("r"), LastUsedAt: t0, IdleExpiresAt: t0.Add(60 * 24 * time.Hour),
	}))
	if _, err := s.Session(ctx, x.ID, x.AbsoluteExpiresAt); !errors.Is(err, store.ErrExpired) {
		t.Fatalf("absolute-expired read = %v", err)
	}
	if _, err := s.Session(ctx, "missing", t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing read = %v", err)
	}
}

func runtimePartial(t *testing.T, s store.Store) {
	ctx := context.Background()
	if _, err := s.RuntimeConfig(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("uninitialized config = %v", err)
	}
	def := wire.AdminConfig{
		PaidCallsEnabled: true, VoyageEnabled: true, TypeSafeEnabled: true,
		EligibilityMode: wire.EligibilityAllowlist, Allowlist: []string{"a@example.test"},
		PersonLimits: wire.DailyLimits{Requests: 1}, GlobalLimits: wire.DailyLimits{Requests: 2},
		AcceptedDomains: []string{"example.test"}, Admins: []string{"a@example.test"},
	}
	must(t, s.InitRuntimeConfig(ctx, def))
	off := false
	mode := wire.EligibilityAll
	got, err := s.UpdateRuntimeConfig(ctx, wire.AdminConfigUpdate{PaidCallsEnabled: &off, EligibilityMode: &mode}, entry("config.update"))
	must(t, err)
	// A second init must not reset the update.
	must(t, s.InitRuntimeConfig(ctx, def))
	got, err = s.RuntimeConfig(ctx)
	must(t, err)
	if got.PaidCallsEnabled || !got.VoyageEnabled || got.EligibilityMode != wire.EligibilityAll ||
		len(got.Allowlist) != 1 || got.PersonLimits.Requests != 1 || got.GlobalLimits.Requests != 2 {
		t.Fatalf("partial update = %+v", got)
	}
	if got.AcceptedDomains != nil || got.Admins != nil {
		t.Fatalf("store kept server-config fields: %+v", got)
	}
	empty := []string{}
	got, err = s.UpdateRuntimeConfig(ctx, wire.AdminConfigUpdate{Allowlist: &empty}, entry("config.update"))
	must(t, err)
	if got.Allowlist == nil || len(got.Allowlist) != 0 || got.EligibilityMode != wire.EligibilityAll {
		t.Fatalf("allowlist clear = %+v", got)
	}
}

func auditWithMutation(t *testing.T, s store.Store) {
	ctx := context.Background()
	x := newSession(t, s, "sess-1", "sub-1")
	at := func(i int) wire.AuditEntry {
		e := entry([]string{"first", "second", "third", "fourth"}[i])
		e.At = t0.Add(time.Duration(i) * time.Second)
		return e
	}
	_, err := s.UpdatePerson(ctx, "sub-1", wire.AdminPersonUpdate{}, at(0))
	must(t, err)
	e1 := at(1)
	must(t, s.RevokeSession(ctx, x.ID, "admin", t0, &e1))
	_, err = s.RevokePersonSessions(ctx, "sub-1", "admin", t0, at(2))
	must(t, err)
	must(t, s.InitRuntimeConfig(ctx, wire.AdminConfig{EligibilityMode: wire.EligibilityAll}))
	_, err = s.UpdateRuntimeConfig(ctx, wire.AdminConfigUpdate{}, at(3))
	must(t, err)
	// Failed preconditions record nothing.
	if _, err := s.UpdatePerson(ctx, "missing", wire.AdminPersonUpdate{}, entry("ghost")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update missing = %v", err)
	}
	ghost := entry("ghost")
	if err := s.RevokeSession(ctx, "missing", "admin", t0, &ghost); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke missing = %v", err)
	}
	if _, err := s.RevokePersonSessions(ctx, "missing", "admin", t0, entry("ghost")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoke missing person = %v", err)
	}
	// Logout revocations are not audited.
	y := newSession(t, s, "sess-2", "sub-1")
	must(t, s.RevokeSession(ctx, y.ID, "logout", t0, nil))

	got, err := s.Audit(ctx, 10)
	must(t, err)
	if len(got) != 4 || got[0].Action != "fourth" || got[3].Action != "first" || !got[0].At.Equal(t0.Add(3*time.Second)) || got[0].Actor != "admin@example.test" {
		t.Fatalf("audit = %+v", got)
	}
	if got, _ = s.Audit(ctx, 2); len(got) != 2 || got[1].Action != "third" {
		t.Fatalf("limited audit = %+v", got)
	}
}
