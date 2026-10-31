package teamaccess

import (
	"context"
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/session"
)

// check routes each origin to the right guard: remote origins pass, the
// default development origin is started on demand, a fixture's explicit
// origin may use fake providers, and any other loopback origin must be live.
func TestCheckRoutesOrigins(t *testing.T) {
	errEnsure, errLive := errors.New("ensure"), errors.New("require live")
	env := map[string]string{}
	a := &access{development: true, getenv: func(k string) string { return env[k] },
		ensure:      func(context.Context, string) error { return errEnsure },
		requireLive: func(context.Context, string) error { return errLive }}
	ctx := context.Background()

	if err := a.check(ctx, "https://team.example.test"); err != nil {
		t.Fatalf("remote origin: %v", err)
	}
	if err := a.check(ctx, session.DevelopmentOrigin); !errors.Is(err, errEnsure) {
		t.Fatalf("development origin: %v, want ensure", err)
	}
	if err := a.check(ctx, "http://127.0.0.1:8788"); !errors.Is(err, errLive) {
		t.Fatalf("selector loopback origin: %v, want require live", err)
	}
	env[session.OriginEnv] = "http://127.0.0.1:8788/"
	if err := a.check(ctx, "http://127.0.0.1:8788"); err != nil {
		t.Fatalf("explicit fixture origin (trailing slash): %v", err)
	}
	a.development = false
	if err := a.check(ctx, session.DevelopmentOrigin); !errors.Is(err, errLive) {
		t.Fatalf("release build, loopback origin: %v, want require live", err)
	}
}
