//go:build capture

package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/claude"
	"github.com/atomicobject/rhizome/pkg/harness/codex"
)

// Recaptures the real harness streams the transcript tests replay. Scrub the
// output for paths, accounts, and tokens before committing it.
// Run: go test -tags 'fts5 capture' ./pkg/app/tasks -run TestCapture -v
func TestCaptureHarnessTurns(t *testing.T) {
	for name, driver := range map[string]harness.Harness{"codex": codex.New(), "claude": claude.New()} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			status, err := driver.Status(ctx)
			if err != nil || !status.Installed || !status.LoggedIn {
				t.Skipf("%s not ready: installed=%v loggedIn=%v err=%v", name, status.Installed, status.LoggedIn, err)
			}
			dir := t.TempDir()
			for _, file := range []string{"alpha.txt", "beta.txt"} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			session, err := driver.StartSession(ctx, harness.SessionOptions{Cwd: dir, PermissionMode: harness.PermissionFullAccess, Effort: "low"})
			if err != nil {
				t.Fatal(err)
			}
			out, err := os.Create(filepath.Join("testdata", name+"-turn.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			encoder := json.NewEncoder(out)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for event := range session.Events() {
					if event.Kind == harness.EventApprovalRequested {
						_ = session.Respond(event.RequestID, harness.DecisionDeny)
					}
					_ = encoder.Encode(event)
				}
			}()
			err = session.SendTurn(ctx, "Think briefly, then run the shell command `ls` in the current directory and reply with how many files it lists.")
			_ = session.Stop()
			<-done
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
