package tasks

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const protectedDiffFile = "protected.diff"

// inPlace is the open checkout an in-place run works in, as it was just
// before the agent started (SPEC-0127 US12).
type inPlace struct {
	dir, branch, head, tree string
	// noHooks disables repository hooks for runner-owned Git commands.
	noHooks string
	// The person's paths before the run: dirty differ from HEAD in the
	// snapshot (staged, unstaged, or untracked), staged differ in the
	// index, flagged are marked assume-unchanged or skip-worktree, and
	// ignored are the untracked files and folders the snapshot left out
	// because Git ignored them.
	dirty, staged, flagged, ignored map[string]bool
	// excludeFile and exclude are the repository's info/exclude file, as
	// Git names it, and its content before the run.
	excludeFile, exclude string
}

// hostCheckout is the checkout in-place tasks land in (taskCheckout).
func (s *Service) hostCheckout() string { return s.taskCheckout() }

// pushAllowed reports whether runs may push: the repository allows it and
// the person turned push on for it on this machine.
func (s *Service) pushAllowed() bool {
	if !s.opts.Config.AllowPush {
		return false
	}
	if s.opts.Push {
		return true
	}
	m, err := s.MachineSettings()
	return err == nil && m.Repository.Push
}

// runInPlace does an in-place task's work in the open checkout and commits
// the task's own changes. itemID is the person's due or needs-attention
// item, or "" for a scheduled run, which gets an item once the checkout is
// ready. A checkout that cannot take the run skips the task with one
// notice, reusing the task's earlier one.
func (s *Service) runInPlace(ctx context.Context, def Definition, a attempt, itemID string, check CheckResult, ref, commit string) Outcome {
	if s.opts.Agent == nil {
		return s.fail(ctx, def, a, itemID, commit, "no agent harness is available to run the prompt", 0)
	}
	ip, notice, err := s.prepareInPlace(ctx, def, a, ref)
	if notice != "" {
		return s.failWith(ctx, def, a, itemID, commit, OutcomeSkipped, notice, 0, nil)
	}
	var done slotLandedError
	if errors.As(err, &done) {
		return s.slotLandedRun(ctx, def, a, done.commit, commit)
	}
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, "prepare the checkout: "+err.Error(), 0)
	}
	var leftover map[string]string
	if err := s.store.Update(ctx, func(st *State) error {
		now := s.opts.Now()
		var it *Item
		if itemID != "" {
			old, err := st.Item(itemID)
			if err != nil {
				return err
			}
			if old.State != StateDue && old.State != StateAttention {
				return fmt.Errorf("item %s is now %s", old.ID, old.State)
			}
			if old.Worktree == "" {
				it = old
			}
		}
		if it == nil {
			// A failed pr attempt's worktree stays on its own item.
			it = st.AddItem(Item{Task: def.ID, Kind: KindTask}, now)
			itemID = it.ID
		}
		it.set(StateRunning, now)
		it.Reason, it.Summary, it.Context = "", check.Summary, check.Context
		it.Commit, it.Landing, it.Run = commit, def.Land, a.id
		running(st, a, func(r *RunRecord) { r.Stage, r.Item, r.Landing = StageAgent, itemID, def.Land })
		// Files left in another checkout are the person's there, and here
		// the same paths are the person's own changes.
		if l, ok := st.Leftover[def.ID]; ok && l.Checkout == resolved(ip.dir) {
			leftover = maps.Clone(l.Files)
		} else {
			delete(st.Leftover, def.ID)
		}
		return nil
	}); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Item: itemID, Reason: err.Error()}
	}
	loc := s.locations(def, ip.head)
	transcript := s.runFile(a.id, transcriptFile)
	for _, dir := range []string{loc.DataDir, filepath.Dir(transcript)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return s.fail(ctx, def, a, itemID, commit, err.Error(), 0)
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, def.Limit)
	res, runErr := s.opts.Agent.RunPrompt(runCtx, AgentRequest{Task: def.ID, Deny: def.Deny, Dir: ip.dir, Prompt: InPlacePrompt(def, loc, check, ref, ip.dir, ip.branch), Transcript: transcript, Env: s.agentEnv(loc)})
	cancel()
	// A stopped run still records and protects what it changed.
	return s.settleInPlace(context.WithoutCancel(ctx), def, a, itemID, commit, ip, leftover, res, runErr)
}

