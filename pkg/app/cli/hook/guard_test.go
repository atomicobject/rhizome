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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const findingsDir = "docs/memory/agent-findings"

var guardHandler = Handler{
	Starter: "test", ID: "guard", Delivers: "refusals", Events: []string{PreToolUse},
	Tools: []string{ToolRead, ToolEdit, ToolShell, ToolPatch}, GuardRules: findingsDir,
}

// guardRepo is a Git repository, since only tracked finding notes hold
// rules.
func guardRepo(t *testing.T) string {
	t.Helper()
	root := repo(t)
	require.NoError(t, os.Remove(filepath.Join(root, ".git")))
	gitRun(t, root, "init", "-q")
	return root
}

func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// writeFinding writes and tracks an approved guard finding with a rule and
// the approved hash shards.mjs would give it.
func writeFinding(t *testing.T, root, name, rule string) string {
	t.Helper()
	var r GuardRule
	hash := ""
	if json.Unmarshal([]byte(rule), &r) == nil {
		hash = GuardHash("guard", r)
	}
	return writeNote(t, root, name, "---\nresponse: guard\nsummary: \"x\"\nguard-rule: "+rule+"\napproved-hash: \""+hash+"\"\nreviewed-by: \"Sam\"\nreviewed-on: 2026-10-22\n---\n\n# x\n", true)
}

// writeNote writes a note in the findings folder, tracked or not.
func writeNote(t *testing.T, root, name, text string, track bool) string {
	t.Helper()
	rel := findingsDir + "/" + name
	require.NoError(t, os.MkdirAll(filepath.Join(root, findingsDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(text), 0o644))
	if track {
		gitRun(t, root, "add", "--", rel)
	}
	return rel
}

type decision struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func preToolUse(t *testing.T, root, harness, toolName string, input any, handlers ...Handler) (decision, string) {
	t.Helper()
	in, err := json.Marshal(input)
	require.NoError(t, err)
	payload := fmt.Sprintf(`{"session_id":"s1","cwd":%q,"hook_event_name":"PreToolUse","tool_name":%q,"tool_input":%s}`, root, toolName, in)
	var out bytes.Buffer
	Run(context.Background(), Options{
		Harness: harness, Point: PreToolUse, Stdin: strings.NewReader(payload), Stdout: &out, Cwd: root,
		Handlers: func(string) ([]Handler, error) { return handlers, nil },
	})
	var d decision
	if out.Len() > 0 {
		require.NoError(t, json.Unmarshal(out.Bytes(), &d), out.String())
	}
	return d, out.String()
}

func diagnostics(t *testing.T, root string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(root, ".rhizome", "hooks", "diagnostics.log"))
	return string(data)
}

