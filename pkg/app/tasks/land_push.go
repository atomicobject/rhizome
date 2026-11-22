package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

// pushInPlace pushes an in-place run's commit when it fast-forwards the
// branch as the push destination has it (SPEC-0127 US13). It returns the
// task's commit the branch now holds, which a replay remakes, with a notice
// for the person when that commit did not reach the push destination.
// After a failed push it fetches and looks for its commit or the slot's
// before retrying; when the upstream diverged it remakes the task's commit
// on the upstream (replay). It never force-pushes, rebases, or autostashes,
// and pushes only commits Rhizome made. Each commit is saved on the
// running record before it is pushed, so a run stopped mid-push still
// counts it as Rhizome's.
func (s *Service) pushInPlace(ctx context.Context, def Definition, a attempt, ip *inPlace, landed string) (string, string) {
	remote, branch, ok := remoteBranch(ctx, ip.dir, ip.branch)
	if !ok {
		return landed, fmt.Sprintf("%s has no upstream branch, so commit %s was not pushed.", ip.branch, short(landed))
	}
	s.noteLanded(ctx, a, landed)
	landingStep("push")
	made := s.madeCommits(landed)
	notice, err := s.pushCommit(ctx, ip, remote, branch, landed, made)
	if notice != "" || err == nil {
		return landed, notice
	}
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	_, _ = gitOutput(fetchCtx, ip.dir, "-c", ip.noHooks, "fetch", "--quiet", remote)
	cancel()
	upstream, upErr := gitOutput(ctx, ip.dir, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch+"^{commit}")
	if upErr != nil {
		return landed, fmt.Sprintf("Push failed, so commit %s stays local: %v.", short(landed), err)
	}
	if isAncestor(ctx, ip.dir, landed, upstream) {
		return landed, ""
	}
	slot := a.slotID()
	if other, ok, _ := s.opts.Repo.SlotLanded(ctx, upstream, def.ID, slot); ok {
		return landed, fmt.Sprintf("Slot %s already reached %s/%s as commit %s, so commit %s stays local.", slot, remote, branch, short(other), short(landed))
	}
	if isAncestor(ctx, ip.dir, upstream, landed) {
		// The upstream did not move, so this was not a race; try once more.
		if notice, err = s.pushCommit(ctx, ip, remote, branch, landed, made); notice == "" && err != nil {
			notice = fmt.Sprintf("Push failed, so commit %s stays local: %v.", short(landed), err)
		}
		return landed, notice
	}
	replayed, notice := s.replay(ctx, def, ip, landed, upstream)
	if notice != "" {
		return landed, fmt.Sprintf("%s/%s moved and %s, so Rhizome did not rebase; commit %s stays local.", remote, branch, notice, short(landed))
	}
	s.noteLanded(ctx, a, replayed)
	made[replayed] = true
	if notice, err = s.pushCommit(ctx, ip, remote, branch, replayed, made); notice == "" && err != nil {
		notice = fmt.Sprintf("Push after rebasing failed, so commit %s stays local: %v.", short(replayed), err)
	}
	return replayed, notice
}

// noteLanded saves commit as the running attempt's landed commit.
func (s *Service) noteLanded(ctx context.Context, a attempt, commit string) {
	_ = s.store.Update(ctx, func(st *State) error {
		running(st, a, func(r *RunRecord) { r.Landed = commit })
		return nil
	})
}

// errDiverged means the push destination has commits the task's commit
// lacks.
var errDiverged = errors.New("the remote branch has commits this checkout lacks")

