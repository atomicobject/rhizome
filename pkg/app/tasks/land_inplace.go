package tasks

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

const protectedDiffFile = "protected.diff"

// inPlace is the open checkout an in-place run works in, as it was just
// before the agent started (SPEC-0127 US12).
type inPlace struct {
	dir, branch, head, tree string
	// noHooks disables repository hooks for runner-owned Git commands.
	noHooks string
	// status is each path's `git status --porcelain` code before the run.
	status map[string]string
}

// hostCheckout is the checkout in-place tasks land in: the person's
// tasks.repositories.<commonDir>.checkout setting (SPEC-0127 US10), else
// the main checkout.
func (s *Service) hostCheckout() string {
	if m, err := s.MachineSettings(); err == nil && m.Repository.Checkout != "" {
		return m.Repository.Checkout
	}
	return s.opts.Repo.MainCheckout
}

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
	ip, notice, err := s.prepareInPlace(ctx, a, ref)
	if notice != "" {
		return s.failWith(ctx, def, a, itemID, commit, OutcomeSkipped, notice, 0, nil)
	}
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, "prepare the checkout: "+err.Error(), 0)
	}
	var leftover []string
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
		leftover = slices.Clone(st.Leftover[def.ID])
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
	res, runErr := s.opts.Agent.RunPrompt(runCtx, AgentRequest{Dir: ip.dir, Prompt: InPlacePrompt(def, loc, check, ref, ip.dir, ip.branch), Transcript: transcript, Env: s.agentEnv(loc)})
	cancel()
	// A stopped run still records and protects what it changed.
	return s.settleInPlace(context.WithoutCancel(ctx), def, a, itemID, commit, ip, leftover, res, runErr)
}

