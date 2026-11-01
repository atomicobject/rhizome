package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotable"
)

// DynamoDB implements Store using version-checked business transactions.
type DynamoDB struct{ table *dynamotable.Table }

func NewDynamoDB(table *dynamotable.Table) *DynamoDB { return &DynamoDB{table: table} }

var _ Store = (*DynamoDB)(nil)

type dynamoPerson struct {
	Person
	Epoch int64
}

func (s *DynamoDB) person(ctx context.Context, sub string) (dynamotable.Item, dynamoPerson, error) {
	var p dynamoPerson
	i, err := s.table.Get(ctx, "people", sub, &p)
	if err == nil && i.Revision == "" {
		err = ErrNotFound
	}
	return i, p, err
}
func (s *DynamoDB) Person(ctx context.Context, sub string) (Person, error) {
	_, p, err := s.person(ctx, sub)
	return p.Person, err
}
func (s *DynamoDB) UpsertPerson(ctx context.Context, sub, email, hd string, now time.Time) (Person, error) {
	var p dynamoPerson
	err := dynamotable.Retry(ctx, func(ctx context.Context) error {
		i, err := s.table.Get(ctx, "people", sub, &p)
		if err != nil {
			return err
		}
		if i.Revision == "" {
			p = dynamoPerson{Person: Person{Sub: sub, Status: wire.PersonActive, CreatedAt: now}}
		}
		p.Email, p.HostedDomain = email, hd
		put, err := s.table.Put(i, p)
		if err != nil {
			return err
		}
		return s.table.Write(ctx, put)
	})
	return p.Person, err
}
func (s *DynamoDB) People(ctx context.Context) ([]Person, error) {
	items, err := s.table.Query(ctx, "people", false, 0)
	if err != nil {
		return nil, err
	}
	var out []Person
	for _, i := range items {
		var p dynamoPerson
		if err := i.Decode(&p); err != nil {
			return nil, err
		}
		out = append(out, p.Person)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}
func (s *DynamoDB) UpdatePerson(ctx context.Context, sub string, u wire.AdminPersonUpdate, audit wire.AuditEntry) (Person, error) {
	var p dynamoPerson
	err := dynamotable.Retry(ctx, func(ctx context.Context) error {
		i, got, err := s.person(ctx, sub)
		if err != nil {
			return err
		}
		p = got
		if u.Status != nil {
			p.Status = *u.Status
		}
		if u.ClearLimits {
			p.Limits = nil
		}
		if u.Limits != nil {
			v := *u.Limits
			p.Limits = &v
		}
		put, err := s.table.Put(i, p)
		if err != nil {
			return err
		}
		return s.audited(ctx, audit, put)
	})
	return p.Person, err
}

// audited serializes audit order with the mutation, including callers whose
// wall clocks disagree. No failed precondition can leave an audit entry.
func (s *DynamoDB) audited(ctx context.Context, e wire.AuditEntry, writes ...types.TransactWriteItem) error {
	var seq int64
	i, err := s.table.Get(ctx, "audit-sequence", "current", &seq)
	if err != nil {
		return err
	}
	seq++
	next, err := s.table.Put(i, seq)
	if err != nil {
		return err
	}
	entry, err := s.table.Put(dynamotable.NewItem("audit", fmt.Sprintf("%020d", seq)), e)
	if err != nil {
		return err
	}
	return s.table.Write(ctx, append(writes, next, entry)...)
}
func (s *DynamoDB) Audit(ctx context.Context, limit int) ([]wire.AuditEntry, error) {
	out := []wire.AuditEntry{}
	if limit == 0 {
		return out, nil
	}
	items, err := s.table.Query(ctx, "audit", true, limit)
	if err != nil {
		return nil, err
	}
	for _, i := range items {
		var e wire.AuditEntry
		if err := i.Decode(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
func (s *DynamoDB) InitRuntimeConfig(ctx context.Context, defaults wire.AdminConfig) error {
	return dynamotable.Retry(ctx, func(ctx context.Context) error {
		i, err := s.table.Get(ctx, "config", "runtime", nil)
		if err != nil || i.Revision != "" {
			return err
		}
		body, err := encodeRuntime(defaults)
		if err != nil {
			return err
		}
		put, err := s.table.Put(i, runtimeDocument{Body: body})
		if err != nil {
			return err
		}
		return s.table.Write(ctx, put)
	})
}

type runtimeDocument struct{ Body string }

func (s *DynamoDB) runtime(ctx context.Context) (dynamotable.Item, wire.AdminConfig, error) {
	var doc runtimeDocument
	i, err := s.table.Get(ctx, "config", "runtime", &doc)
	if err != nil {
		return i, wire.AdminConfig{}, err
	}
	if i.Revision == "" {
		return i, wire.AdminConfig{}, ErrNotFound
	}
	c, err := decodeRuntime(doc.Body)
	return i, c, err
}
func (s *DynamoDB) RuntimeConfig(ctx context.Context) (wire.AdminConfig, error) {
	_, c, err := s.runtime(ctx)
	return c, err
}
func (s *DynamoDB) UpdateRuntimeConfig(ctx context.Context, u wire.AdminConfigUpdate, audit wire.AuditEntry) (wire.AdminConfig, error) {
	var c wire.AdminConfig
	err := dynamotable.Retry(ctx, func(ctx context.Context) error {
		i, got, err := s.runtime(ctx)
		if err != nil {
			return err
		}
		c = got
		applyRuntime(&c, u)
		body, err := encodeRuntime(c)
		if err != nil {
			return err
		}
		put, err := s.table.Put(i, runtimeDocument{Body: body})
		if err != nil {
			return err
		}
		return s.audited(ctx, audit, put)
	})
	return c, err
}