func TestCommandRulesMatchCommandWordsOfEachSimpleCommand(t *testing.T) {
	rule := strings.Fields("go mod vendor")
	for _, tc := range []struct {
		cmd             string
		matched, unsure bool
	}{
		{"go mod vendor", true, false},
		{"cd sub && go mod vendor -v", true, false},
		{"cd ./x && go mod vendor", true, false},
		{"make a && go mod vendor -v", true, false},
		{"git status; go   mod  vendor", true, false},
		{"make lint || go mod vendor", true, false},
		{"ls\ngo mod vendor", true, false},
		{"FOO=1 go mod vendor", true, false},
		{"go mod >vendor.log vendor", true, false},
		{"go mod 2>&1 vendor -v", true, false},
		{`FOO="a b" BAR+=1 go mod vendor`, true, false},
		{"/usr/local/go/bin/go mod vendor", true, false},
		{"go mod 'vendor'", true, false},
		{"go mod vendor &> log.txt", true, false},
		{"go mod vendor 2>&1 | tee log.txt", true, false},
		{`echo "go mod vendor"`, false, false},
		{"echo hi # ; go mod vendor", false, false},
		{"# go mod vendor\ngo mod tidy", false, false},
		{"echo a#b; go mod vendor", true, false},
		{"git commit -m 'go mod vendor'", false, false},
		{"go mod vendorize", false, false},
		{"go mod tidy", false, false},
		{"go mod $X", false, false},
		// Not only plain simple commands: the call proceeds and the
		// dispatcher logs.
		{"cat <<EOF\ngo mod vendor\nEOF", false, true},
		{"(cd sub && go mod vendor)", false, true},
		{"eval 'go mod vendor'", false, true},
		{"bash -c 'go mod vendor'", false, true},
		{"bash -c 'go mod tidy'", false, false},
		{"bash -lc 'go mod vendor'", false, true},
		{"echo 'go mod vendor", false, true},
		{"$GO go mod vendor", false, true},
		{"cat <<EOF\nhello\nEOF", false, false},
		// Anything not plain anywhere leaves the whole command unevaluated,
		// even the part before it.
		{"go mod vendor; cat <<EOF\nx\nEOF", false, true},
		{"go mod vendor; (true)", false, true},
		// Each of these may change what go means, or whether it runs: here
		// go is a function that prints harmless.
		{"builtin eval 'go() { printf harmless; }'; go mod vendor", false, true},
		{"time FOO=1 builtin eval 'go() { :; }'; go mod vendor", false, true},
		{"if true; then builtin eval 'go() { :; }'; fi; go mod vendor", false, true},
		{"{ eval 'go() { :; }'; }; go mod vendor", false, true},
		{"! eval 'go() { :; }'; go mod vendor", false, true},
		{"noglob eval 'go() { :; }'; go mod vendor", false, true},
		{"eval 'go() { :; }'; go mod vendor", false, true},
		{"command -p eval x; go mod vendor", false, true},
		{"go() { :; }; go mod vendor", false, true},
		{"function go { :; }; go mod vendor", false, true},
		{"alias go=true\ngo mod vendor", false, true},
		{"hash -p /bin/true go; go mod vendor", false, true},
		{". ./env.sh && go mod vendor", false, true},
		{"source ./env.sh && go mod vendor", false, true},
		{"exec true; go mod vendor", false, true},
		{"$X; go mod vendor", false, true},
		{"g$X mod vendor; go mod vendor", false, true},
		{"echo `true`; go mod vendor", false, true},
		// A quoted word is a command, not an assignment, and a quoted
		// command word may be a reserved word.
		{`"FOO=1" go mod vendor`, false, true},
		{`FOO\=1 go mod vendor`, false, true},
		{`"go" mod vendor`, false, false},
		{"FOO=$X go mod vendor", false, true},
		{"FOO=1; go mod vendor", false, true},
		// Deliberately lost: wrapped or after a builtin, the command is not
		// evaluated.
		{"command -v go && go mod vendor", false, true},
		{"env GOFLAGS=-mod=mod go mod vendor", false, true},
		{"sudo go mod vendor", false, true},
	} {
		matched, unsure := commandRuleMatches(tc.cmd, rule)
		require.Equal(t, [2]bool{tc.matched, tc.unsure}, [2]bool{matched, unsure}, tc.cmd)
	}
	// A rule's wrappers and command-name case do not keep it from matching.
	for _, rule := range []string{"sudo reboot", "env reboot", "REBOOT", "FOO=1 reboot"} {
		refused, _ := GuardRule{Tool: ToolShell, Command: rule}.refuses(Event{Tool: ToolShell, Command: "cd / && reboot now"})
		require.True(t, refused, rule)
	}
}

func TestAMatchingCallIsRefusedBeforeItRunsInBothHosts(t *testing.T) {
	root := guardRepo(t)
	rel := writeFinding(t, root, "guard-vendor.md", `{"tool":"shell","command":"go mod vendor","reason":"It deletes hand-vendored C files.","alternative":"Run make vendor."}`)
	for _, harness := range []string{HarnessClaude, HarnessCodex} {
		d, _ := preToolUse(t, root, harness, "Bash", map[string]string{"command": "cd pkg && go mod vendor"}, guardHandler)
		require.Equal(t, "PreToolUse", d.HookSpecificOutput.HookEventName)
		require.Equal(t, "deny", d.HookSpecificOutput.PermissionDecision)
		require.Equal(t, "Refused by the guard rule in "+rel+". Reason: It deletes hand-vendored C files. Instead: Run make vendor.", d.HookSpecificOutput.PermissionDecisionReason)

		// Other calls proceed unchanged.
		_, out := preToolUse(t, root, harness, "Bash", map[string]string{"command": "go mod tidy"}, guardHandler)
		require.Empty(t, out)
	}
	// Every matching call is refused, however often it repeats, and counted
	// per host.
	_, out := preToolUse(t, root, HarnessClaude, "Bash", map[string]string{"command": "go mod vendor"}, guardHandler)
	require.Contains(t, out, `"deny"`)
	require.Equal(t, map[string]map[string]int{HarnessClaude: {rel: 2}, HarnessCodex: {rel: 1}}, ReadStatus(root).Refused)
}

