package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The test binary doubles as a handler: `<test binary> hook-helper <mode>`.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "hook-helper" {
		helper(os.Args[2], os.Args[3:])
		return
	}
	os.Exit(m.Run())
}

func helper(mode string, args []string) {
	input, _ := io.ReadAll(os.Stdin)
	if dir := os.Getenv("HOOK_TEST_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, strings.SplitN(mode, ":", 2)[0]+".input.json"), input, 0o644)
	}
	switch {
	case strings.HasPrefix(mode, "items:"):
		var items []Item
		for _, id := range strings.Split(strings.TrimPrefix(mode, "items:"), ",") {
			items = append(items, Item{ID: id, Text: "text " + id})
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"items": items})
	case mode == "big":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"items": []Item{{ID: "big", Text: strings.Repeat("x", 20000)}, {ID: "small", Text: "small"}}})
	case mode == "fail":
		fmt.Fprintln(os.Stderr, "handler broke")
		os.Exit(3)
	case mode == "garbage":
		fmt.Println("not json")
	case mode == "sleep":
		child := exec.Command(os.Args[0], "hook-helper", "grandchild", args[0])
		_ = child.Start()
		time.Sleep(30 * time.Second)
	case mode == "grandchild":
		_ = os.WriteFile(args[0], []byte(strconv.Itoa(os.Getpid())), 0o644)
		time.Sleep(30 * time.Second)
	}
}

func helperHandler(id string, mode string, extra ...string) Handler {
	return Handler{
		Starter: "test", ID: id, Delivers: "test context",
		Events:  []string{SessionStart, SubagentStart, PostToolUse},
		Tools:   []string{ToolRead, ToolEdit, ToolShell, ToolPatch},
		Command: append([]string{os.Args[0], "hook-helper", mode}, extra...),
		Timeout: 3 * time.Second,
	}
}

// repo returns a canonical scratch worktree with a file to read.
func repo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package a\n"), 0o644))
	t.Setenv("HOOK_TEST_DIR", root)
	return root
}

type result struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func dispatchOnce(t *testing.T, root, harness, point, payload string, handlers ...Handler) (result, string) {
	t.Helper()
	var out bytes.Buffer
	Run(context.Background(), Options{
		Harness: harness, Point: point, Stdin: strings.NewReader(payload), Stdout: &out, Cwd: root,
		Handlers: func(string) ([]Handler, error) { return handlers, nil },
	})
	var res result
	if out.Len() > 0 {
		require.NoError(t, json.Unmarshal(out.Bytes(), &res), out.String())
	}
	return res, out.String()
}

func readPayload(root string) string {
	return fmt.Sprintf(`{"session_id":"s1","cwd":%q,"tool_name":"Read","tool_input":{"file_path":"pkg/a.go"}}`, root)
}

func handlerInputOf(t *testing.T, root, mode string) handlerInput {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, mode+".input.json"))
	require.NoError(t, err)
	var in handlerInput
	require.NoError(t, json.Unmarshal(data, &in))
	return in
}

func TestDispatchEmitsInHostFormAndDedupesPerAgent(t *testing.T) {
	root := repo(t)
	h := helperHandler("one", "items:a,b")
	res, _ := dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), h)
	require.Equal(t, "PostToolUse", res.HookSpecificOutput.HookEventName)
	require.Equal(t, "text a\n\ntext b", res.HookSpecificOutput.AdditionalContext)

	in := handlerInputOf(t, root, "items")
	require.Equal(t, []string{"pkg/a.go"}, in.Event.Paths)
	require.Equal(t, ToolRead, in.Event.Tool)
	require.Equal(t, Identity{Worktree: root, Harness: HarnessClaude, Session: "s1"}, in.Event.Agent)
	require.Equal(t, contextBudget(HarnessClaude), in.Budget)
	require.Empty(t, in.Emitted)

	// The same agent gets nothing twice, and the handler is told what it has.
	_, out := dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), h)
	require.Empty(t, out)
	require.ElementsMatch(t, []string{"a", "b"}, handlerInputOf(t, root, "items").Emitted)

	// A subagent is its own context, and session start is its own scope.
	sub := fmt.Sprintf(`{"session_id":"s1","agent_id":"child","cwd":%q,"tool_name":"Read","tool_input":{"file_path":"pkg/a.go"}}`, root)
	res, _ = dispatchOnce(t, root, HarnessClaude, PostToolUse, sub, h)
	require.Equal(t, "text a\n\ntext b", res.HookSpecificOutput.AdditionalContext)
	res, _ = dispatchOnce(t, root, HarnessClaude, SessionStart, fmt.Sprintf(`{"session_id":"s1","cwd":%q,"source":"startup"}`, root), h)
	require.Equal(t, "SessionStart", res.HookSpecificOutput.HookEventName)
	require.NotEmpty(t, res.HookSpecificOutput.AdditionalContext)
}

