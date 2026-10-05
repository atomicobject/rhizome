package claude

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// Run: RHIZOME_HARNESS_LIVE=1 go test -tags fts5 ./pkg/harness/claude -run TestLive -v
func TestLiveClaude(t *testing.T) {
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
	out, err := driver.Generate(ctx, harness.GenerateRequest{Prompt: `Return {"answer":"pong"}.`, Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`)})
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
		for event := range session.Events() {
			t.Logf("event: %+v", event)
			if event.Kind == harness.EventApprovalRequested {
				_ = session.Respond(event.RequestID, harness.DecisionDeny)
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
