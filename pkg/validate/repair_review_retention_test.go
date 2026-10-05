package validate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRepairReviewCapacityPreservesTerminalRetryUntilExpiry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "applied"
		if fail {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
			store := NewRepairReviewStore(RepairReviewStoreOptions{TTL: time.Minute, MaxPerVault: 1, Now: func() time.Time { return now }})
			root := t.TempDir()
			before := []byte("before\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0600))
			planned := repairReviewFixtureResult(t, []FixAction{{ID: "action", Check: CheckLinkHygiene, Kind: FixKindRewriteLinkGroup, Safety: FixSafetySafe, Title: "Repair", IssueKeys: []string{"issue"}}}, []RepairOperation{repairReviewWrite("op", "action", []string{"issue"}, "note.md", before, []byte("after\n"), "")})
			create := RepairReviewCreateRequest{VaultIdentity: "vault:test", Generation: 4, PlanFingerprint: planned.FixPlan.Fingerprint, ActionIDs: []string{"action"}, Result: planned, RunContext: RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t)}}
			review, err := store.Create(context.Background(), create)
			require.NoError(t, err)
			apply := RepairReviewApplyRequest{Generation: 4, PlanFingerprint: review.PlanFingerprint, SelectionFingerprint: review.SelectionFingerprint}
			if !fail {
				apply.Options = Options{PostApplyRefresher: &repairPostApplyProbe{t: t}}
			}
			result, execution, applyErr := store.Apply(context.Background(), review.ID, apply)
			if fail {
				require.Error(t, applyErr)
			} else {
				require.NoError(t, applyErr)
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), before, 0600))
			_, err = store.Create(context.Background(), create)
			require.Error(t, err, "capacity must reject new work rather than discard unexpired terminal evidence")
			retried, retryExecution, retryErr := store.Apply(context.Background(), review.ID, apply)
			wantJSON, err := json.Marshal(result)
			require.NoError(t, err)
			gotJSON, err := json.Marshal(retried)
			require.NoError(t, err)
			require.JSONEq(t, string(wantJSON), string(gotJSON))
			require.Equal(t, before, mustReadFile(t, filepath.Join(root, "note.md")))
			wantJSON, err = json.Marshal(execution)
			require.NoError(t, err)
			gotJSON, err = json.Marshal(retryExecution)
			require.NoError(t, err)
			require.JSONEq(t, string(wantJSON), string(gotJSON))
			if fail {
				require.EqualError(t, retryErr, applyErr.Error())
			} else {
				require.NoError(t, retryErr)
			}
			now = now.Add(2 * time.Minute)
			_, err = store.Create(context.Background(), create)
			require.NoError(t, err, "expired terminal evidence can be reclaimed")
		})
	}
}