// pushCommit pushes c to remote's branch when that fast-forwards the branch
// as the push destination has it now (not as the fetch remote's tracking
// ref says), and every commit it adds is one Rhizome made. The push is tied
// to the commit the check saw, so a branch that moved since, even back to
// an ancestor of c, rejects it. A notice means Rhizome chose not to push.
func (s *Service) pushCommit(ctx context.Context, ip *inPlace, remote, branch, c string, made map[string]bool) (string, error) {
	urls, err := gitOutput(ctx, ip.dir, "remote", "get-url", "--push", "--all", remote)
	if err != nil {
		return "", err
	}
	if n := len(strings.Split(urls, "\n")); urls == "" || n != 1 {
		return fmt.Sprintf("%s has %d push URLs, so Rhizome did not push commit %s; push it yourself.", remote, n, short(c)), nil
	}
	netCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	ref := "refs/heads/" + branch
	out, err := gitOutput(netCtx, ip.dir, "ls-remote", "--", urls, ref)
	if err != nil {
		return "", errors.New(redactURLCredentials(err.Error()))
	}
	dest := ""
	for _, line := range strings.Split(out, "\n") {
		if id, name, _ := strings.Cut(line, "\t"); name == ref {
			dest = id
		}
	}
	switch {
	case dest == "":
		return fmt.Sprintf("%s has no branch %s, so commit %s was not pushed.", remote, branch, short(c)), nil
	case dest == c:
		return "", nil
	case !hasCommit(ctx, ip.dir, dest) || !isAncestor(ctx, ip.dir, dest, c):
		return "", errDiverged
	}
	commits, err := gitOutput(ctx, ip.dir, "rev-list", dest+".."+c)
	if err != nil {
		return "", err
	}
	for _, id := range strings.Fields(commits) {
		if !made[id] {
			return fmt.Sprintf("%s has commits that are not Rhizome's task commits (%s), so Rhizome did not push commit %s; push them yourself.", ip.branch, short(id), short(c)), nil
		}
	}
	// A plain push would accept any remote that is an ancestor of c, so a
	// branch rewound past dest since the check would get the rewound commits
	// back. The lease allows the update only while the remote still holds
	// dest, and c descends from dest, so this is always a fast-forward,
	// never a force (SPEC-0127 US13). A missing branch never gets here.
	landingStep("push-checked")
	_, err = gitOutput(netCtx, ip.dir, "-c", ip.noHooks, "push", "--quiet", "--no-verify", "--force-with-lease="+ref+":"+dest, remote, c+":"+ref)
	if err != nil {
		return "", errors.New(redactURLCredentials(err.Error()))
	}
	return "", nil
}

// madeCommits are the commits Rhizome landed in place on this machine, as
// its run records name them, plus own. A stopped or interrupted run's
// record counts too: its commit may already be on the branch.
func (s *Service) madeCommits(own ...string) map[string]bool {
	made := map[string]bool{}
	for _, c := range own {
		made[c] = true
	}
	st, err := s.store.Load()
	if err != nil {
		return made
	}
	for _, r := range slices.Concat(st.History, slices.Collect(maps.Values(st.Last))) {
		landedHere := r.Outcome == OutcomeLanded || r.Outcome == OutcomeStopped || r.Outcome == OutcomeInterrupted
		if landedHere && r.Landing == taskdef.LandInPlace && r.Landed != "" {
			made[r.Landed] = true
		}
	}
	return made
}

func hasCommit(ctx context.Context, dir, id string) bool {
	_, err := gitOutput(ctx, dir, "cat-file", "-e", id+"^{commit}")
	return err == nil
}

