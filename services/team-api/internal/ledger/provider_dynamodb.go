package ledger

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type storedDailyUsage struct {
	wire.DailyUsage
	BudgetedRequests int64
}

func providerDayKey(provider, sub string) string {
	return provider + ":" + base64.RawURLEncoding.EncodeToString([]byte(sub))
}

func (l *DynamoDB) reserveProvider(ctx context.Context, day string, r Receipt) ([]types.TransactWriteItem, error) {
	if !budgeted(r) {
		return nil, nil
	}
	var fence providerFence
	fenceItem, err := l.table.Get(ctx, "provider-fences", r.Provider, &fence)
	if err != nil {
		return nil, err
	}
	if fence.Fenced {
		return nil, codeErr{wire.CodeBudgetExhausted}
	}
	// A concurrent bound breach changes this revision (or inserts the marker),
	// invalidating admission even when its own day's counters are unchanged.
	writes := []types.TransactWriteItem{l.table.Check(fenceItem)}
	for _, sub := range []string{global, r.Sub} {
		var u providerCounter
		i, err := l.table.Get(ctx, "provider-day#"+day, providerDayKey(r.Provider, sub), &u)
		if err != nil {
			return nil, err
		}
		if sub == global && (u.Fenced || r.ReservedNanodollars > r.ProviderLimitNanodollars-u.ReservedNanodollars) {
			return nil, codeErr{wire.CodeBudgetExhausted}
		}
		u.Requests++
		u.ReservedNanodollars += r.ReservedNanodollars
		put, err := l.table.Put(i, u)
		if err != nil {
			return nil, err
		}
		writes = append(writes, put)
	}
	return writes, nil
}

func (l *DynamoDB) adjustProvider(ctx context.Context, r storedReceipt, adjust func(*providerCounter)) ([]types.TransactWriteItem, error) {
	var writes []types.TransactWriteItem
	for _, sub := range []string{global, r.Sub} {
		var u providerCounter
		i, err := l.table.Get(ctx, "provider-day#"+r.Day, providerDayKey(r.Provider, sub), &u)
		if err != nil {
			return nil, err
		}
		if i.Revision == "" || u.Requests <= 0 || u.ReservedNanodollars < r.ReservedNanodollars {
			return nil, errors.New("ledger: provider receipt counter missing or inconsistent")
		}
		adjust(&u)
		if sub == global && u.Fenced {
			var fence providerFence
			fenceItem, err := l.table.Get(ctx, "provider-fences", r.Provider, &fence)
			if err != nil {
				return nil, err
			}
			put, err := l.table.Put(fenceItem, providerFence{Fenced: true})
			if err != nil {
				return nil, err
			}
			writes = append(writes, put)
		}
		put, err := l.table.Put(i, u)
		if err != nil {
			return nil, err
		}
		writes = append(writes, put)
	}
	return writes, nil
}

func (l *DynamoDB) ProviderUsage(ctx context.Context, date string) (map[string]wire.ProviderUsage, map[string]map[string]wire.ProviderUsage, error) {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return nil, nil, fmt.Errorf("ledger: date must be YYYY-MM-DD")
	}
	items, err := l.table.Query(ctx, "provider-day#"+date, false, 0)
	if err != nil {
		return nil, nil, err
	}
	everyone := map[string]wire.ProviderUsage{}
	people := map[string]map[string]wire.ProviderUsage{}
	for _, i := range items {
		provider, encodedSub, ok := strings.Cut(i.SK, ":")
		if !ok {
			return nil, nil, errors.New("ledger: malformed provider counter key")
		}
		sub, err := base64.RawURLEncoding.DecodeString(encodedSub)
		if err != nil {
			return nil, nil, err
		}
		var u providerCounter
		if err := i.Decode(&u); err != nil {
			return nil, nil, err
		}
		if string(sub) == global {
			everyone[provider] = u
		} else {
			if people[string(sub)] == nil {
				people[string(sub)] = map[string]wire.ProviderUsage{}
			}
			people[string(sub)][provider] = u
		}
	}
	// Fenced means current admission status, including a breach on a different
	// day. Synthesize an empty usage row when this date has no provider traffic.
	fences, err := l.table.Query(ctx, "provider-fences", false, 0)
	if err != nil {
		return nil, nil, err
	}
	for _, i := range fences {
		var fence providerFence
		if err := i.Decode(&fence); err != nil {
			return nil, nil, err
		}
		if fence.Fenced {
			u := everyone[i.SK]
			u.Fenced = true
			everyone[i.SK] = u
		}
	}
	return everyone, people, nil
}