// prepareInPlace brings the open checkout up to the primary ref and
// snapshots it. A notice means the task does not run: the checkout is not
// on the primary ref's branch, or Git refused to fast-forward it.
func (s *Service) prepareInPlace(ctx context.Context, a attempt, ref string) (*inPlace, string, error) {
	dir := s.hostCheckout()
	if dir == "" {
		return nil, "in-place tasks need a checkout, and this repository has none", nil
	}
	branch, _ := gitOutput(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if !onBranchOf(ctx, dir, branch, ref) {
		return nil, fmt.Sprintf("the checkout %s is on %s, not the branch of %s, so the in-place task did not run", dir, cmp.Or(branch, "a detached HEAD"), ref), nil
	}
	noHooks, err := s.noHooks(a)
	if err != nil {
		return nil, "", err
	}
	if branch != ref {
		if remote, _ := gitOutput(ctx, dir, "config", "branch."+branch+".remote"); remote != "" && remote != "." {
			// A failed fetch falls back to the last fetched ref.
			_, _ = gitOutput(ctx, dir, "-c", noHooks, "fetch", "--quiet", remote)
		}
		// Dirty files are fine; Git refuses when incoming changes touch them.
		if _, err := gitOutput(ctx, dir, "-c", noHooks, "merge", "--ff-only", "--quiet", ref); err != nil {
			return nil, fmt.Sprintf("could not fast-forward %s to %s, so the in-place task did not run: %v", branch, ref, err), nil
		}
	}
	ip := &inPlace{dir: dir, branch: branch, noHooks: noHooks}
	if ip.head, err = gitOutput(ctx, dir, "rev-parse", "HEAD"); err != nil {
		return nil, "", err
	}
	// Status first: a person's edit landing between the two reads is then
	// in the snapshot, so the run never claims it.
	if ip.status, err = gitStatus(ctx, dir); err != nil {
		return nil, "", err
	}
	if ip.tree, err = s.captureTree(ctx, dir, noHooks); err != nil {
		return nil, "", err
	}
	return ip, "", nil
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

// gitStatus maps each path that differs from HEAD, in the index or the
// working tree, untracked ones included, to its two-letter status code.
func gitStatus(ctx context.Context, dir string, paths ...string) (map[string]string, error) {
	args := []string{"--no-optional-locks", "--literal-pathspecs", "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames"}
	if len(paths) > 0 {
		args = append(append(args, "--"), paths...)
	}
	out, err := gitBytes(ctx, dir, nil, args...)
	if err != nil {
		return nil, err
	}
	status := map[string]string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if len(entry) > 3 {
			status[entry[3:]] = entry[:2]
		}
	}
	return status, nil
}

func staged(code string) bool { return code != "" && code[0] != ' ' && code[0] != '?' }

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
// left. A failed run, or one during which HEAD moved, commits nothing and
// leaves its files for the task's next successful run.
func (s *Service) settleInPlace(ctx context.Context, def Definition, a attempt, itemID, commit string, ip *inPlace, leftover []string, res AgentResult, runErr error) Outcome {
	after, err := s.captureTree(ctx, ip.dir, ip.noHooks)
	var changed []string
	if err == nil {
		changed, err = diffTrees(ctx, ip.dir, ip.tree, after)
	}
	if err != nil {
		return s.fail(ctx, def, a, itemID, commit, "read what the run changed: "+err.Error(), res.Tokens)
	}
	var notices []string
	var guarded []string
	for _, p := range changed {
		if protected(def, p) {
			guarded = append(guarded, p)
		}
	}
	if len(guarded) > 0 {
		diff := s.runFile(a.id, protectedDiffFile)
		if err := restoreProtected(ctx, ip, after, guarded, diff); err != nil {
			notices = append(notices, fmt.Sprintf("The run changed protected files (%s), and restoring them failed: %v.", strings.Join(guarded, ", "), err))
		} else {
			notices = append(notices, fmt.Sprintf("The run changed protected files (%s); Rhizome restored them and saved the run's diff to %s.", strings.Join(guarded, ", "), diff))
		}
	}
	owned, left := ownPaths(def, ip, leftover, changed, guarded)
	keep := func(st *State, it *Item, r *RunRecord) {
		r.Landing, r.Changed, r.Left = def.Land, owned, left.paths()
		if st.Leftover == nil {
			st.Leftover = map[string][]string{}
		}
		st.Leftover[def.ID] = owned
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
		if landed, err = s.commitInPlace(ctx, def, a, ip, owned, message); err != nil {
			reason := "Commit failed: " + err.Error()
			if errors.Is(err, errHeadMoved) {
				reason = ip.moved(ctx) + ", so Rhizome did not commit"
			}
			reason += ". The task's files stay in the checkout (" + strings.Join(owned, ", ") + "); its next successful run commits them."
			return s.failWith(ctx, def, a, itemID, commit, OutcomeFailed, joinNotices(append([]string{reason, left.text()}, notices...)...), res.Tokens, keep)
		}
	}
	if landed != "" && s.pushAllowed() {
		pushed, notice := s.pushInPlace(ctx, def, ip, landed, a.slotID())
		landed = cmp.Or(pushed, landed)
		if notice != "" {
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

// leftPaths are changed paths the run does not commit, with why.
type leftPaths [][2]string

func (l leftPaths) paths() []string {
	out := make([]string, len(l))
	for i, p := range l {
		out[i] = p[0]
	}
	return out
}

func (l leftPaths) text() string {
	if len(l) == 0 {
		return ""
	}
	parts := make([]string, len(l))
	for i, p := range l {
		parts[i] = p[0] + " (" + p[1] + ")"
	}
	return "Left uncommitted: " + strings.Join(parts, ", ") + "."
}

// ownPaths splits the run's changed paths into the task's own and the ones
// it leaves. Paths an earlier failed run left count as the task's while
// they are still changed and not staged.
// ponytail: a person's edit to a clean file inside writes during the run
// is indistinguishable from the task's and is committed with it (US12).
func ownPaths(def Definition, ip *inPlace, leftover, changed, guarded []string) ([]string, leftPaths) {
	var owned []string
	var left leftPaths
	for _, p := range changed {
		code := ip.status[p]
		switch {
		case slices.Contains(guarded, p):
		case staged(code):
			left = append(left, [2]string{p, "staged before the run"})
		case code != "" && !slices.Contains(leftover, p):
			left = append(left, [2]string{p, "changed before the run"})
		case !def.MayWrite(p):
			left = append(left, [2]string{p, "outside writes"})
		default:
			owned = append(owned, p)
		}
	}
	for _, p := range leftover {
		if code := ip.status[p]; code != "" && !staged(code) && !slices.Contains(changed, p) && def.MayWrite(p) && !protected(def, p) {
			owned = append(owned, p)
		}
	}
	slices.Sort(owned)
	return owned, left
}

func joinNotices(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), " ")
}

var errHeadMoved = errors.New("HEAD moved during the run")

// commitInPlace commits exactly paths with `git commit --only`, so changes
// the person staged stay staged, with the task tag and slot line. Hooks
// and signing are off. It rechecks the branch and HEAD first.
func (s *Service) commitInPlace(ctx context.Context, def Definition, a attempt, ip *inPlace, paths []string, message string) (string, error) {
	if ip.moved(ctx) != "" {
		return "", errHeadMoved
	}
	now, err := gitStatus(ctx, ip.dir, paths...)
	if err != nil {
		return "", err
	}
	var added []string
	for _, p := range paths {
		if now[p] == "??" {
			added = append(added, p)
		}
	}
	// commit --only accepts only paths Git knows.
	if len(added) > 0 {
		if _, err := gitOutput(ctx, ip.dir, append([]string{"--literal-pathspecs", "-c", ip.noHooks, "add", "--intent-to-add", "--"}, added...)...); err != nil {
			return "", err
		}
	}
	args := append([]string{"--literal-pathspecs", "-c", ip.noHooks, "-c", "commit.gpgSign=false", "commit", "--quiet", "--no-verify", "--only", "-F", "-", "--"}, paths...)
	if _, err := gitBytesInput(ctx, ip.dir, nil, []byte(inPlaceMessage(def, message, a.slotID())), args...); err != nil {
		if len(added) > 0 {
			_, _ = gitOutput(ctx, ip.dir, append([]string{"--literal-pathspecs", "reset", "--quiet", "--"}, added...)...)
		}
		return "", err
	}
	return gitOutput(ctx, ip.dir, "rev-parse", "HEAD")
}

// inPlaceMessage is the commit message: the agent's message without lines
// that would pose as Rhizome's own, then the task tag and slot line.
func inPlaceMessage(def Definition, message, slot string) string {
	var kept []string
	for _, line := range strings.Split(message, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), TagKey) {
			kept = append(kept, line)
		}
	}
	body := cmp.Or(strings.TrimSpace(strings.Join(kept, "\n")), "The task's agent left no message.")
	return fmt.Sprintf("chore(tasks): %s\n\n%s\n\n%s\n%s: %s\n", truncate(def.Name, 60), body, def.Tag(), taskdef.SlotKey, slot)
}

