package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotable"
)

type dynamoSession struct {
	Session
	Epoch int64
}
type revocation struct {
	At     time.Time
	Reason string
}

// Revoking a person's sessions advances an epoch atomically with an immutable
// revocation record. Every session writer bumps the person's revision, so a
// bulk revoke counts a stable snapshot without a 100-item transaction limit.
func (s *DynamoDB) effective(ctx context.Context, x dynamoSession, p dynamoPerson) (Session, error) {
	if x.RevokedAt.IsZero() && x.Epoch < p.Epoch {
		var r revocation
		i, err := s.table.Get(ctx, "revocations#"+p.Sub, fmt.Sprint(x.Epoch+1), &r)
		if err != nil {
			return Session{}, err
		}
		if i.Revision == "" {
			return Session{}, fmt.Errorf("store: missing revocation epoch")
		}
		x.RevokedAt, x.RevokeReason = r.At, r.Reason
	}
	return x.Session, nil
}
func (s *DynamoDB) CreateSession(ctx context.Context, x Session) error {
	return dynamotable.Retry(ctx, func(ctx context.Context) error {
		pi, p, err := s.person(ctx, x.Sub)
		if err != nil {
			return err
		}
		si, err := s.table.Get(ctx, "sessions", x.ID, nil)
		if err != nil {
			return err
		}
		if si.Revision != "" {
			return ErrStale
		}
		session, err := s.table.Put(si, dynamoSession{Session: x, Epoch: p.Epoch})
		if err != nil {
			return err
		}
		person, err := s.table.Put(pi, p)
		if err != nil {
			return err
		}
		return s.table.Write(ctx, person, session)
	})
}
func (s *DynamoDB) session(ctx context.Context, id string) (dynamotable.Item, dynamoSession, error) {
	var x dynamoSession
	i, err := s.table.Get(ctx, "sessions", id, &x)
	if err == nil && i.Revision == "" {
		err = ErrNotFound
	}
	return i, x, err
}
func (s *DynamoDB) Session(ctx context.Context, id string, now time.Time) (Session, error) {
	_, x, err := s.session(ctx, id)
	if err != nil {
		return Session{}, err
	}
	_, p, err := s.person(ctx, x.Sub)
	if err != nil {
		return Session{}, err
	}
	got, err := s.effective(ctx, x, p)
	if err != nil {
		return Session{}, err
	}
	if !got.RevokedAt.IsZero() {
		return Session{}, ErrRevoked
	}
	if !now.Before(got.AbsoluteExpiresAt) || !now.Before(got.IdleExpiresAt) {
		return Session{}, ErrExpired
	}
	return got, nil
}
func (s *DynamoDB) Sessions(ctx context.Context, sub string) ([]Session, error) {
	items, err := s.table.Query(ctx, "sessions", false, 0)
	if err != nil {
		return nil, err
	}
	people := map[string]dynamoPerson{}
	var out []Session
	for _, i := range items {
		var x dynamoSession
		if err := i.Decode(&x); err != nil {
			return nil, err
		}
		if sub != "" && x.Sub != sub {
			continue
		}
		p, ok := people[x.Sub]
		if !ok {
			_, p, err = s.person(ctx, x.Sub)
			if err != nil {
				return nil, err
			}
			people[x.Sub] = p
		}
		got, err := s.effective(ctx, x, p)
		if err != nil {
			return nil, err
		}
		out = append(out, got)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (s *DynamoDB) RotateSession(ctx context.Context, id string, generation int64, r Rotation) error {
	return dynamotable.Retry(ctx, func(ctx context.Context) error {
		si, x, err := s.session(ctx, id)
		if err == ErrNotFound {
			return ErrStale
		}
		if err != nil {
			return err
		}
		pi, p, err := s.person(ctx, x.Sub)
		if err != nil {
			return err
		}
		got, err := s.effective(ctx, x, p)
		if err != nil {
			return err
		}
		if !got.RevokedAt.IsZero() || x.Generation != generation {
			return ErrStale
		}
		x.AccessHash, x.AccessExpiresAt = r.AccessHash, r.AccessExpiresAt
		x.RefreshHash, x.PrevRefreshHash, x.PrevRefreshDeadline = r.RefreshHash, r.PrevRefreshHash, r.PrevRefreshDeadline
		x.LastUsedAt, x.IdleExpiresAt = r.LastUsedAt, r.IdleExpiresAt
		x.Generation++
		session, err := s.table.Put(si, x)
		if err != nil {
			return err
		}
		person, err := s.table.Put(pi, p)
		if err != nil {
			return err
		}
		return s.table.Write(ctx, person, session)
	})
}
func (s *DynamoDB) RevokeSession(ctx context.Context, id, reason string, now time.Time, audit *wire.AuditEntry) error {
	return dynamotable.Retry(ctx, func(ctx context.Context) error {
		si, x, err := s.session(ctx, id)
		if err != nil {
			return err
		}
		pi, p, err := s.person(ctx, x.Sub)
		if err != nil {
			return err
		}
		got, err := s.effective(ctx, x, p)
		if err != nil {
			return err
		}
		x.Session = got
		if x.RevokedAt.IsZero() {
			x.RevokedAt, x.RevokeReason = now, reason
		}
		session, err := s.table.Put(si, x)
		if err != nil {
			return err
		}
		person, err := s.table.Put(pi, p)
		if err != nil {
			return err
		}
		if audit != nil {
			return s.audited(ctx, *audit, person, session)
		}
		return s.table.Write(ctx, person, session)
	})
}
func (s *DynamoDB) RevokePersonSessions(ctx context.Context, sub, reason string, now time.Time, audit wire.AuditEntry) (int, error) {
	count := 0
	err := dynamotable.Retry(ctx, func(ctx context.Context) error {
		pi, p, err := s.person(ctx, sub)
		if err != nil {
			return err
		}
		sessions, err := s.Sessions(ctx, sub)
		if err != nil {
			return err
		}
		count = 0
		for _, x := range sessions {
			if x.RevokedAt.IsZero() {
				count++
			}
		}
		p.Epoch++
		person, err := s.table.Put(pi, p)
		if err != nil {
			return err
		}
		marker, err := s.table.Put(dynamotable.NewItem("revocations#"+sub, fmt.Sprint(p.Epoch)), revocation{At: now, Reason: reason})
		if err != nil {
			return err
		}
		return s.audited(ctx, audit, person, marker)
	})
	return count, err
}
