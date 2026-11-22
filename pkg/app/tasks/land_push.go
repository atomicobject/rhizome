package tasks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// pushInPlace pushes an in-place run's commit when it fast-forwards the
// branch's upstream (SPEC-0127 US13), returning the commit that reached
// the upstream, or a notice for the person. After a failed push it fetches
// and looks for the slot's commit before retrying; when the upstream
// diverged it rebases only a clean tracked tree, aborting on conflict. It
// never force-pushes or autostashes, and never pushes commits that are not
// task commits.
func (s *Service) pushInPlace(ctx context.Context, def Definition, ip *inPlace, landed, slot string) (string, string) {
	remote, branch, ok := remoteBranch(ctx, ip.dir, ip.branch)
	if !ok {
		return "", fmt.Sprintf("%s has no upstream branch, so commit %s was not pushed.", ip.branch, short(landed))
	}
	tracking := "refs/remotes/" + remote + "/" + branch
	push := func(c string) (string, error) {
		// Everything between the upstream and c is pushed, so a person's
		// own unpushed commits there stop the push.
		n, err := gitOutput(ctx, ip.dir, "rev-list", "--count", "--invert-grep", "--grep=^"+TagKey+": ", tracking+".."+c)
		if err != nil {
			return "", err
		}
		if n != "0" {
			return fmt.Sprintf("%s has local commits that are not task commits, so Rhizome did not push commit %s; push them yourself.", ip.branch, short(c)), nil
		}
		_, err = gitOutput(ctx, ip.dir, "-c", ip.noHooks, "push", "--quiet", "--no-verify", remote, c+":refs/heads/"+branch)
		return "", err
	}
	notice, err := push(landed)
	if notice != "" || err == nil {
		return pushedOr(landed, notice)
	}
	_, _ = gitOutput(ctx, ip.dir, "-c", ip.noHooks, "fetch", "--quiet", remote)
	if isAncestor(ctx, ip.dir, landed, tracking) {
		return landed, ""
	}
	if other, ok, _ := s.opts.Repo.SlotLanded(ctx, tracking, def.ID, slot); ok {
		return "", fmt.Sprintf("Slot %s already reached %s/%s as commit %s, so commit %s stays local.", slot, remote, branch, short(other), short(landed))
	}
	if isAncestor(ctx, ip.dir, tracking, landed) {
		// The upstream did not move, so this was not a race; try once more.
		if notice, err = push(landed); notice == "" && err != nil {
			notice = fmt.Sprintf("Push failed, so commit %s stays local: %v.", short(landed), err)
		}
		return pushedOr(landed, notice)
	}
	if status, err := gitOutput(ctx, ip.dir, "--no-optional-locks", "status", "--porcelain", "--untracked-files=no"); err != nil || status != "" {
		return "", fmt.Sprintf("%s/%s moved and the checkout has uncommitted changes, so Rhizome did not rebase; commit %s stays local.", remote, branch, short(landed))
	}
	if _, err := gitOutput(ctx, ip.dir, "-c", ip.noHooks, "-c", "rebase.autoStash=false", "pull", "--rebase", "--no-autostash", "--quiet", remote, branch); err != nil {
		_, _ = gitOutput(ctx, ip.dir, "-c", ip.noHooks, "rebase", "--abort")
		return "", fmt.Sprintf("Rebasing onto %s/%s failed, so Rhizome aborted it; commit %s stays local: %v.", remote, branch, short(landed), err)
	}
	head, err := gitOutput(ctx, ip.dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err.Error()
	}
	if notice, err = push(head); notice == "" && err != nil {
		notice = fmt.Sprintf("Push after rebasing failed, so commit %s stays local: %v.", short(head), err)
	}
	return pushedOr(head, notice)
}

func pushedOr(commit, notice string) (string, string) {
	if notice != "" {
		return "", notice
	}
	return commit, ""
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
