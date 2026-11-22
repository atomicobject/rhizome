package tasks

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// landingHook, when set, runs before each step of an in-place landing that
// changes the checkout, so tests can act as a person working there.
var landingHook func(step string)

func landingStep(step string) {
	if landingHook != nil {
		landingHook(step)
	}
}

// operationInProgress names a Git operation under way in the checkout, such
// as a merge or a rebase, or returns "". In-place runs never start one or
// move HEAD under one.
func operationInProgress(ctx context.Context, dir string) string {
	gitDir, err := gitOutput(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "an unreadable Git state (" + err.Error() + ")"
	}
	for _, op := range [][2]string{
		{"rebase-merge", "a rebase"}, {"rebase-apply", "a rebase"}, {"MERGE_HEAD", "a merge"},
		{"CHERRY_PICK_HEAD", "a cherry-pick"}, {"REVERT_HEAD", "a revert"}, {"sequencer", "a cherry-pick or revert"},
		{"BISECT_LOG", "a bisect"},
	} {
		if _, err := os.Lstat(filepath.Join(gitDir, op[0])); err == nil {
			return op[1]
		}
	}
	if out, err := gitOutput(ctx, dir, "ls-files", "--unmerged"); err != nil || out != "" {
		return "a conflict resolution"
	}
	return ""
}

// nameSet runs a Git command that lists NUL-separated paths.
func nameSet(ctx context.Context, dir string, args ...string) (map[string]bool, error) {
	out, err := gitBytes(ctx, dir, nil, args...)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			set[p] = true
		}
	}
	return set, nil
}

// stagedPaths are the paths whose index entry differs from HEAD.
func stagedPaths(ctx context.Context, dir string) (map[string]bool, error) {
	return nameSet(ctx, dir, "diff-index", "--cached", "--name-only", "-z", "--no-renames", "HEAD")
}

// flaggedPaths are index entries marked assume-unchanged or skip-worktree:
// Git does not report their working-tree edits, so they are the person's.
func flaggedPaths(ctx context.Context, dir string) (map[string]bool, error) {
	out, err := gitBytes(ctx, dir, nil, "ls-files", "-v", "-z")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if len(entry) > 2 && (unicode.IsLower(rune(entry[0])) || entry[0] == 'S') {
			set[entry[2:]] = true
		}
	}
	return set, nil
}

// incomingNotice explains why moving the checkout from one commit to
// another could overwrite the person's files, or returns "": the move
// changes a path flagged assume-unchanged or skip-worktree, or adds a path
// where an untracked or ignored file, folder, or symlink is. Git itself
// refuses to overwrite local changes to tracked files, but it replaces
// ignored files without asking.
func incomingNotice(ctx context.Context, dir, from, to string) (string, error) {
	out, err := gitBytes(ctx, dir, nil, "diff-tree", "-r", "-z", "--no-renames", "--name-status", from, to)
	if err != nil {
		return "", err
	}
	flagged, err := flaggedPaths(ctx, dir)
	if err != nil {
		return "", err
	}
	fields := strings.Split(string(out), "\x00")
	removed := map[string]bool{}
	var added []string
	for i := 0; i+1 < len(fields); i += 2 {
		status, p := fields[i], fields[i+1]
		// A sparse checkout's skipped files are absent, so nothing of the
		// person's is there to overwrite.
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(p))); flagged[p] && err == nil {
			return fmt.Sprintf("the incoming change touches %s, which is marked assume-unchanged or skip-worktree", p), nil
		}
		switch status {
		case "D":
			removed[p] = true
		case "A":
			added = append(added, p)
		}
	}
	for _, p := range added {
		for q := p; q != "." && q != "/"; q = path.Dir(q) {
			if q != p && removed[q] {
				// A tracked file becoming a folder; Git checks it is clean.
				break
			}
			info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(q)))
			if err != nil {
				continue
			}
			if q == p || !info.IsDir() {
				return fmt.Sprintf("the incoming change adds %s, where an untracked or ignored %s is", p, q), nil
			}
		}
	}
	return "", nil
}

