package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/stretchr/testify/require"
)

// listingHarness is a harness whose CLI lists the given MCP servers.
type listingHarness struct {
	harnesstest.Harness
	servers []harness.MCPServerState
	listed  int
	dirs    []string
}

func (h *listingHarness) ListMCPServers(_ context.Context, dir string) ([]harness.MCPServerState, error) {
	h.listed++
	h.dirs = append(h.dirs, dir)
	return h.servers, nil
}

// slowListingHarness lists nothing until its context ends.
type slowListingHarness struct{ harnesstest.Harness }

func (*slowListingHarness) ListMCPServers(ctx context.Context, _ string) ([]harness.MCPServerState, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// resolvingAgent is a fakeAgent whose prompt runs would use driver.
type resolvingAgent struct {
	*fakeAgent
	kind   harness.Kind
	driver harness.Harness
}

func (a resolvingAgent) Harness(context.Context, string) (harness.Kind, harness.Harness, error) {
	return a.kind, a.driver, nil
}

func TestTaskFilesDeclareRequiresAndDeny(t *testing.T) {
	def, err := ParseDefinition(".rhizome/tasks/x.yml", []byte("name: x\nevery: 1d\nrequires:\n  commands: [gh, jq]\n  mcp: [linear]\ndeny: [WebFetch, \"Bash(git push:*)\"]\nprompt: x\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"gh", "jq"}, def.Requires.Commands)
	require.Equal(t, []string{"linear"}, def.Requires.MCP)
	require.Equal(t, []string{"WebFetch", "Bash(git push:*)"}, def.Deny)
	for name, body := range map[string]string{
		"a path":                 "name: x\nevery: 1d\nrequires: {commands: [./bin/tool]}\nprompt: x\n",
		"an empty server":        "name: x\nevery: 1d\nrequires: {mcp: [\"\"]}\nprompt: x\n",
		"an unknown requirement": "name: x\nevery: 1d\nrequires: {tools: [x]}\nprompt: x\n",
		"deny without a prompt":  "name: x\nevery: 1d\ncheck: {command: [\"true\"]}\ndeny: [WebFetch]\n",
		"mcp without a prompt":   "name: x\nevery: 1d\ncheck: {command: [\"true\"]}\nrequires: {mcp: [linear]}\n",
	} {
		_, err := ParseDefinition(".rhizome/tasks/x.yml", []byte(body))
		require.Error(t, err, name)
	}
}

const needsTask = `name: Needs
every: 1d
requires:
  commands: [rzm-test-no-such-command]
check:
  command: [sh, check.sh]
prompt: Do it.
`

func TestMissingCommandFailsBeforeTheCheckOrPrompt(t *testing.T) {
	shellAvailable(t)
	dir := newRepo(t, map[string]string{"needs.yml": needsTask})
	commitFile(t, dir, "check.sh", "touch \"$RZM_TASK_DATA_DIR/checked\"\nprintf '{\"needed\": true}'\n", "add check")
	agent := &fakeAgent{}
	svc := newService(t, dir, agent, "auto")
	ctx := context.Background()

	summary, err := svc.RunDue(ctx)
	require.NoError(t, err)
	require.Len(t, summary.Outcomes, 1)
	require.Equal(t, OutcomeFailed, summary.Outcomes[0].Outcome)
	require.Contains(t, summary.Outcomes[0].Reason, `"rzm-test-no-such-command"`)
	require.Zero(t, agent.calls())
	_, err = os.Stat(filepath.Join(svc.stateDir, "data", "needs", "checked"))
	require.ErrorIs(t, err, os.ErrNotExist, "the check must not run")

	st := mustState(t, svc)
	items := taskItems(st, "needs")
	require.Len(t, items, 1)
	require.Equal(t, StateAttention, items[0].State)
	require.Contains(t, items[0].Reason, "missing requirements")
	last, ok := st.LastRun("needs")
	require.True(t, ok)
	require.Len(t, last.Missing, 1)
	require.True(t, requirementsBlocked(&last))

	// A run the person asks for checks requirements too.
	out, err := svc.Run(ctx, items[0].ID)
	require.NoError(t, err)
	require.Equal(t, OutcomeFailed, out.Outcome)
	require.Zero(t, agent.calls())

	// So does a local test of a draft.
	writeFile(t, filepath.Join(dir, TasksDir, "draft.yml"), needsTask)
	out, err = svc.RunDraft(ctx, filepath.Join(dir, TasksDir, "draft.yml"))
	require.NoError(t, err)
	require.Equal(t, OutcomeFailed, out.Outcome)
	require.Contains(t, out.Reason, `"rzm-test-no-such-command"`)
	require.Zero(t, agent.calls())
}

func TestMCPAndDenyRequirementsAskTheTasksHarness(t *testing.T) {
	const task = "name: Linear\nevery: 1d\nrequires: {mcp: [linear, files]}\ndeny: [WebFetch]\nprompt: Triage.\n"
	ctx := context.Background()

	t.Run("an unusable server fails the run", func(t *testing.T) {
		dir := newRepo(t, map[string]string{"linear.yml": task})
		driver := &listingHarness{servers: []harness.MCPServerState{{Name: "linear", Status: "connected", Usable: true}, {Name: "files", Status: "failed to connect"}}}
		agent := resolvingAgent{fakeAgent: &fakeAgent{}, kind: harness.KindClaude, driver: driver}
		summary, err := newService(t, dir, agent, "auto").RunDue(ctx)
		require.NoError(t, err)
		require.Equal(t, OutcomeFailed, summary.Outcomes[0].Outcome)
		require.Contains(t, summary.Outcomes[0].Reason, `MCP server "files" for claude reports "failed to connect"`)
		require.NotContains(t, summary.Outcomes[0].Reason, `"linear"`)
		require.Zero(t, agent.calls())
		st := mustState(t, newService(t, dir, agent, "auto"))
		item, err := st.Item(summary.Outcomes[0].Item)
		require.NoError(t, err)
		require.Empty(t, item.Worktree, "a run refused for a missing server keeps no worktree")
	})

	t.Run("a listing that does not finish in time is a missing requirement", func(t *testing.T) {
		restore := mcpListTimeout
		mcpListTimeout = 50 * time.Millisecond
		t.Cleanup(func() { mcpListTimeout = restore })
		dir := newRepo(t, map[string]string{"linear.yml": task})
		agent := resolvingAgent{fakeAgent: &fakeAgent{}, kind: harness.KindClaude, driver: &slowListingHarness{}}
		summary, err := newService(t, dir, agent, "auto").RunDue(ctx)
		require.NoError(t, err)
		require.Equal(t, OutcomeFailed, summary.Outcomes[0].Outcome)
		require.Contains(t, summary.Outcomes[0].Reason, "could not list claude MCP servers within 50ms")
		require.Zero(t, agent.calls())
	})

	t.Run("codex cannot enforce deny", func(t *testing.T) {
		dir := newRepo(t, map[string]string{"linear.yml": task})
		driver := &listingHarness{servers: []harness.MCPServerState{{Name: "linear", Usable: true}, {Name: "files", Usable: true}}}
		agent := resolvingAgent{fakeAgent: &fakeAgent{}, kind: harness.KindCodex, driver: driver}
		summary, err := newService(t, dir, agent, "auto").RunDue(ctx)
		require.NoError(t, err)
		require.Contains(t, summary.Outcomes[0].Reason, "codex cannot enforce deny; use Claude Code for this task or remove deny")
		require.Zero(t, agent.calls())
	})

	t.Run("met requirements run the prompt with the deny list", func(t *testing.T) {
		dir := newRepo(t, map[string]string{"linear.yml": task})
		driver := &listingHarness{servers: []harness.MCPServerState{{Name: "linear", Usable: true}, {Name: "files", Usable: true}}}
		var deny []string
		var sessionDir string
		agent := resolvingAgent{fakeAgent: &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
			deny, sessionDir = req.Deny, req.Dir
			return AgentResult{Message: "nothing to do"}, nil
		}}, kind: harness.KindClaude, driver: driver}
		summary, err := newService(t, dir, agent, "auto").RunDue(ctx)
		require.NoError(t, err)
		require.Equal(t, OutcomeNoChanges, summary.Outcomes[0].Outcome)
		require.Equal(t, 1, agent.calls())
		require.Equal(t, []string{"WebFetch"}, deny)
		// MCP configuration can be scoped to a directory, so the servers are
		// listed where the session runs: the run's worktree.
		require.Equal(t, []string{sessionDir}, driver.dirs)
	})
}

func TestTaskSessionsLoadTheUsersMCPAndRefuseDeniedTools(t *testing.T) {
	driver := &harnesstest.Harness{}
	agent := HarnessAgent{
		Drivers:  map[harness.Kind]harness.Harness{harness.KindClaude: driver},
		Settings: func() (agentchat.Settings, error) { return agentchat.Settings{Harness: harness.KindClaude}, nil },
	}
	_, err := agent.RunPrompt(context.Background(), AgentRequest{Dir: t.TempDir(), Prompt: "go", Deny: []string{"WebFetch"}})
	require.NoError(t, err)
	require.Len(t, driver.StartedWith, 1)
	require.True(t, driver.StartedWith[0].UserMCPConfig)
	require.Equal(t, []string{"WebFetch"}, driver.StartedWith[0].DisallowedTools)
}