// replay remakes the task's commit on top of upstream and moves the checkout
// there, as `git pull --rebase` would for one commit, but with nothing in
// progress to abort. It returns a notice instead when the person committed
// or switched branches since the task's commit, other local commits sit
// under it, a Git operation is under way, a tracked file has uncommitted
// changes, the upstream changed the task's files, or the move could
// overwrite the person's files.
func (s *Service) replay(ctx context.Context, def Definition, ip *inPlace, landed, upstream string) (string, string) {
	if head, _ := gitOutput(ctx, ip.dir, "rev-parse", "HEAD"); head != landed {
		return "", "HEAD is no longer the task's commit"
	}
	if op := operationInProgress(ctx, ip.dir); op != "" {
		return "", op + " is in progress"
	}
	if ahead, err := gitOutput(ctx, ip.dir, "rev-list", upstream+".."+landed); err != nil || ahead != landed {
		return "", "other local commits are not on the upstream"
	}
	if status, err := gitOutput(ctx, ip.dir, "--no-optional-locks", "status", "--porcelain", "--untracked-files=no"); err != nil || status != "" {
		return "", "the checkout has uncommitted changes"
	}
	parent := landed + "^"
	ours, err := diffTrees(ctx, ip.dir, parent, landed)
	if err != nil {
		return "", err.Error()
	}
	theirs, err := diffTrees(ctx, ip.dir, parent, upstream)
	if err != nil {
		return "", err.Error()
	}
	for _, p := range ours {
		for _, q := range theirs {
			if p == q || nested(p, q) {
				return "", "the upstream changed " + q + ", which the task's commit changes too"
			}
		}
	}
	entries, err := treeEntries(ctx, ip.dir, landed, ours)
	if err != nil {
		return "", err.Error()
	}
	tree, err := buildTree(ctx, ip.dir, ip.noHooks, upstream, indexInfo(ours, entries, len(landed)))
	if err != nil {
		return "", err.Error()
	}
	message, err := gitBytes(ctx, ip.dir, nil, "log", "-1", "--format=%B", landed)
	if err != nil {
		return "", err.Error()
	}
	out, err := gitBytesInput(ctx, ip.dir, nil, message, "-c", ip.noHooks, "commit-tree", "--no-gpg-sign", "-p", upstream, "-F", "-", tree)
	if err != nil {
		return "", err.Error()
	}
	replayed := strings.TrimSpace(string(out))
	landingStep("replay")
	notice, err := moveCheckout(ctx, ip, landed, replayed, "rhizome tasks: "+def.ID+" onto "+short(upstream))
	switch {
	case errors.Is(err, errHeadMoved):
		return "", "HEAD moved"
	case err != nil:
		return "", err.Error()
	case notice != "":
		return "", notice
	}
	return replayed, ""
}

func isAncestor(ctx context.Context, dir, a, b string) bool {
	_, err := gitOutput(ctx, dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// remoteBranch names the remote and branch of a remote-tracking ref, or of
// a local branch's upstream.
func remoteBranch(ctx context.Context, dir, ref string) (remote, branch string, ok bool) {
	full, err := gitOutput(ctx, dir, "rev-parse", "--symbolic-full-name", ref)
	if err != nil {
		return "", "", false
	}
	if local, isLocal := strings.CutPrefix(full, "refs/heads/"); isLocal {
		remote, _ = gitOutput(ctx, dir, "config", "branch."+local+".remote")
		merge, _ := gitOutput(ctx, dir, "config", "branch."+local+".merge")
		branch, _ = strings.CutPrefix(merge, "refs/heads/")
		return remote, branch, remote != "" && remote != "." && branch != ""
	}
	rest, isRemote := strings.CutPrefix(full, "refs/remotes/")
	if !isRemote {
		return "", "", false
	}
	remotes, _ := gitOutput(ctx, dir, "remote")
	for _, r := range strings.Split(remotes, "\n") {
		if b, found := strings.CutPrefix(rest, r+"/"); r != "" && found {
			return r, b, true
		}
	}
	return "", "", false
}

// pushPR pushes a pr run's branch and opens a pull request against the
// primary ref's branch, carrying the task tag in its body (SPEC-0127
// US13). It returns the pull request's URL, or a notice for the person.
func (s *Service) pushPR(ctx context.Context, def Definition, a attempt, ref, branch, dir, message string) (string, string) {
	remote, base, ok := remoteBranch(ctx, dir, ref)
	if !ok {
		return "", fmt.Sprintf("%s has no remote branch, so %s was not pushed.", ref, branch)
	}
	noHooks, err := s.noHooks(a)
	if err != nil {
		return "", err.Error()
	}
	if _, err := gitOutput(ctx, dir, "-c", noHooks, "push", "--quiet", "--no-verify", remote, branch+":refs/heads/"+branch); err != nil {
		return "", fmt.Sprintf("Pushing %s failed, so the branch stays local: %v.", branch, err)
	}
	body := strings.TrimSpace(message) + "\n\n" + def.Tag() + "\n"
	url, err := openPullRequest(ctx, dir, base, branch, "chore(tasks): "+truncate(def.Name, 60), body)
	if err != nil {
		return "", fmt.Sprintf("Pushed %s, but opening its pull request failed: %v.", branch, err)
	}
	return url, ""
}

// openPullRequest opens a pull request with gh and returns its URL; tests
// replace it.
var openPullRequest = func(ctx context.Context, dir, base, head, title, body string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "pr", "create", "--base", base, "--head", head, "--title", title, "--body-file", "-")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh pr create: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	return strings.TrimSpace(lines[len(lines)-1]), nil
}