func TestPathRulesMatchContainedPathsTheCallReadsOrEdits(t *testing.T) {
	root := guardRepo(t)
	writeFinding(t, root, "guard-vendor-edit.md", `{"tool":"edit","path":"vendor/**","reason":"Vendored code is generated.","alternative":"Change go.mod and run make vendor."}`)
	writeFinding(t, root, "guard-secrets-read.md", `{"tool":"read","path":"**/*.pem","reason":"Keys stay out of context.","alternative":"Ask the person."}`)
	refused := func(harness, tool string, input any) bool {
		_, out := preToolUse(t, root, harness, tool, input, guardHandler)
		return strings.Contains(out, `"deny"`)
	}
	// A file the call is about to create in a new folder is still matched.
	require.True(t, refused(HarnessClaude, "Write", map[string]string{"file_path": filepath.Join(root, "vendor", "new", "x.go")}))
	require.True(t, refused(HarnessClaude, "Edit", map[string]string{"file_path": "pkg/../vendor/a.go"}))
	require.False(t, refused(HarnessClaude, "Edit", map[string]string{"file_path": "pkg/a.go"}))
	// An edit rule covers Codex's patch tool.
	require.True(t, refused(HarnessCodex, "apply_patch", map[string]string{"command": "*** Begin Patch\n*** Add File: vendor/b/c.go\n+package c\n*** End Patch"}))
	// A path rule names its tool class: reading vendored code is fine.
	require.False(t, refused(HarnessClaude, "Read", map[string]string{"file_path": "vendor/a.go"}))
	require.True(t, refused(HarnessClaude, "Read", map[string]string{"file_path": "certs/dev.pem"}))
	require.False(t, refused(HarnessCodex, "Bash", map[string]string{"command": "cat certs/dev.pem"}), "a shell read is not a read-tool call")
	// A patch Codex sends to another environment edits no file here.
	require.False(t, refused(HarnessCodex, "apply_patch", map[string]string{"command": "*** Begin Patch\n*** Environment ID: remote\n*** Add File: vendor/b/c.go\n+package c\n*** End Patch"}))
	// A path outside the worktree is never matched.
	require.False(t, refused(HarnessClaude, "Read", map[string]string{"file_path": "/etc/dev.pem"}))
}