// slotLandedError means another machine landed the run's slot while the
// checkout was being brought up to date.
type slotLandedError struct{ commit string }

func (e slotLandedError) Error() string { return "slot already landed as " + e.commit }

// prepareInPlace brings the open checkout up to the primary ref and
// snapshots it. A notice means the task does not run: the checkout is not
// on the primary ref's branch, a Git operation is under way there, or the
// fast-forward could overwrite the person's files or was refused.
func (s *Service) prepareInPlace(ctx context.Context, def Definition, a attempt, ref string) (*inPlace, string, error) {
	dir := s.hostCheckout()
	if dir == "" {
		return nil, "in-place tasks need a checkout, and this repository has none", nil
	}
	branch, _ := gitOutput(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if !onBranchOf(ctx, dir, branch, ref) {
		return nil, fmt.Sprintf("the checkout %s is on %s, not the branch of %s, so the in-place task did not run", dir, cmp.Or(branch, "a detached HEAD"), ref), nil
	}
	if op := operationInProgress(ctx, dir); op != "" {
		return nil, fmt.Sprintf("%s is in progress in %s, so the in-place task did not run", op, dir), nil
	}
	noHooks, err := s.noHooks(a)
	if err != nil {
		return nil, "", err
	}
	ip := &inPlace{dir: dir, branch: branch, noHooks: noHooks}
	if branch != ref {
		if remote, _ := gitOutput(ctx, dir, "config", "branch."+branch+".remote"); remote != "" && remote != "." {
			landingStep("fetch")
			// A failed fetch falls back to the last fetched ref.
			fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
			_, _ = gitOutput(fetchCtx, dir, "-c", noHooks, "fetch", "--quiet", remote)
			cancel()
			if now, _ := gitOutput(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); now != branch {
				return nil, fmt.Sprintf("the checkout %s switched from %s to %s while Rhizome fetched, so the in-place task did not run", dir, branch, cmp.Or(now, "a detached HEAD")), nil
			}
		}
	}
	if ip.head, err = gitOutput(ctx, dir, "rev-parse", "HEAD"); err != nil {
		return nil, "", err
	}
	target, err := gitOutput(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return nil, "", err
	}
	// The fetch may have brought another machine's commit for this slot.
	if a.slot != "" {
		if landed, ok, _ := s.opts.Repo.SlotLanded(ctx, target, def.ID, a.slot); ok {
			return nil, "", slotLandedError{landed}
		}
	}
	if branch != ref && target != ip.head && !isAncestor(ctx, dir, target, ip.head) {
		if notice := s.fastForward(ctx, ip, ref, target); notice != "" {
			return nil, notice, nil
		}
		ip.head = target
	}
	if ip.tree, err = s.captureTree(ctx, dir, noHooks); err != nil {
		return nil, "", err
	}
	// The snapshot decides what the person had changed: `git status` does
	// not report edits to assume-unchanged or skip-worktree files.
	dirty, err := diffTrees(ctx, dir, ip.head, ip.tree)
	if err != nil {
		return nil, "", err
	}
	ip.dirty = map[string]bool{}
	for _, p := range dirty {
		ip.dirty[p] = true
	}
	if ip.staged, err = stagedPaths(ctx, dir); err != nil {
		return nil, "", err
	}
	if ip.flagged, err = flaggedPaths(ctx, dir); err != nil {
		return nil, "", err
	}
	if ip.ignored, err = ignoredPaths(ctx, dir); err != nil {
		return nil, "", err
	}
	ip.excludeFile, ip.exclude = excludeRules(ctx, dir)
	return ip, "", nil
}

// fastForward moves the checkout from ip.head to target as `git merge
// --ff-only` would, but never autostashes, runs hooks, overwrites an
// ignored or untracked file, or lands on a branch the person switched to.
// It returns a notice when the checkout cannot take it.
func (s *Service) fastForward(ctx context.Context, ip *inPlace, ref, target string) string {
	refused := func(why string) string {
		return fmt.Sprintf("could not fast-forward %s to %s, so the in-place task did not run: %s", ip.branch, ref, why)
	}
	if !isAncestor(ctx, ip.dir, ip.head, target) {
		return refused(fmt.Sprintf("%s has commits that %s lacks", ip.branch, ref))
	}
	if op := operationInProgress(ctx, ip.dir); op != "" {
		return refused(op + " is in progress")
	}
	landingStep("fast-forward")
	notice, err := moveCheckout(ctx, ip, ip.head, target, "rhizome tasks: fast-forward to "+ref)
	switch {
	case errors.Is(err, errHeadMoved):
		return refused(ip.moved(context.WithoutCancel(ctx)))
	case err != nil:
		return refused(err.Error())
	case notice != "":
		return refused(notice)
	}
	return ""
}

// slotLandedRun records a run whose slot another machine landed while the
// checkout was fetched as skipped, so the task waits for its next slot.
func (s *Service) slotLandedRun(ctx context.Context, def Definition, a attempt, landed, commit string) Outcome {
	reason := fmt.Sprintf("slot %s already landed as commit %s", a.slot, short(landed))
	_ = s.store.Update(ctx, func(st *State) error {
		r := a.record(def.ID, OutcomeSkipped, s.opts.Now())
		r.Message, r.Commit, r.Landing, r.Landed = reason, commit, def.Land, landed
		st.Record(r)
		return nil
	})
	return Outcome{Task: def.ID, Outcome: OutcomeSkipped, Reason: reason}
}

// onBranchOf reports whether a local branch is the primary ref or tracks it.
func onBranchOf(ctx context.Context, dir, branch, ref string) bool {
	if branch == "" {
		return false
	}
	if branch == ref || "refs/heads/"+branch == ref {
		return true
	}
	up, err := gitOutput(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}")
	return err == nil && (up == ref || "refs/remotes/"+up == ref)
}

// moved describes how the checkout's branch or HEAD changed since the
// snapshot, or returns "".
func (ip *inPlace) moved(ctx context.Context) string {
	branch, _ := gitOutput(ctx, ip.dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	head, _ := gitOutput(ctx, ip.dir, "rev-parse", "HEAD")
	if branch == ip.branch && head == ip.head {
		return ""
	}
	return fmt.Sprintf("HEAD moved during the run (%s at %s, now %s at %s)", ip.branch, short(ip.head), cmp.Or(branch, "detached"), short(head))
}

// settleInPlace works out what the run changed, restores protected paths,
// and commits the task's own changes: paths that changed during the run,
// were clean before it, and match writes, plus files an earlier failed run
// left that are still as it left them. A failed run, or one during which
// HEAD moved, commits nothing and leaves its files for the task's next
// successful run.
func (s *Service) settleInPlace(ctx context.Context, def Definition, a attempt, itemID, commit string, ip *inPlace, leftover map[string]string, res AgentResult, runErr error) Outcome {
	after, err := s.captureTree(ctx, ip.dir, ip.noHooks)
	var changed []string
	var gitlinks map[string]bool
	if err == nil {
		changed, gitlinks, err = treeChanges(ctx, ip.dir, ip.tree, after)
	}
	var stagedNow map[string]bool
	if err == nil {
		stagedNow, err = stagedPaths(ctx, ip.dir)
	}
	var before map[string]string
	if err == nil {
		before, err = treeEntries(ctx, ip.dir, ip.tree, slices.Sorted(maps.Keys(leftover)))
	}
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, "read what the run changed: "+err.Error(), res.Tokens)
	}
	var notices []string
	var guarded []string
	for _, p := range changed {
		// What Git ignored before the run was on disk outside the
		// snapshot, so the snapshot cannot put it back; a nested
		// repository is a folder of someone's history.
		if protected(def, p) && !ip.wasIgnored(p) && !gitlinks[p] {
			guarded = append(guarded, p)
		}
	}
	if len(guarded) > 0 {
		notices = append(notices, restoreProtected(ctx, ip, after, guarded, s.runFile(a.id, protectedDiffFile)))
	}
	if notice := ip.ignoreRulesNotice(ctx, def, changed); notice != "" {
		notices = append(notices, notice)
	}
	owned, left := ownPaths(def, ip, leftover, before, stagedNow, gitlinks, changed, guarded)
	entries, err := treeEntries(ctx, ip.dir, after, owned)
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, "read what the run changed: "+err.Error(), res.Tokens)
	}
	keep := func(st *State, it *Item, r *RunRecord) {
		r.Landing, r.Changed, r.Left = def.Land, owned, left.paths()
		if st.Leftover == nil {
			st.Leftover = map[string]Leftover{}
		}
		files := map[string]string{}
		for _, p := range owned {
			files[p] = entries[p]
		}
		st.Leftover[def.ID] = Leftover{Checkout: resolved(ip.dir), Files: files}
	}
	if runErr != nil {
		reason := fmt.Sprintf("Prompt run failed: %v.", runErr)
		if len(owned) > 0 {
			reason += fmt.Sprintf(" Its changes stay in %s (%s); the task's next successful run commits them.", ip.dir, strings.Join(owned, ", "))
		}
		return s.failWith(ctx, def, a, itemID, commit, OutcomeFailed, joinNotices(append([]string{reason, left.text()}, notices...)...), res.Tokens, keep)
	}
	message := strings.TrimSpace(res.Message)
	if len(message) > maxMessage {
		message = message[:maxMessage]
	}
	landed := ""
	if len(owned) > 0 {
		if landed, err = s.commitInPlace(ctx, def, a, ip, owned, entries, message); err != nil {
			reason := "Commit failed: " + err.Error()
			if errors.Is(err, errHeadMoved) {
				reason = ip.moved(ctx) + ", so Rhizome did not commit"
			}
			reason += ". The task's files stay in the checkout (" + strings.Join(owned, ", ") + "); its next successful run commits them."
			return s.failWith(ctx, def, a, itemID, commit, OutcomeFailed, joinNotices(append([]string{reason, left.text()}, notices...)...), res.Tokens, keep)
		}
	}
	if landed != "" && s.pushAllowed() {
		var notice string
		if landed, notice = s.pushInPlace(ctx, def, a, ip, landed); notice != "" {
			notices = append(notices, notice)
		}
	}
	outcome := OutcomeLanded
	if landed == "" {
		outcome = OutcomeNoChanges
	}
	now := s.opts.Now()
	if err := s.store.Update(ctx, func(st *State) error {
		delete(st.Leftover, def.ID)
		reason := joinNotices(append([]string{left.text()}, notices...)...)
		if it, err := st.Item(itemID); err == nil && it.State == StateRunning {
			it.Message, it.Tokens, it.Changed, it.Landed, it.Reason = message, res.Tokens, owned, landed, reason
			if len(notices) > 0 {
				it.set(StateAttention, now)
			} else {
				it.set(StateDone, now)
			}
		}
		r := a.record(def.ID, outcome, now)
		r.Tokens, r.Message, r.Item, r.Commit, r.Changed = res.Tokens, firstLine(message), itemID, ip.head, owned
		r.Landing, r.Landed, r.Left = def.Land, landed, left.paths()
		st.Record(r)
		return nil
	}); err != nil {
		return Outcome{Task: def.ID, Outcome: OutcomeFailed, Item: itemID, Reason: fmt.Sprintf("the run finished (%s, commit %s) but its result could not be saved: %v", outcome, short(landed), err)}
	}
	return Outcome{Task: def.ID, Outcome: outcome, Item: itemID, Reason: joinNotices(notices...)}
}

// InPlacePrompt is the preamble plus the task's prompt for a run in the
// open checkout.
func InPlacePrompt(def Definition, loc Locations, check CheckResult, ref, dir, branch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are running the scheduled task %q (%s) in the background. No person is present to answer questions, so make reasonable choices and keep going.\n\n", def.Name, def.ID)
	fmt.Fprintf(&b, "Work in this checkout: %s\nIt is on the branch %s at commit %s, up to date with %s. A person works here too, so leave files you were not asked to change alone.\n\n", dir, branch, loc.Commit, ref)
	writeLocations(&b, loc, check)
	fmt.Fprintf(&b, "Change only files matching these patterns: %s\nWhen you finish, Rhizome commits your changes to those files with your final message and tags the commit with the task; changes to other files stay uncommitted, and changes under .agents/ or .rhizome/ that the patterns do not name are undone. Do not stage, commit, stash, push, or switch branches. End with a short message for the person: what you did and anything they should check.\n\n", strings.Join(def.Writes, ", "))
	b.WriteString("Task instructions:\n\n")
	b.WriteString(strings.TrimSpace(def.Prompt))
	b.WriteString("\n")
	return b.String()
}
