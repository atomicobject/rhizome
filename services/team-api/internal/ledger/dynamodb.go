package ledger

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/dynamotable"
)

type DynamoDB struct {
	table *dynamotable.Table
	owner string
	now   func() time.Time
}

type ownerLease struct {
	ExpiresAt time.Time
	Retired   bool
}
type storedReceipt struct {
	Receipt
	Day, State, Owner string
	Settled           *int64
}

func OpenDynamoDB(ctx context.Context, table *dynamotable.Table, now func() time.Time) (*DynamoDB, error) {
	if now == nil {
		now = time.Now
	}
	l := &DynamoDB{table: table, owner: newOwner(), now: now}
	put, err := table.Put(dynamotable.NewItem("owners", l.owner), ownerLease{ExpiresAt: now().Add(LeaseDuration)})
	if err != nil {
		return nil, err
	}
	if err = table.Write(ctx, put); err != nil {
		return nil, err
	}
	return l, nil
}
func (l *DynamoDB) Day() string { return l.now().UTC().Format(time.DateOnly) }
func (l *DynamoDB) lease(ctx context.Context) (dynamotable.Item, ownerLease, error) {
	var lease ownerLease
	i, err := l.table.Get(ctx, "owners", l.owner, &lease)
	if err == nil && (i.Revision == "" || lease.Retired || !l.now().Before(lease.ExpiresAt)) {
		err = ErrPrecondition
	}
	return i, lease, err
}
func (l *DynamoDB) Heartbeat(ctx context.Context) error {
	return dynamotable.Retry(ctx, func(ctx context.Context) error {
		i, lease, err := l.lease(ctx)
		if err != nil {
			return err
		}
		lease.ExpiresAt = l.now().Add(LeaseDuration)
		put, err := l.table.Put(i, lease)
		if err != nil {
			return err
		}
		return l.table.Write(ctx, put)
	})
}