func TestCompactionResetsTheAgentsContextInBothScopes(t *testing.T) {
	root := repo(t)
	h := helperHandler("one", "items:a")
	start := func(source string) string {
		return fmt.Sprintf(`{"session_id":"s1","cwd":%q,"source":%q}`, root, source)
	}
	_, out := dispatchOnce(t, root, HarnessClaude, SessionStart, start("startup"), h)
	require.NotEmpty(t, out)
	_, out = dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), h)
	require.NotEmpty(t, out)
	_, out = dispatchOnce(t, root, HarnessClaude, SessionStart, start("resume"), h)
	require.Empty(t, out, "a resume keeps the context")

	res, _ := dispatchOnce(t, root, HarnessClaude, SessionStart, start("compact"), h)
	require.Equal(t, "text a", res.HookSpecificOutput.AdditionalContext)
	require.Equal(t, "compact", handlerInputOf(t, root, "items").Event.Reset)
	_, out = dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), h)
	require.NotEmpty(t, out, "tool-call items are delivered again after a compaction")
}

func TestCodexToolCallsDedupePerTurn(t *testing.T) {
	root := repo(t)
	h := helperHandler("one", "items:a")
	patch := func(turn string) string {
		return fmt.Sprintf(`{"session_id":"s1","turn_id":%q,"cwd":%q,"tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Update File: pkg/a.go\n*** End Patch"}}`, turn, root)
	}
	_, out := dispatchOnce(t, root, HarnessCodex, PostToolUse, patch("t1"), h)
	require.NotEmpty(t, out)
	require.Equal(t, ToolPatch, handlerInputOf(t, root, "items").Event.Tool)
	_, out = dispatchOnce(t, root, HarnessCodex, PostToolUse, patch("t1"), h)
	require.Empty(t, out)
	_, out = dispatchOnce(t, root, HarnessCodex, PostToolUse, patch("t2"), h)
	require.NotEmpty(t, out)
}

func TestOneHandlerFailureKeepsTheOthersItemsAndLogs(t *testing.T) {
	root := repo(t)
	for _, broken := range []string{"fail", "garbage"} {
		res, _ := dispatchOnce(t, root, HarnessClaude, PostToolUse, fmt.Sprintf(`{"session_id":%q,"cwd":%q,"tool_name":"Read","tool_input":{"file_path":"pkg/a.go"}}`, broken, root),
			helperHandler("broken", broken), helperHandler("good", "items:a"))
		require.Equal(t, "text a", res.HookSpecificOutput.AdditionalContext, broken)
	}
	log, err := os.ReadFile(filepath.Join(root, ".rhizome", "hooks", "diagnostics.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "test/broken")
	require.Contains(t, string(log), "handler broke")
	require.Contains(t, string(log), "parse output")
}

func TestOutputBudgetDropsItemsBeyondTheAllowance(t *testing.T) {
	root := repo(t)
	res, _ := dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), helperHandler("big", "big"), helperHandler("after", "items:a"))
	require.Equal(t, "small\n\ntext a", res.HookSpecificOutput.AdditionalContext)
	require.Equal(t, contextBudget(HarnessClaude)-len("small"), handlerInputOf(t, root, "items").Budget)
}

func TestTheDeadlineBoundsTheWholeInvocation(t *testing.T) {
	root := repo(t)
	slow := helperHandler("slow", "sleep", filepath.Join(root, "pid"))
	var out bytes.Buffer
	started := time.Now()
	Run(context.Background(), Options{
		Harness: HarnessClaude, Point: PostToolUse, Stdin: strings.NewReader(readPayload(root)), Stdout: &out, Cwd: root,
		Deadline: time.Second, Handlers: func(string) ([]Handler, error) { return []Handler{slow}, nil },
	})
	require.Less(t, time.Since(started), 2*time.Second)
	require.Empty(t, out.String())
}

func TestNoApplicableHandlerLeavesStateUntouched(t *testing.T) {
	root := repo(t)
	readsOnly := helperHandler("reads", "items:a")
	readsOnly.Tools = []string{ToolRead}
	shell := fmt.Sprintf(`{"session_id":"s1","cwd":%q,"tool_name":"Bash","tool_input":{"command":"go test ./..."}}`, root)
	_, out := dispatchOnce(t, root, HarnessClaude, PostToolUse, shell, readsOnly)
	require.Empty(t, out)
	_, err := os.Stat(filepath.Join(root, ".rhizome", "hooks"))
	require.True(t, os.IsNotExist(err), "no state was read or written")

	// A read outside the worktree gives a handler nothing.
	outside := fmt.Sprintf(`{"session_id":"s1","cwd":%q,"tool_name":"Read","tool_input":{"file_path":"/etc/hosts"}}`, root)
	_, out = dispatchOnce(t, root, HarnessClaude, PostToolUse, outside, readsOnly)
	require.Empty(t, out)
}

