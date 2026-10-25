package tasks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// isolateGit keeps the person's Git configuration (hooks, signing) out of
// tests.
func isolateGit(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Tester")
	t.Setenv("GIT_AUTHOR_EMAIL", "tester@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Tester")
	t.Setenv("GIT_COMMITTER_EMAIL", "tester@example.com")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitOutput(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo creates a repository on main with the given task files committed.
func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	run(t, dir, "init", "--quiet", "-b", "main")
	writeFile(t, filepath.Join(dir, "README.md"), "hello\n")
	for name, content := range files {
		writeFile(t, filepath.Join(dir, TasksDir, name), content)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "-m", "initial")
	return dir
}

func commitFile(t *testing.T, dir, path, content, message string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, path), content)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "--quiet", "-m", message)
}

type fakeAgent struct {
	mu      sync.Mutex
	prompts []string
	run     func(req AgentRequest) (AgentResult, error)
}

func (f *fakeAgent) RunPrompt(ctx context.Context, req AgentRequest) (AgentResult, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, req.Prompt)
	f.mu.Unlock()
	if f.run == nil {
		return AgentResult{Message: "nothing to do"}, nil
	}
	return f.run(req)
}

func (f *fakeAgent) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.prompts)
}

func newService(t *testing.T, dir string, agent AgentRunner, mode string) *Service {
	t.Helper()
	repo, err := OpenRepo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Options{
		Repo:   repo,
		Config: repoconfig.LocalTasksConfig{PrimaryRef: "main", DefaultMode: mode},
		Agent:  agent,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func mustState(t *testing.T, svc *Service) State {
	t.Helper()
	st, err := svc.Store().Load()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func taskItems(st State, task string) []Item {
	var out []Item
	for _, it := range st.Items {
		if it.Task == task && it.Kind == KindTask {
			out = append(out, it)
		}
	}
	return out
}

func hasLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l == line {
			return true
		}
	}
	return false
}

func shellAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("needs sh")
	}
}