func diffTrees(ctx context.Context, dir, from, to string) ([]string, error) {
	out, err := gitBytes(ctx, dir, nil, "diff-tree", "-r", "-z", "--no-renames", "--name-only", from, to)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// restoreProtected saves the run's diff of protected paths to diffFile,
// then puts each back to its snapshot content, removing ones the run
// created.
func restoreProtected(ctx context.Context, ip *inPlace, after string, paths []string, diffFile string) error {
	diff, err := gitBytes(ctx, ip.dir, nil, append([]string{"--literal-pathspecs", "diff", "--binary", "--no-renames", ip.tree, after, "--"}, paths...)...)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(diffFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(diffFile, diff, 0o600); err != nil {
		return err
	}
	for _, p := range paths {
		if err := restorePath(ctx, ip.dir, ip.tree, p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

func restorePath(ctx context.Context, dir, tree, path string) error {
	full := filepath.Join(dir, filepath.FromSlash(path))
	entry, err := gitOutput(ctx, dir, "ls-tree", "-z", tree, "--", path)
	if err != nil {
		return err
	}
	meta, name, _ := strings.Cut(strings.TrimSuffix(entry, "\x00"), "\t")
	fields := strings.Fields(meta)
	if name != path || len(fields) != 3 {
		// The run created it.
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := gitBytes(ctx, dir, nil, "cat-file", "blob", fields[2])
	if err != nil {
		return err
	}
	if err := os.RemoveAll(full); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	switch fields[0] {
	case "120000":
		return os.Symlink(string(data), full)
	case "100755":
		return os.WriteFile(full, data, 0o755)
	default:
		return os.WriteFile(full, data, 0o644)
	}
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
