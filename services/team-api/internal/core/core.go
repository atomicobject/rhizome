// Package core holds the few types the gateway's identity side (auth, store,
// admin) and traffic side (proxy, admission, ledger) share.
package core

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// Principal is the authenticated caller of a gateway request.
type Principal struct {
	Sub       string
	Email     string
	SessionID string
	Admin     bool
	// Limits are the person's effective daily limits: their override, or the
	// runtime defaults.
	Limits wire.DailyLimits
}

type principalKey struct{}

// WithPrincipal returns a context carrying p. Only the auth middleware calls it.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the request's principal, if authentication ran.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Runtime is the current runtime configuration, cached by its source.
type Runtime = wire.AdminConfig

// RuntimeSource supplies the runtime configuration. The identity side owns it.
type RuntimeSource interface {
	Runtime(ctx context.Context) (Runtime, error)
}

// UsageReader reports one UTC day's usage. The traffic side's ledger owns it;
// the admin API reads it. date is YYYY-MM-DD.
type UsageReader interface {
	Usage(ctx context.Context, date string) (global wire.DailyUsage, people map[string]wire.DailyUsage, err error)
}