// moveHead moves the checkout's branch from old to new inside one Git ref
// transaction that locks HEAD and the branch, after checking that HEAD
// still names ip.branch at old. A commit, switch, pull, or reset since old
// makes it fail with errHeadMoved instead of landing on the wrong branch,
// and while the locks are held those commands fail rather than race. during
// runs under the locks; when it fails, the branch stays at old. new == ""
// only holds the locks for during.
func moveHead(ctx context.Context, ip *inPlace, old, new, reason string, during func() error) (err error) {
	cmd := exec.CommandContext(ctx, "git", "-c", ip.noHooks, "update-ref", "-m", reason, "--stdin")
	cmd.Dir = ip.dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	out := bufio.NewReader(stdout)
	send := func(command string) error {
		if _, err := io.WriteString(in, command+"\n"); err != nil {
			return err
		}
		got, err := out.ReadString('\n')
		if err != nil || strings.TrimSpace(got) != command+": ok" {
			return fmt.Errorf("git update-ref %s: %s", command, cmp.Or(strings.TrimSpace(stderr.String()), strings.TrimSpace(got), fmt.Sprint(err)))
		}
		return nil
	}
	defer func() {
		_ = in.Close()
		if waitErr := cmd.Wait(); err == nil && waitErr != nil {
			err = fmt.Errorf("git update-ref: %v: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
	}()
	change := fmt.Sprintf("update HEAD %s %s", new, old)
	if new == "" {
		change = "verify HEAD " + old
	}
	if err := send("start"); err != nil {
		return err
	}
	if _, err := io.WriteString(in, change+"\n"); err != nil {
		return err
	}
	if err := send("prepare"); err != nil {
		if ip.moved(ctx) != "" {
			return errHeadMoved
		}
		return err
	}
	// HEAD.lock is held, so this reads what HEAD will stay until commit.
	if ref, _ := gitOutput(ctx, ip.dir, "symbolic-ref", "--quiet", "HEAD"); ref != "refs/heads/"+ip.branch {
		_ = send("abort")
		return errHeadMoved
	}
	if err := during(); err != nil {
		_ = send("abort")
		return err
	}
	if new == "" {
		return send("abort")
	}
	return send("commit")
}

// readTree moves the index and working tree from one commit to another as
// a fast-forward checkout does, refusing to overwrite local changes. Run it
// under moveHead.
func readTree(ctx context.Context, ip *inPlace, from, to string) error {
	// Stat-only differences would otherwise count as local changes.
	_, _ = gitBytes(ctx, ip.dir, nil, "-c", ip.noHooks, "update-index", "-q", "--refresh")
	_, err := gitBytes(ctx, ip.dir, nil, "-c", ip.noHooks, "read-tree", "-m", "-u", from, to)
	return err
}

// checkoutPath is rel's location in the checkout for a restore. It refuses
// when any parent of rel is a symlink or not a folder, so nothing is read,
// written, or removed outside the checkout; missing parents are created
// only when create is set.
func checkoutPath(root, rel string, create bool) (string, error) {
	cur := root
	parts := strings.Split(rel, "/")
	for i, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		switch {
		case errors.Is(err, os.ErrNotExist) && create:
			if err := os.Mkdir(cur, 0o755); err != nil {
				return "", err
			}
		case errors.Is(err, os.ErrNotExist):
			return "", os.ErrNotExist
		case err != nil:
			return "", err
		case info.Mode()&os.ModeSymlink != 0:
			return "", fmt.Errorf("%s is a symlink", strings.Join(parts[:i+1], "/"))
		case !info.IsDir():
			return "", fmt.Errorf("%s is not a folder", strings.Join(parts[:i+1], "/"))
		}
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}

// nested reports whether one path is a folder holding the other.
func nested(a, b string) bool {
	return strings.HasPrefix(b, a+"/") || strings.HasPrefix(a, b+"/")
}
