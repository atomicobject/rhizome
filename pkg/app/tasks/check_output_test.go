package tasks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckOutputRequiresOneCompleteJSONValue(t *testing.T) {
	shellAvailable(t)
	for _, mode := range []string{"scheduled", "draft"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name, output, outcome string
			}{
				{"closing bracket", `{"needed":true}]`, OutcomeFailed},
				{"closing brace and garbage", `{"needed":false}}garbage`, OutcomeFailed},
				{"second value", `{"needed":false} {}`, OutcomeFailed},
				{"valid object", `{"needed":false}`, OutcomeNotNeeded},
				{"trailing whitespace", "{\"needed\":false} \n\t", OutcomeNotNeeded},
			} {
				t.Run(tc.name, func(t *testing.T) {
					dir := newRepo(t, map[string]string{"check.yml": checkingTask})
					commitFile(t, dir, "check.sh", "printf '%s' '"+tc.output+"'\n", "set check output")
					agent := &fakeAgent{}
					svc := newService(t, dir, agent, "auto")
					var outcome Outcome
					if mode == "scheduled" {
						summary, err := svc.RunDue(context.Background())
						require.NoError(t, err)
						require.Len(t, summary.Outcomes, 1)
						outcome = summary.Outcomes[0]
					} else {
						var err error
						outcome, err = svc.RunDraft(context.Background(), filepath.Join(dir, TasksDir, "check.yml"))
						require.NoError(t, err)
					}
					require.Equal(t, tc.outcome, outcome.Outcome)
					require.Zero(t, agent.calls(), "a failed or unnecessary check must not start agent work")
				})
			}
		})
	}
}