// Recover first permanently fences expired owners. Heartbeats and fencing
// compare the same lease revision, so exactly one can win. Receipt recovery
// then commits with both day counters and a condition on the fenced owner.
func (l *DynamoDB) Recover(ctx context.Context) error {
	owners, err := l.table.Query(ctx, "owners", false, 0)
	if err != nil {
		return err
	}
	for _, o := range owners {
		var observed ownerLease
		if err := o.Decode(&observed); err != nil {
			return err
		}
		if observed.Retired || l.now().Before(observed.ExpiresAt) {
			continue
		}
		err = dynamotable.Retry(ctx, func(ctx context.Context) error {
			var lease ownerLease
			i, err := l.table.Get(ctx, "owners", o.SK, &lease)
			if err != nil {
				return err
			}
			if lease.Retired || l.now().Before(lease.ExpiresAt) {
				return nil
			}
			lease.Retired = true
			put, err := l.table.Put(i, lease)
			if err != nil {
				return err
			}
			return l.table.Write(ctx, put)
		})
		if err != nil {
			return err
		}
	}
	receipts, err := l.table.Query(ctx, "receipts", false, 0)
	if err != nil {
		return err
	}
	for _, i := range receipts {
		var observed storedReceipt
		if err := i.Decode(&observed); err != nil {
			return err
		}
		if observed.State != stateReserved && observed.State != stateDispatched {
			continue
		}
		if err := l.recoverReceipt(ctx, i.SK); err != nil {
			return err
		}
	}
	return nil
}
func (l *DynamoDB) recoverReceipt(ctx context.Context, key string) error {
	return dynamotable.Retry(ctx, func(ctx context.Context) error {
		var r storedReceipt
		i, err := l.table.Get(ctx, "receipts", key, &r)
		if err != nil {
			return err
		}
		if r.State != stateReserved && r.State != stateDispatched {
			return nil
		}
		var lease ownerLease
		owner, err := l.table.Get(ctx, "owners", r.Owner, &lease)
		if err != nil {
			return err
		}
		if owner.Revision != "" && !lease.Retired {
			return nil
		}
		r.State = stateUnknown
		return l.change(ctx, i, r, l.table.Check(owner), func(u *wire.DailyUsage) { u.Unknown++ })
	})
}
func receiptKey(sub, key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(sub)) + ":" + key
}
func (l *DynamoDB) Reserve(ctx context.Context, r Receipt, person, everyone wire.DailyLimits) (wire.ErrorCode, error) {
	if err := validReceipt(r); err != nil {
		return "", err
	}
	err := dynamotable.Retry(context.WithoutCancel(ctx), func(ctx context.Context) error {
		owner, _, err := l.lease(ctx)
		if err != nil {
			return err
		}
		var prev storedReceipt
		i, err := l.table.Get(ctx, "receipts", receiptKey(r.Sub, r.Key), &prev)
		if err != nil {
			return err
		}
		if i.Revision != "" {
			if prev.Route != r.Route || prev.BodyHash != r.BodyHash {
				return codeErr{wire.CodeIdempotencyClash}
			}
			switch prev.State {
			case stateReserved, stateDispatched:
				return codeErr{wire.CodeInProgress}
			case stateSucceeded, stateRejected:
				return codeErr{wire.CodeResultLost}
			case stateUnknown:
				return codeErr{wire.CodeOutcomeUnknown}
			}
		}
		day := l.Day()
		writes := []types.TransactWriteItem{l.table.Check(owner)}
		for _, scope := range []struct {
			sub    string
			limits wire.DailyLimits
		}{{r.Sub, person}, {global, everyone}} {
			var u wire.DailyUsage
			counter, err := l.table.Get(ctx, "day#"+day, dayKey(scope.sub), &u)
			if err != nil {
				return err
			}
			if exceeds(u, r, scope.limits) {
				return codeErr{wire.CodeBudgetExhausted}
			}
			u.Requests++
			u.InputBytes += r.InputBytes
			u.ReservedCredits += r.Credits
			put, err := l.table.Put(counter, u)
			if err != nil {
				return err
			}
			writes = append(writes, put)
		}
		put, err := l.table.Put(i, storedReceipt{Receipt: r, Day: day, State: stateReserved, Owner: l.owner})
		if err != nil {
			return err
		}
		return l.table.Write(ctx, append(writes, put)...)
	})
	var ce codeErr
	if errors.As(err, &ce) {
		return ce.code, nil
	}
	return "", err
}
func dayKey(sub string) string {
	if sub == global {
		return "global"
	}
	return "person#" + sub
}
func (l *DynamoDB) MarkDispatched(ctx context.Context, sub, key string) error {
	return l.transition(ctx, sub, key, stateReserved, stateDispatched, nil)
}
func (l *DynamoDB) MarkUnknown(ctx context.Context, sub, key string) error {
	return l.transition(ctx, sub, key, stateDispatched, stateUnknown, nil)
}
func (l *DynamoDB) Settle(ctx context.Context, sub, key string, outcome Outcome, actual int64) error {
	to, ok := map[Outcome]string{NotDispatched: stateNotDispatched, Succeeded: stateSucceeded, Rejected: stateRejected}[outcome]
	if !ok {
		return fmt.Errorf("ledger: unknown outcome %d", outcome)
	}
	var settled *int64
	if outcome == Succeeded && actual >= 0 {
		settled = &actual
	}
	return l.transition(ctx, sub, key, stateDispatched, to, settled)
}
func (l *DynamoDB) transition(ctx context.Context, sub, key, from, to string, settled *int64) error {
	return dynamotable.Retry(context.WithoutCancel(ctx), func(ctx context.Context) error {
		owner, _, err := l.lease(ctx)
		if err != nil {
			return err
		}
		var r storedReceipt
		i, err := l.table.Get(ctx, "receipts", receiptKey(sub, key), &r)
		if err != nil {
			return err
		}
		if i.Revision == "" || r.State != from || r.Owner != l.owner {
			return ErrPrecondition
		}
		r.State, r.Settled = to, settled
		var adjust func(*wire.DailyUsage)
		if to != stateDispatched {
			adjust = func(u *wire.DailyUsage) {
				switch to {
				case stateUnknown:
					u.Unknown++
				case stateNotDispatched:
					u.Requests--
					u.InputBytes -= r.InputBytes
					u.ReservedCredits -= r.Credits
				case stateRejected:
					u.ReservedCredits -= r.Credits
				case stateSucceeded:
					if settled != nil {
						u.ReservedCredits += *settled - r.Credits
						u.SettledCredits += *settled
					}
				}
			}
		}
		return l.change(ctx, i, r, l.table.Check(owner), adjust)
	})
}
func (l *DynamoDB) change(ctx context.Context, i dynamotable.Item, r storedReceipt, owner types.TransactWriteItem, adjust func(*wire.DailyUsage)) error {
	put, err := l.table.Put(i, r)
	if err != nil {
		return err
	}
	writes := []types.TransactWriteItem{owner, put}
	if adjust != nil {
		for _, sub := range []string{r.Sub, global} {
			var u wire.DailyUsage
			counter, err := l.table.Get(ctx, "day#"+r.Day, dayKey(sub), &u)
			if err != nil {
				return err
			}
			if counter.Revision == "" {
				return errors.New("ledger: receipt counter missing")
			}
			adjust(&u)
			put, err := l.table.Put(counter, u)
			if err != nil {
				return err
			}
			writes = append(writes, put)
		}
	}
	return l.table.Write(ctx, writes...)
}
func (l *DynamoDB) Usage(ctx context.Context, date string) (wire.DailyUsage, map[string]wire.DailyUsage, error) {
	var everyone wire.DailyUsage
	people := map[string]wire.DailyUsage{}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return everyone, nil, fmt.Errorf("ledger: date must be YYYY-MM-DD")
	}
	items, err := l.table.Query(ctx, "day#"+date, false, 0)
	if err != nil {
		return everyone, nil, err
	}
	for _, i := range items {
		var u wire.DailyUsage
		if err := i.Decode(&u); err != nil {
			return everyone, nil, err
		}
		if i.SK == "global" {
			everyone = u
		} else {
			people[i.SK[len("person#"):]] = u
		}
	}
	return everyone, people, nil
}
