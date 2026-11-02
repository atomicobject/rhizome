package ledger

import "github.com/atomicobject/rhizome/pkg/teamapi/wire"

const providerAccountingVersion = 1

type providerCounter = wire.ProviderUsage

// providerFence is independent of admission dates. A breached pricing bound
// invalidates future reservations until an operator reviews the assumption.
type providerFence struct {
	Fenced bool
}

func settledCurrency(r Receipt, state string, actual []int64) *int64 {
	if budgeted(r) && state == stateSucceeded && len(actual) == 1 && actual[0] >= 0 && actual[0] <= r.ReservedNanodollars {
		return &actual[0]
	}
	return nil
}

func budgeted(r Receipt) bool {
	return r.AccountingVersion == providerAccountingVersion && r.Provider != "" && r.PriceVersion != ""
}

// Currency transitions use the admission's immutable price snapshot. An
// unverified response never frees a reservation. Only a proven no-write
// failure can remove a provider request entirely.
func providerAdjustment(r Receipt, state string, actual []int64) (func(*providerCounter), bool) {
	if !budgeted(r) || state == stateDispatched {
		return nil, false
	}
	known := settledCurrency(r, state, actual) != nil
	overbound := state == stateSucceeded && len(actual) == 1 && actual[0] > r.ReservedNanodollars
	return func(u *providerCounter) {
		switch {
		case state == stateNotDispatched:
			u.Requests--
			u.ReservedNanodollars -= r.ReservedNanodollars
		case known:
			u.ReservedNanodollars += actual[0] - r.ReservedNanodollars
			u.SettledNanodollars += actual[0]
		default:
			u.Unknown++
		}
		u.Fenced = u.Fenced || overbound
	}, state == stateSucceeded && !known
}
