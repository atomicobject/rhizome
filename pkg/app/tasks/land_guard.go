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
	"time"
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

// ignoredPaths are the untracked files and folders Git ignores, which a
// snapshot leaves out; an ignored folder is one entry, without its slash.
func ignoredPaths(ctx context.Context, dir string) (map[string]bool, error) {
	listed, err := nameSet(ctx, dir, "-c", "core.fsmonitor=false", "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for p := range listed {
		set[strings.TrimSuffix(p, "/")] = true
	}
	return set, nil
}

// excludeRules reads the repository's info/exclude file, returning its
// name as Git gives it and its content ("" when absent).
func excludeRules(ctx context.Context, dir string) (string, string) {
	name, err := gitOutput(ctx, dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return "", ""
	}
	full := name
	if !filepath.IsAbs(full) {
		full = filepath.Join(dir, full)
	}
	data, _ := os.ReadFile(full)
	return name, string(data)
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

// nameStatus lists the paths that differ between two commits with their
// `git diff-tree --name-status` letter.
func nameStatus(ctx context.Context, dir, from, to string) ([][2]string, error) {
	out, err := gitBytes(ctx, dir, nil, "diff-tree", "-r", "-z", "--no-renames", "--name-status", from, to)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(string(out), "\x00")
	var changes [][2]string
	for i := 0; i+1 < len(fields); i += 2 {
		changes = append(changes, [2]string{fields[i], fields[i+1]})
	}
	return changes, nil
}

// incomingNotice explains why a move that makes the incoming changes could
// overwrite the person's files, or returns "": it changes a path flagged
// assume-unchanged or skip-worktree, or adds a path where an untracked or
// ignored file, folder, or symlink is. Git itself refuses to overwrite
// local changes to tracked files, but it replaces ignored files without
// asking.
func incomingNotice(ctx context.Context, dir string, incoming [][2]string) (string, error) {
	flagged, err := flaggedPaths(ctx, dir)
	if err != nil {
		return "", err
	}
	removed := map[string]bool{}
	var added []string
	for _, c := range incoming {
		status, p := c[0], c[1]
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

// localGitTimeout bounds the Git commands that move the checkout, which a
// stop does not interrupt.
const localGitTimeout = 5 * time.Minute

// moveCheckout moves the checkout's branch, index, and working tree from
// one commit to another, as a fast-forward checkout does, under moveHead.
// It checks with incomingNotice while the locks are held, returning the
// notice when the move could overwrite the person's files. A stop does not
// cut it short: Git killed mid-move leaves its lock files behind, so it
// runs on until done or localGitTimeout.
func moveCheckout(ctx context.Context, ip *inPlace, from, to, reason string) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), localGitTimeout)
	defer cancel()
	moving := *ip
	moving.head = from
	var notice string
	errRefused := errors.New("refused")
	err := moveHead(ctx, &moving, from, to, reason, func() error {
		incoming, err := nameStatus(ctx, ip.dir, from, to)
		if err != nil {
			return err
		}
		if notice, err = incomingNotice(ctx, ip.dir, incoming); err != nil || notice != "" {
			return cmp.Or(err, errRefused)
		}
		return readTree(ctx, &moving, from, to)
	})
	if notice != "" {
		return notice, nil
	}
	return "", err
}

// moveHead moves the checkout's branch from old to new inside one Git ref
// transaction that locks HEAD and the branch, after checking that HEAD
// still names ip.branch at old. A commit, switch, pull, or reset since old
// makes it fail with errHeadMoved instead of landing on the wrong branch,
// and while the locks are held those commands fail rather than race. during
// runs under the locks; when it fails, the branch stays at old. new == ""
// only holds the locks for during.
func moveHead(ctx context.Context, ip *inPlace, old, new, reason string, during func() error) error {
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
	var waitErr error
	waited := false
	// stderr is safe to read only once Wait has returned.
	wait := func() error {
		if !waited {
			waited = true
			_ = in.Close()
			waitErr = cmd.Wait()
		}
		return waitErr
	}
	defer wait()
	failed := func(step string) error {
		_ = wait()
		return fmt.Errorf("git update-ref %s: %s", step, cmp.Or(strings.TrimSpace(stderr.String()), fmt.Sprint(waitErr)))
	}
	out := bufio.NewReader(stdout)
	send := func(command string) bool {
		if _, err := io.WriteString(in, command+"\n"); err != nil {
			return false
		}
		got, err := out.ReadString('\n')
		return err == nil && strings.TrimSpace(got) == command+": ok"
	}
	change := fmt.Sprintf("update HEAD %s %s", new, old)
	if new == "" {
		change = "verify HEAD " + old
	}
	if !send("start") {
		return failed("start")
	}
	if _, err := io.WriteString(in, change+"\n"); err != nil {
		return failed(change)
	}
	if !send("prepare") {
		err := failed("prepare")
		if ip.moved(ctx) != "" {
			return errHeadMoved
		}
		return err
	}
	// HEAD.lock is held, so this reads what HEAD will stay until commit.
	if ref, _ := gitOutput(ctx, ip.dir, "symbolic-ref", "--quiet", "HEAD"); ref != "refs/heads/"+ip.branch {
		send("abort")
		return errHeadMoved
	}
	if err := during(); err != nil {
		send("abort")
		return err
	}
	step := "commit"
	if new == "" {
		step = "abort"
	}
	if !send(step) {
		return failed(step)
	}
	if err := wait(); err != nil {
		return failed(step)
	}
	return nil
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
