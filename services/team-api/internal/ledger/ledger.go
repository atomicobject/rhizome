// Package ledger defines atomic paid-request accounting operations. Receipts
// and their per-person and global counters always change together.
package ledger

import (
	"context"
	"errors"
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

// ErrPrecondition means the receipt state, ownership, or live lease did not
// match the operation preconditions. Nothing changed.
var ErrPrecondition = errors.New("ledger: receipt state or owner lease precondition failed")

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
	Provider, PriceVersion           string
	ReservedNanodollars              int64
	PriceNanodollarsPerToken         int64
	MaxBillableTokens                int64
	ProviderLimitNanodollars         int64
	AccountingVersion                int
}

// LeaseDuration bounds crash recovery delay. Owners heartbeat every third of
// this interval and must never reuse an instance ID or renew an expired lease.
const LeaseDuration = time.Minute

// Ledger owns one process instance. All receipt transitions require that
// instance's live lease and receipt ownership, or return ErrPrecondition.
// Recover is safe during traffic: it only retires expired owners and marks
// their unfinished receipts unknown, keeping reservations.
type Ledger interface {
	// Heartbeat extends this instance's live lease; an expired or fenced owner
	// returns ErrPrecondition. Recover never needs a live lease.
	Heartbeat(context.Context) error
	Recover(context.Context) error
	Day() string
	// Reserve atomically checks both daily budgets and idempotency, inserting
	// the receipt and its reservation. Only NotDispatched keys can be readmitted.
	Reserve(context.Context, Receipt, wire.DailyLimits, wire.DailyLimits) (wire.ErrorCode, error)
	// MarkDispatched requires reserved. Call it before any upstream byte.
	MarkDispatched(context.Context, string, string) error
	// MarkUnknown requires dispatched and keeps the full reservation.
	MarkUnknown(context.Context, string, string) error
	// Settle requires dispatched, applies an Outcome once to the admission
	// day, and rejects late settlement after recovery. Negative actual credits
	// mean unknown usage, preserving the reservation on success. For currency
	// receipts the optional actual nanodollars must be known and within the
	// recorded reservation; otherwise both holds remain and usage is unknown.
	Settle(context.Context, string, string, Outcome, int64, ...int64) error
	Usage(context.Context, string) (wire.DailyUsage, map[string]wire.DailyUsage, error)
	ProviderUsage(context.Context, string) (map[string]wire.ProviderUsage, map[string]map[string]wire.ProviderUsage, error)
}

type codeErr struct{ code wire.ErrorCode }

func (e codeErr) Error() string { return string(e.code) }

func validReceipt(r Receipt) error {
	if r.Sub == "" || r.Key == "" || r.InputBytes < 0 || r.Credits < 0 {
		return errors.New("ledger: receipt requires a person, key, and nonnegative reservation")
	}
	if r.Provider == "" && (r.PriceVersion != "" || r.ReservedNanodollars != 0 || r.PriceNanodollarsPerToken != 0 || r.MaxBillableTokens != 0 || r.ProviderLimitNanodollars != 0 || r.AccountingVersion != 0) {
		return errors.New("ledger: currency reservation requires a provider")
	}
	if r.Provider != "" && (r.Provider != "voyage" && r.Provider != "typesafe" || r.PriceVersion == "" || r.ReservedNanodollars <= 0 || r.PriceNanodollarsPerToken <= 0 || r.MaxBillableTokens <= 0 || r.ProviderLimitNanodollars < 0 || r.MaxBillableTokens > r.ReservedNanodollars/r.PriceNanodollarsPerToken) {
		return errors.New("ledger: invalid provider reservation or price bound")
	}
	return nil
}
func exceeds(u wire.DailyUsage, r Receipt, l wire.DailyLimits) bool {
	return u.Requests >= l.Requests || r.InputBytes > l.InputBytes-u.InputBytes || r.Credits > l.ChargeCredits-u.ReservedCredits
}