func TestShellCallsWithoutAReadNeedAPublishedPrefix(t *testing.T) {
	root := repo(t)
	h := helperHandler("cmd", "items:a")
	h.CommandPrefixes = ".rhizome/test/prefixes.json"
	shell := func(cmd string) string {
		return fmt.Sprintf(`{"session_id":%q,"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}}`, cmd, root, cmd)
	}
	_, out := dispatchOnce(t, root, HarnessClaude, PostToolUse, shell("go mod vendor"), h)
	require.Empty(t, out, "a missing prefix file starts nothing")

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "test"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "test", "prefixes.json"), []byte(`["go mod vendor"]`), 0o644))
	_, out = dispatchOnce(t, root, HarnessClaude, PostToolUse, shell("go build ./..."), h)
	require.Empty(t, out)
	res, _ := dispatchOnce(t, root, HarnessClaude, PostToolUse, shell("cd sub && go mod vendor"), h)
	require.Equal(t, "text a", res.HookSpecificOutput.AdditionalContext)
	require.Equal(t, "cd sub && go mod vendor", handlerInputOf(t, root, "items").Event.Command)

	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "test", "prefixes.json"), []byte(`not json`), 0o644))
	_, out = dispatchOnce(t, root, HarnessClaude, PostToolUse, shell("go mod vendor -v"), h)
	require.Empty(t, out, "an unreadable prefix file starts nothing")
}

func TestDispatcherRecordsWhenEachHandlerReturnedContext(t *testing.T) {
	root := repo(t)
	at := time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)
	var out bytes.Buffer
	Run(context.Background(), Options{
		Harness: HarnessCodex, Point: PostToolUse, Stdin: strings.NewReader(readPayload(root)), Stdout: &out, Cwd: root,
		Now: func() time.Time { return at },
		Handlers: func(string) ([]Handler, error) {
			return []Handler{helperHandler("one", "items:a"), helperHandler("none", "items:")}, nil
		},
	})
	require.Equal(t, Status{HarnessCodex: {"test/one": at}}, ReadStatus(root))
}

func TestBadInputEndsQuietly(t *testing.T) {
	root := repo(t)
	h := helperHandler("one", "items:a")
	for _, payload := range []string{"not json", strings.Repeat(" ", maxPayloadBytes+1)} {
		_, out := dispatchOnce(t, root, HarnessClaude, PostToolUse, payload, h)
		require.Empty(t, out)
	}
	_, out := dispatchOnce(t, root, "cursor", PostToolUse, readPayload(root), h)
	require.Empty(t, out)
}

func TestStdinThatNeverClosesEndsAtTheDeadline(t *testing.T) {
	root := repo(t)
	stdin, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	started := time.Now()
	Run(context.Background(), Options{
		Harness: HarnessClaude, Point: PostToolUse, Stdin: stdin, Stdout: io.Discard, Cwd: root,
		Deadline: 200 * time.Millisecond, Handlers: func(string) ([]Handler, error) { return nil, nil },
	})
	require.Less(t, time.Since(started), time.Second)
	log, err := os.ReadFile(filepath.Join(root, ".rhizome", "hooks", "diagnostics.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "standard input stayed open")
}

func TestCorruptStateFilesAreTreatedAsEmpty(t *testing.T) {
	root := repo(t)
	h := helperHandler("one", "items:a")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "hooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "hooks", "status.json"), []byte("null"), 0o644))
	statePath := sessionStatePath(root, Identity{Worktree: root, Harness: HarnessClaude, Session: "s1"})
	require.NoError(t, os.MkdirAll(filepath.Dir(statePath), 0o755))
	require.NoError(t, os.WriteFile(statePath, []byte(`{"scopes":[1]}`), 0o644))
	res, _ := dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), h)
	require.Equal(t, "text a", res.HookSpecificOutput.AdditionalContext)
	require.Contains(t, ReadStatus(root)[HarnessClaude], "test/one")
	_, out := dispatchOnce(t, root, HarnessClaude, PostToolUse, readPayload(root), h)
	require.Empty(t, out, "the rewritten state records the item")
}

func TestAPanicEndsQuietlyWithADiagnostic(t *testing.T) {
	root := repo(t)
	require.NotPanics(t, func() {
		Run(context.Background(), Options{
			Harness: HarnessClaude, Point: PostToolUse, Stdin: strings.NewReader(readPayload(root)), Stdout: io.Discard, Cwd: root,
			Handlers: func(string) ([]Handler, error) { panic("handler registry broke") },
		})
	})
	log, err := os.ReadFile(filepath.Join(root, ".rhizome", "hooks", "diagnostics.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "panic: handler registry broke")
}
