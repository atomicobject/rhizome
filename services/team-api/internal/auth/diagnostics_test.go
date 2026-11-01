package auth

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
)

func TestDiagnosticsRejectForgedIdentityValues(t *testing.T) {
	var out bytes.Buffer
	rec := diagnostics.OpenStream(&out, diagnostics.Options{})
	RecordEvent(context.Background(), rec, "auth", "auth.refresh", "session_revoked", time.Millisecond,
		slog.String("session_id", "rzt_a_synthetic.credential"), slog.String("target", "person@example.com"))
	for _, forbidden := range []string{"rzt_a_synthetic", "credential", "person@example.com"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("log leaked %q", forbidden)
		}
	}
}