// A shell path rule refuses only a read whose directory is certain.
func TestShellPathRulesRefuseOnlyWhereTheDirectoryIsCertain(t *testing.T) {
	root := guardRepo(t)
	rel := writeFinding(t, root, "guard-doc-shell.md", `{"tool":"shell","path":"docs/x.md","reason":"r","alternative":"a"}`)
	writeFinding(t, root, "guard-sub-shell.md", `{"tool":"shell","path":"sub/docs/x.md","reason":"r","alternative":"a"}`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub", "docs"), 0o755))
	refused := func(harness, cmd string) bool {
		_, out := preToolUse(t, root, harness, "Bash", map[string]string{"command": cmd}, guardHandler)
		return strings.Contains(out, `"deny"`)
	}
	// An unquoted backslash is an escape in bash, so commands name a Windows
	// directory with forward slashes, as Git Bash does.
	slashRoot := filepath.ToSlash(root)
	for _, cmd := range []string{
		"cat docs/x.md",
		"cd ./pkg/.. && cat docs/x.md",
		"cd ./sub && cat docs/x.md",
		"cd -- ./sub && cat docs/x.md",
		"cd -L ./sub && cat docs/x.md",
		"FOO=1 cat docs/x.md",
		"git status && cd ../" + filepath.Base(root) + "/sub && cat docs/x.md",
		"cd . && cat docs/x.md",
		"cd sub | true; cd " + slashRoot + " && cat docs/x.md",
		"cat " + slashRoot + "/docs/x.md",
		"cd " + slashRoot + " && cat docs/x.md",
	} {
		require.True(t, refused(HarnessClaude, cmd), cmd)
	}
	for _, cmd := range []string{
		// The reader may run somewhere the parse cannot follow.
		`cd "$OTHER" && cat docs/x.md`,
		"cd $(mktemp -d) && cat docs/x.md",
		"pushd /tmp && cat docs/x.md",
		"cd && cat docs/x.md",
		"cd - && cat docs/x.md",
		"eval 'cd /tmp'; cat docs/x.md",
		"cat docs/x.md; eval true",
		"command cd /tmp && cat docs/x.md",
		"builtin cd /tmp && cat docs/x.md",
		"if true; then cd /tmp; fi; cat docs/x.md",
		"source env.sh && cat docs/x.md",
		// A wrapped source, ., eval, or cd still runs in this shell.
		"command source ./move.sh && cat docs/x.md",
		"builtin source ./move.sh && cat docs/x.md",
		"command . ./move.sh && cat docs/x.md",
		"builtin eval 'cd /tmp'; cat docs/x.md",
		"command -p cd /tmp && cat docs/x.md",
		"$CD /tmp && cat docs/x.md",
		"cd /tmp | true; cat docs/x.md",
		"cat docs/x.md\ncat <<EOF\nhi\nEOF",
		"(cd /tmp); cat docs/x.md",
		// cat may be a function that reads nothing.
		"function cat { :; }; cat docs/x.md",
		"alias cat=true\ncat docs/x.md",
		"cat() { :; }; cat docs/x.md",
		"hash -p /bin/true cat; cat docs/x.md",
		"command -p cat docs/x.md",
		"time FOO=1 builtin eval 'cd /tmp'; cat docs/x.md",
		"if true; then builtin eval 'cd /tmp'; fi; cat docs/x.md",
		"noglob eval 'cd /tmp'; cat docs/x.md",
		"cd -P ./sub && cat docs/x.md",
	} {
		require.False(t, refused(HarnessClaude, cmd), cmd)
	}
	// Whether these read sub/docs/x.md depends on whether the cd ran in this
	// shell and succeeded: a cd in a pipeline or in the background leaves
	// the reader's directory alone, and a command after ; or || also runs
	// when the cd failed.
	for _, cmd := range []string{
		"cd sub | cat docs/x.md",
		"cd sub & cat docs/x.md",
		"cd sub; cat docs/x.md",
		"cd sub && true || cat docs/x.md",
		"true | cd sub && cat docs/x.md",
		"false || cd sub && cat docs/x.md",
		// CDPATH, which the hook cannot see, may send a cd to a bare
		// relative name anywhere.
		"cd sub && cat docs/x.md",
		"CDPATH=/elsewhere cd sub && cat docs/x.md",
		"cd pkg/../sub && cat docs/x.md",
	} {
		require.False(t, refused(HarnessClaude, cmd), cmd)
	}
	// ./cd is a program in a child process, not the builtin, so the read
	// stays in the starting directory.
	_, out := preToolUse(t, root, HarnessClaude, "Bash", map[string]string{"command": "./cd sub && cat docs/x.md"}, guardHandler)
	require.Contains(t, out, "guard-doc-shell.md")
	require.Contains(t, diagnostics(t, root), "guard rule "+rel+" not applied")

	// Codex runs a shell command in a workdir its hook payload omits, so only
	// a read under an absolute directory is certain there.
	require.False(t, refused(HarnessCodex, "cat docs/x.md"))
	require.False(t, refused(HarnessCodex, "cd ./sub && cat docs/x.md"))
	require.True(t, refused(HarnessCodex, "cd "+slashRoot+" && cat docs/x.md"))
	require.True(t, refused(HarnessCodex, "cd "+slashRoot+"/sub && cat docs/x.md"))
	require.True(t, refused(HarnessCodex, "cat "+slashRoot+"/docs/x.md"))
}

func TestOnlyApprovedGuardFindingsCarryRules(t *testing.T) {
	root := guardRepo(t)
	rule := `{"tool":"shell","command":"make omega","reason":"r","alternative":"a"}`
	writeFinding(t, root, "guard-ok.md", rule)
	require.NoError(t, os.WriteFile(filepath.Join(root, findingsDir, "fix-x.md"), []byte("---\nresponse: fix\nguard-rule: "+rule+"\nreviewed-by: Sam\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, findingsDir, "guard-unreviewed.md"), []byte("---\nresponse: guard\nguard-rule: "+rule+"\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, findingsDir, "guard-plain.md"), []byte("---\nresponse: guard\nreviewed-by: Sam\n---\n"), 0o644))
	for name, rule := range map[string]string{
		"guard-abs.md":     `{"tool":"read","path":"/docs/x.md","reason":"r","alternative":"a"}`,
		"guard-dot.md":     `{"tool":"read","path":"./docs/x.md","reason":"r","alternative":"a"}`,
		"guard-quote.md":   `{"tool":"shell","command":"git commit -m \"wip\"","reason":"r","alternative":"a"}`,
		"guard-expand.md":  `{"tool":"shell","command":"$GO mod vendor","reason":"r","alternative":"a"}`,
		"guard-glob.md":    `{"tool":"shell","command":"sudo *","reason":"r","alternative":"a"}`,
		"guard-readcmd.md": `{"tool":"read","command":"cat docs/x.md","reason":"r","alternative":"a"}`,
		"guard-long.md":    `{"tool":"shell","command":"make omega","reason":"` + strings.Repeat("é", 301) + `","alternative":"a"}`,
	} {
		writeFinding(t, root, name, rule)
	}
	rules, problems := LoadGuardRules(context.Background(), root, findingsDir)
	require.Len(t, rules, 1)
	require.Equal(t, findingsDir+"/guard-ok.md", rules[0].Source)
	require.Equal(t, []string{
		findingsDir + "/fix-x.md: only a guard finding may carry guard-rule",
		findingsDir + `/guard-abs.md: guard-rule path "/docs/x.md" is not a repository-relative glob`,
		findingsDir + `/guard-dot.md: guard-rule path "./docs/x.md" is not a repository-relative glob`,
		findingsDir + `/guard-expand.md: guard-rule command "$GO mod vendor" must be plain words, without quotes or expansions`,
		findingsDir + `/guard-glob.md: guard-rule command "sudo *" must name a literal command word`,
		findingsDir + "/guard-long.md: guard-rule reason and alternative must each be at most 300 characters",
		findingsDir + `/guard-quote.md: guard-rule command "git commit -m \"wip\"" must be plain words, without quotes or expansions`,
		findingsDir + `/guard-readcmd.md: guard-rule command "cat docs/x.md" needs tool shell; a read rule needs a path`,
		findingsDir + "/guard-unreviewed.md: no reviewed-by: a person has not approved it",
	}, problems)
	rules, problems = LoadGuardRules(context.Background(), root, "docs/none")
	require.Empty(t, rules)
	require.Empty(t, problems)
}

// Only a tracked finding note, under the name shards.mjs gives it, whose rule
// still matches its approved hash, holds a rule.
func TestOnlyTrackedUnchangedFindingNotesHoldRules(t *testing.T) {
	root := guardRepo(t)
	rel := writeFinding(t, root, "guard-ok.md", `{"tool":"shell","command":"make omega","reason":"r","alternative":"a"}`)
	text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoError(t, err)
	retired := strings.Replace(string(text), "make omega", "make retired", 1)
	retired = strings.Replace(retired, GuardHash("guard", GuardRule{Tool: ToolShell, Command: "make omega", Reason: "r", Alternative: "a"}),
		GuardHash("guard", GuardRule{Tool: ToolShell, Command: "make retired", Reason: "r", Alternative: "a"}), 1)
	// A backup keeps a retired rule, tracked or not; neither applies.
	writeNote(t, root, "guard-ok.md.bak", retired, true)
	writeNote(t, root, "guard-ok.md~", retired, false)
	writeNote(t, root, "Guard-Copy.md", retired, true)
	// An approved note applies once it is committed, not before.
	writeNote(t, root, "guard-retired.md", retired, false)
	rules, problems := LoadGuardRules(context.Background(), root, findingsDir)
	require.Len(t, rules, 1)
	require.Equal(t, rel, rules[0].Source)
	require.Equal(t, []string{findingsDir + "/guard-retired.md: not tracked in Git; stage or commit it to activate its rule"}, problems)
	_, out := preToolUse(t, root, HarnessClaude, "Bash", map[string]string{"command": "make retired"}, guardHandler)
	require.Empty(t, out)

	// A rule edited after approval goes inert until it is revised.
	writeNote(t, root, "guard-ok.md", strings.Replace(string(text), `"reason":"r"`, `"reason":"edited"`, 1), true)
	rules, problems = LoadGuardRules(context.Background(), root, findingsDir)
	require.Empty(t, rules)
	require.Contains(t, problems, rel+": edited after approval; revise it through the compounding review")
	_, out = preToolUse(t, root, HarnessClaude, "Bash", map[string]string{"command": "make omega"}, guardHandler)
	require.Empty(t, out)
}

// A rule loads only as JSON.stringify writes it, escapes included; the
// compounding scripts' table (guard-frontmatter.json) covers other forms.
func TestGuardRulesLoadOnlyAsTheScriptsWriteThem(t *testing.T) {
	root := guardRepo(t)
	written := `{"tool":"shell","command":"make omega","reason":"Say \"hi\" \\ <b>&\t\u0001\u001f é — 😀","alternative":"a\nb"}`
	writeFinding(t, root, "guard-escapes.md", written)
	writeFinding(t, root, "guard-escaped-letter.md", `{"tool":"shell","command":"make omega","reason":"\u0072","alternative":"a"}`)
	rules, problems := LoadGuardRules(context.Background(), root, findingsDir)
	require.Len(t, rules, 1)
	require.Equal(t, "Say \"hi\" \\ <b>&\t\x01\x1f é — 😀", rules[0].Reason)
	require.Equal(t, []string{findingsDir + "/guard-escaped-letter.md: guard-rule is not the JSON the compounding scripts write; revise it through the compounding review"}, problems)
}

// shards.mjs computes the same hash; its test pins the same value.
func TestGuardHashIsTheOneShardsMjsWrites(t *testing.T) {
	r := GuardRule{Tool: ToolShell, Command: "go mod vendor", Reason: "It deletes hand-vendored C files.", Alternative: "make vendor"}
	require.Equal(t, "b9a4a97b3a352156", GuardHash("guard", r))
}

func TestAGuardPastItsDeadlineLetsTheCallProceed(t *testing.T) {
	root := guardRepo(t)
	writeFinding(t, root, "guard-vendor.md", `{"tool":"shell","command":"go mod vendor","reason":"r","alternative":"a"}`)
	payload := fmt.Sprintf(`{"session_id":"s1","cwd":%q,"tool_name":"Bash","tool_input":{"command":"go mod vendor"}}`, root)
	var out bytes.Buffer
	// The deadline passes after the payload is parsed, while handlers load.
	Run(context.Background(), Options{
		Harness: HarnessClaude, Point: PreToolUse, Stdin: strings.NewReader(payload), Stdout: &out, Cwd: root, Deadline: 100 * time.Millisecond,
		Handlers: func(string) ([]Handler, error) {
			time.Sleep(200 * time.Millisecond)
			return []Handler{guardHandler}, nil
		},
	})
	require.Empty(t, out.String())
	require.Contains(t, diagnostics(t, root), "guard stopped at the deadline, so the call proceeds")
}

func TestAGuardNeverBlocksOnItsOwnFailure(t *testing.T) {
	root := guardRepo(t)
	writeFinding(t, root, "guard-broken.md", `{"tool":"shell","command":"go mod vendor","reason":"r"`)
	writeFinding(t, root, "guard-no-alternative.md", `{"tool":"shell","command":"go mod vendor","reason":"r","alternative":""}`)
	bash := map[string]string{"command": "go mod vendor"}

	// Unreadable rules are skipped, and the call proceeds with a diagnostic.
	_, out := preToolUse(t, root, HarnessClaude, "Bash", bash, guardHandler)
	require.Empty(t, out)
	log := diagnostics(t, root)
	require.Contains(t, log, "guard rule skipped: "+findingsDir+"/guard-broken.md: unreadable frontmatter")
	require.Contains(t, log, "guard rule skipped: "+findingsDir+"/guard-no-alternative.md: guard-rule needs a reason and an alternative")

	// A command it cannot parse confidently proceeds, with a diagnostic.
	writeFinding(t, root, "guard-ok.md", `{"tool":"shell","command":"go mod vendor","reason":"r","alternative":"a"}`)
	_, out = preToolUse(t, root, HarnessClaude, "Bash", map[string]string{"command": "cat <<EOF\ngo mod vendor\nEOF"}, guardHandler)
	require.Empty(t, out)
	require.Contains(t, diagnostics(t, root), "guard rule "+findingsDir+"/guard-ok.md not applied: the command is not built only of plain simple commands")

	// A payload that is not JSON, or a host that never closes standard
	// input before the deadline, lets the call proceed.
	var buf bytes.Buffer
	Run(context.Background(), Options{Harness: HarnessClaude, Point: PreToolUse, Stdin: strings.NewReader("{"), Stdout: &buf, Cwd: root,
		Handlers: func(string) ([]Handler, error) { return []Handler{guardHandler}, nil }})
	require.Empty(t, buf.String())
	stdin, _ := io.Pipe()
	Run(context.Background(), Options{Harness: HarnessClaude, Point: PreToolUse, Stdin: stdin, Stdout: &buf, Cwd: root, Deadline: 50 * time.Millisecond,
		Handlers: func(string) ([]Handler, error) { return []Handler{guardHandler}, nil }})
	require.Empty(t, buf.String())
	require.Contains(t, diagnostics(t, root), "standard input stayed open past the deadline")

	// Without a guard declaration nothing is refused.
	_, out = preToolUse(t, root, HarnessClaude, "Bash", bash, helperHandler("one", "items:a"))
	require.Empty(t, out)
}

// The compounding scripts check a rule's path against the same table
// before approval, so the dispatcher matches only the dialect approval's
// breadth check counted.
func TestGuardPathRulesUseTheShardPatternDialect(t *testing.T) {
	raw, err := os.ReadFile("../init/testdata/compounding/guard-paths.json")
	require.NoError(t, err)
	var cases [][2]any
	require.NoError(t, json.Unmarshal(raw, &cases))
	for i, c := range cases {
		root := guardRepo(t)
		path, ok := c[0].(string), c[1].(bool)
		quoted, _ := json.Marshal(path)
		rel := writeFinding(t, root, fmt.Sprintf("guard-case-%d.md", i), `{"tool":"read","path":`+string(quoted)+`,"reason":"r","alternative":"a"}`)
		rules, problems := LoadGuardRules(context.Background(), root, findingsDir)
		require.Equal(t, ok, len(rules) == 1, path)
		if !ok {
			require.Equal(t, []string{fmt.Sprintf("%s: guard-rule path %q may use only literal names, *, **, and ?", rel, path)}, problems)
		}
	}
}

// The compounding scripts check a rule's command against the same table
// before approval, so a command approval accepts is one the dispatcher loads.
func TestGuardCommandRulesLoadAsTheScriptsValidateThem(t *testing.T) {
	raw, err := os.ReadFile("../init/testdata/compounding/guard-commands.json")
	require.NoError(t, err)
	var cases [][2]any
	require.NoError(t, json.Unmarshal(raw, &cases))
	root := guardRepo(t)
	for i, c := range cases {
		command, ok := c[0].(string), c[1].(bool)
		quoted, _ := json.Marshal(command)
		writeFinding(t, root, fmt.Sprintf("guard-case-%d.md", i), `{"tool":"shell","command":`+string(quoted)+`,"reason":"r","alternative":"a"}`)
		rules, _ := LoadGuardRules(context.Background(), root, findingsDir)
		require.Equal(t, ok, slices.ContainsFunc(rules, func(r GuardRule) bool { return r.Source == fmt.Sprintf("%s/guard-case-%d.md", findingsDir, i) }), command)
	}
}

// The compounding scripts accept only frontmatter in the form they write;
// the same table pins that the dispatcher loads every note they accept, and
// rejects the malformed ones check reports. A case with its own guard-rule
// replaces the canonical one.
func TestGuardNotesTheScriptsAcceptLoad(t *testing.T) {
	raw, err := os.ReadFile("../init/testdata/compounding/guard-frontmatter.json")
	require.NoError(t, err)
	var cases [][3]any
	require.NoError(t, json.Unmarshal(raw, &cases))
	rule := GuardRule{Tool: ToolShell, Command: "go mod vendor", Reason: "r", Alternative: "a"}
	canonical := `guard-rule: {"tool":"shell","command":"go mod vendor","reason":"r","alternative":"a"}` + "\n"
	root := guardRepo(t)
	for i, c := range cases {
		lines, loads := c[0].(string), c[2].(bool)
		name := fmt.Sprintf("guard-case-%d.md", i)
		front := canonical
		if strings.Contains(lines, "guard-rule:") {
			front = ""
		}
		front += "approved-hash: \"" + GuardHash("guard", rule) + "\"\nreviewed-by: \"Sam\""
		writeNote(t, root, name, "---\nresponse: guard\n"+lines+"\n"+front+"\n---\n\n# x\n", true)
		rules, _ := LoadGuardRules(context.Background(), root, findingsDir)
		require.Equal(t, loads, slices.ContainsFunc(rules, func(r GuardRule) bool { return r.Source == findingsDir+"/"+name }), lines)
	}
}
