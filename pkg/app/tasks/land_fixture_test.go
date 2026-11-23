package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

const inPlaceTask = "name: Tend notes\nevery: 1h\nwrites: [\"notes/**\"]\nprompt: Tend the notes.\n"

// landFixture is a bare remote, the host clone tasks land in, and another
// clone that pushes as a teammate or another machine would.
type landFixture struct {
	t                   *testing.T
	remote, host, other string
	now                 time.Time
	agent               *fakeAgent
	svc                 *Service
	// env is the user configuration services get; zero means none.
	env vaultconfig.Env
}

func newLandFixture(t *testing.T, task string) *landFixture {
	t.Helper()
	shellAvailable(t)
	isolateGit(t)
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	f := &landFixture{t: t, remote: filepath.Join(root, "remote.git"), host: filepath.Join(root, "host"), other: filepath.Join(root, "other"),
		now: time.Date(2026, 11, 23, 5, 0, 0, 0, time.UTC), agent: &fakeAgent{}}
	run(t, root, "init", "--quiet", "--bare", "-b", "main", f.remote)
	seed := filepath.Join(root, "seed")
	run(t, root, "clone", "--quiet", f.remote, seed)
	for path, content := range map[string]string{
		"README.md":                   "hello\n",
		".gitignore":                  ".rhizome/task-state/\n",
		".agents/skills/a/SKILL.md":   "agent skill\n",
		".rhizome/tasks/tend.yml":     task,
		"notes/log.md":                "log\n",
		"notes/dirty.md":              "dirty\n",
		"notes/staged.md":             "staged\n",
		"other/readme.md":             "other\n",
		".rhizome/memory/handoff.md":  "handoff\n",
		".agents/skills/a/notes/x.md": "x\n",
	} {
		writeFile(t, filepath.Join(seed, path), content)
	}
	run(t, seed, "add", "-A")
	run(t, seed, "commit", "--quiet", "-m", "initial")
	run(t, seed, "push", "--quiet", "origin", "HEAD:main")
	run(t, root, "clone", "--quiet", f.remote, f.host)
	run(t, root, "clone", "--quiet", f.remote, f.other)
	f.svc = f.service(repoconfig.LocalTasksConfig{PrimaryRef: "origin/main", DefaultMode: "auto", AllowPush: true}, true)
	return f
}

func (f *landFixture) service(cfg repoconfig.LocalTasksConfig, push bool) *Service {
	f.t.Helper()
	repo, err := OpenRepo(context.Background(), f.host)
	if err != nil {
		f.t.Fatal(err)
	}
	svc, err := New(Options{Repo: repo, Config: cfg, Agent: f.agent, Push: push, Now: func() time.Time { return f.now }, Env: f.env})
	if err != nil {
		f.t.Fatal(err)
	}
	return svc
}

// pass advances the clock past the task's cadence and runs one pass.
func (f *landFixture) pass() Outcome {
	f.t.Helper()
	f.now = f.now.Add(2 * time.Hour)
	summary, err := f.svc.RunDue(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	for _, o := range summary.Outcomes {
		if o.Task == "tend" {
			return o
		}
	}
	f.t.Fatalf("the task did not run: %+v", summary)
	return Outcome{}
}

// push commits a file in the other clone and pushes it to the remote.
func (f *landFixture) push(path, content string) {
	f.t.Helper()
	run(f.t, f.other, "pull", "--quiet", "--rebase", "origin", "main")
	commitFile(f.t, f.other, path, content, "teammate: "+path)
	run(f.t, f.other, "push", "--quiet", "origin", "HEAD:main")
}

// edits makes the agent write files, relative to its checkout.
func (f *landFixture) edits(files map[string]string, err error) {
	f.agent.run = func(req AgentRequest) (AgentResult, error) {
		for path, content := range files {
			writeFile(f.t, filepath.Join(req.Dir, path), content)
		}
		return AgentResult{Message: "Tended the notes."}, err
	}
}

func (f *landFixture) read(path string) string {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.host, path))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

// status is the host's short status, one sorted line per path.
func (f *landFixture) status() string {
	return run(f.t, f.host, "status", "--porcelain", "--untracked-files=all")
}

func (f *landFixture) committed(rev string) []string {
	return strings.Fields(run(f.t, f.host, "diff-tree", "--no-commit-id", "--name-only", "-r", rev))
}

func (f *landFixture) attention() []Item {
	var out []Item
	for _, it := range taskItems(mustState(f.t, f.svc), "tend") {
		if it.State == StateAttention {
			out = append(out, it)
		}
	}
	return out
}
