package codex

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// Opt-in live check against the installed, logged-in codex CLI.
// Run: RHIZOME_HARNESS_LIVE=1 go test -tags fts5 ./pkg/harness/codex -run TestLive -v
func TestLiveCodex(t *testing.T) {
	if os.Getenv("RHIZOME_HARNESS_LIVE") == "" {
		t.Skip("set RHIZOME_HARNESS_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	driver := New()

	status, err := driver.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	t.Logf("status: %+v", status)
	if !status.Installed || !status.LoggedIn {
		t.Fatalf("expected installed and logged in: %+v", status)
	}

	out, err := driver.Generate(ctx, harness.GenerateRequest{
		Prompt: "Return the JSON object {\"answer\": \"pong\"}.",
		Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	t.Logf("generate: %s", out)

	session, err := driver.StartSession(ctx, harness.SessionOptions{Cwd: t.TempDir(), PermissionMode: harness.PermissionApprovalRequired})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range session.Events() {
			t.Logf("event %s turn=%s item=%s text=%q name=%s cmd=%q status=%s reason=%q", ev.Kind, ev.TurnID, ev.ItemID, ev.Text, ev.Name, ev.Command, ev.Status, ev.Reason)
			if ev.Kind == harness.EventApprovalRequested {
				_ = session.Respond(ev.RequestID, harness.DecisionDeny)
			}
		}
	}()
	if err := session.SendTurn(ctx, "Reply with the single word pong."); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if err := session.Stop(); err != nil {
		t.Logf("stop: %v", err)
	}
	<-done
}
