// Package tasks implements scheduled tasks (SPEC-0127): task files read
// from the primary ref, per-person modes and run records, the Git-history
// clock, the repository-wide runner, and the inbox.
//
// Docs: [[scheduled-tasks]]
package tasks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoPrimaryRef means the primary ref is unset and origin/HEAD is unknown,
// or the configured ref does not resolve to a commit.
var ErrNoPrimaryRef = errors.New("primary ref is not resolvable")

// Repo identifies one repository by its Git common directory, which every
// worktree shares.
type Repo struct {
	CommonDir string
	// MainCheckout is the main worktree's root, or "" for a bare repository.
	MainCheckout string
}

// OpenRepo finds the repository that dir belongs to.
func OpenRepo(ctx context.Context, dir string) (Repo, error) {
	out, err := gitOutput(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Repo{}, fmt.Errorf("not a Git repository: %w", err)
	}
	common := filepath.Clean(out)
	if resolved, err := filepath.EvalSymlinks(common); err == nil {
		common = resolved
	}
	repo := Repo{CommonDir: common}
	bare, _ := gitOutput(ctx, "", "--git-dir="+common, "rev-parse", "--is-bare-repository")
	if bare != "true" && filepath.Base(common) == ".git" {
		repo.MainCheckout = filepath.Dir(common)
	}
	return repo, nil
}

// StateDir is where task state lives: the main checkout's ignored
// .rhizome/task-state, or a per-user folder keyed by the common directory
// when the repository has no main checkout.
func (r Repo) StateDir() (string, error) {
	if r.MainCheckout != "" {
		return filepath.Join(r.MainCheckout, ".rhizome", "task-state"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(r.CommonDir))
	return filepath.Join(base, "rhizome", "task-state", hex.EncodeToString(sum[:8])), nil
}

// git runs a Git command against the common directory, so it works from
// any worktree and for bare repositories.
func (r Repo) git(ctx context.Context, args ...string) (string, error) {
	return gitOutput(ctx, "", append([]string{"--git-dir=" + r.CommonDir}, args...)...)
}

// ResolvePrimary resolves the configured primary ref, or origin/HEAD when
// none is configured, to a ref name and one commit.
func (r Repo) ResolvePrimary(ctx context.Context, configured string) (ref, commit string, err error) {
	ref = strings.TrimSpace(configured)
	if ref == "" {
		ref, err = r.git(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
		if err != nil || ref == "" {
			return "", "", fmt.Errorf("%w: origin/HEAD is not set; set tasks.primaryRef in .rhizome/config.yml or run `git remote set-head origin --auto`", ErrNoPrimaryRef)
		}
	}
	commit, err = r.git(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil || commit == "" {
		return ref, "", fmt.Errorf("%w: %q", ErrNoPrimaryRef, ref)
	}
	return ref, commit, nil
}

// TaskFiles lists the task files at a commit.
func (r Repo) TaskFiles(ctx context.Context, commit string) ([]string, error) {
	out, err := r.git(ctx, "ls-tree", "--name-only", commit, TasksDir+"/")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, ".yml") || strings.HasSuffix(line, ".yaml") {
			files = append(files, line)
		}
	}
	return files, nil
}

// ShowFile reads one file at a commit.
func (r Repo) ShowFile(ctx context.Context, commit, path string) ([]byte, error) {
	out, err := r.git(ctx, "show", commit+":"+path)
	return []byte(out), err
}

// Tagged is the latest commit on a ref whose message carries a task's tag.
type Tagged struct {
	At     time.Time
	Author string
	Commit string
}

// LastTagged finds the latest commit on ref carrying the task's tag as a
// whole line anywhere in its message; squash merges move trailers into the
// body, so Git's trailer parser would miss them.
func (r Repo) LastTagged(ctx context.Context, ref, taskID string) (Tagged, bool, error) {
	out, err := r.git(ctx, "log", "-1", "--format=%H%x1f%cI%x1f%an", "--grep=^"+TagKey+": "+taskID+"$", ref)
	if err != nil || out == "" {
		return Tagged{}, false, err
	}
	parts := strings.Split(out, "\x1f")
	if len(parts) != 3 {
		return Tagged{}, false, fmt.Errorf("unexpected git log output %q", out)
	}
	at, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return Tagged{}, false, err
	}
	return Tagged{Commit: parts[0], At: at, Author: parts[2]}, true, nil
}

// FetchedAt reports when the repository last fetched, from FETCH_HEAD.
func (r Repo) FetchedAt() (time.Time, bool) {
	info, err := os.Stat(filepath.Join(r.CommonDir, "FETCH_HEAD"))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}
