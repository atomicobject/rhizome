package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// testSource admits only files inside a checkout of this repository.
func (s *Service) testSource(ctx context.Context, file string) (string, string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	} else if !errors.Is(resolveErr, os.ErrNotExist) {
		return "", "", resolveErr
	}
	root, err := gitOutput(ctx, filepath.Dir(abs), "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	repo, err := OpenRepo(ctx, root)
	if err != nil {
		return "", "", err
	}
	if repo.CommonDir != s.opts.Repo.CommonDir {
		return "", "", errors.New("draft task belongs to a different repository")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("draft task is outside its checkout")
	}
	return root, filepath.ToSlash(rel), nil
}

// captureTest commits a snapshot of the checkout's effective source bytes
// and checks it out detached for the test. It never changes the author's
// index, HEAD, or working tree.
func (s *Service) captureTest(ctx context.Context, a attempt, root string) (string, string, error) {
	noHooks, err := s.noHooks(a)
	if err != nil {
		return "", "", err
	}
	tree, err := s.captureTree(ctx, root, noHooks)
	if err != nil {
		return "", "", err
	}
	head, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	env := []string{"GIT_AUTHOR_NAME=Rhizome task test", "GIT_AUTHOR_EMAIL=task-test@rhizome.local", "GIT_COMMITTER_NAME=Rhizome task test", "GIT_COMMITTER_EMAIL=task-test@rhizome.local"}
	commit, err := gitBytes(ctx, root, env, "-c", noHooks, "-c", "commit.gpgSign=false", "commit-tree", tree, "-p", head, "-m", "Local task test snapshot", "--no-gpg-sign")
	if err != nil {
		return "", "", err
	}
	snapshot := strings.TrimSpace(string(commit))
	dir := filepath.Join(s.stateDir, "tests", a.id)
	if err = os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return snapshot, "", err
	}
	if err = s.store.Update(ctx, func(st *State) error {
		running(st, a, func(r *RunRecord) { r.Commit, r.Worktree = snapshot, dir })
		return nil
	}); err != nil {
		return snapshot, "", err
	}
	if _, err = s.opts.Repo.git(ctx, "-c", noHooks, "worktree", "add", "--quiet", "--detach", dir, snapshot); err != nil {
		return snapshot, dir, err
	}
	return snapshot, dir, nil
}

// noHooks is the -c setting that points Git at an empty hooks folder in
// the run's folder, so runner-owned Git commands run no repository hooks
// without changing Git configuration.
func (s *Service) noHooks(a attempt) (string, error) {
	runDir, err := s.runDir(a.id)
	if err != nil {
		return "", err
	}
	hooks := filepath.Join(runDir, "hooks")
	if err := os.MkdirAll(hooks, 0o700); err != nil {
		return "", err
	}
	return "core.hooksPath=" + hooks, nil
}

// captureTree writes the checkout's effective working-tree contents as a
// Git tree through a private copy of its index, so the author's index,
// HEAD, and working tree never change. The runner's state folder is left
// out.
func (s *Service) captureTree(ctx context.Context, root, noHooks string) (string, error) {
	index, err := gitOutput(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(index)
	if err != nil {
		return "", err
	}
	indexFile, err := os.CreateTemp("", "rhizome-task-index-*")
	if err != nil {
		return "", err
	}
	privateIndex := indexFile.Name()
	if err = indexFile.Close(); err != nil {
		return "", err
	}
	defer os.Remove(privateIndex)
	if err = os.WriteFile(privateIndex, data, 0o600); err != nil {
		return "", err
	}
	env := []string{"GIT_INDEX_FILE=" + privateIndex}
	tracked, err := gitBytes(ctx, root, env, "-c", noHooks, "ls-files", "-z")
	if err != nil {
		return "", err
	}
	if _, err = gitBytesInput(ctx, root, env, tracked, "-c", noHooks, "update-index", "--no-assume-unchanged", "-z", "--stdin"); err != nil {
		return "", err
	}
	args := []string{"-c", noHooks, "-c", "core.splitIndex=false", "add", "-A", "--", "."}
	// The executing runner owns this directory, including its changing locks.
	if rel, err := filepath.Rel(root, s.stateDir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = filepath.ToSlash(rel)
		// Git rejects even a negative pathspec naming an ignored directory.
		// Already-ignored state needs no explicit exclusion.
		if _, err := gitBytes(ctx, root, nil, "check-ignore", "--quiet", "--", rel); err != nil {
			args = append(args, ":(exclude)"+rel)
		}
	}
	if _, err = gitBytes(ctx, root, env, args...); err != nil {
		return "", err
	}
	tree, err := gitBytes(ctx, root, env, "-c", noHooks, "write-tree")
	return strings.TrimSpace(string(tree)), err
}

func requireTestIndex(root string) error {
	cfg, err := repoconfig.LoadLocalConfig(root)
	if err != nil && !errors.Is(err, repoconfig.ErrNoLocalConfig) {
		return err
	}
	configured := ""
	if cfg != nil {
		configured = cfg.IndexPath
	}
	path := repoconfig.UnifiedIndexPath(root, configured)
	if !fileExists(path) {
		return fmt.Errorf("code-mode task tests need an existing index in %s; open this checkout in Rhizome or run `rzm index` first", root)
	}
	return nil
}
