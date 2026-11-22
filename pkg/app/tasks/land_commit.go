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

	"github.com/atomicobject/rhizome/pkg/vault/taskdef"
)

var errHeadMoved = errors.New("HEAD moved during the run")

// treeEntries maps each of paths that tree holds as a file or symlink to
// "<mode> <object id>"; a path tree lacks has no entry.
func treeEntries(ctx context.Context, dir, tree string, paths []string) (map[string]string, error) {
	out := map[string]string{}
	if len(paths) == 0 {
		return out, nil
	}
	data, err := gitBytes(ctx, dir, nil, append([]string{"--literal-pathspecs", "ls-tree", "-r", "-z", "--full-tree", tree, "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	for _, line := range strings.Split(string(data), "\x00") {
		meta, name, ok := strings.Cut(line, "\t")
		if fields := strings.Fields(meta); ok && want[name] && len(fields) == 3 {
			out[name] = fields[0] + " " + fields[2]
		}
	}
	return out, nil
}

// indexInfo is `git update-index -z --index-info` input that sets each path
// to its entry, removing paths without one. idLen is the repository's
// object id length.
func indexInfo(paths []string, entries map[string]string, idLen int) []byte {
	var removals, sets strings.Builder
	for _, p := range paths {
		if e, ok := entries[p]; ok {
			fmt.Fprintf(&sets, "%s\t%s\x00", e, p)
		} else {
			fmt.Fprintf(&removals, "0 %s\t%s\x00", strings.Repeat("0", idLen), p)
		}
	}
	return []byte(removals.String() + sets.String())
}

// buildTree writes base's tree with info applied, through a private index,
// leaving the person's index alone.
func buildTree(ctx context.Context, dir, noHooks, base string, info []byte) (string, error) {
	tmp, err := os.MkdirTemp("", "rhizome-task-commit-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(tmp, "index")}
	// A split index would put shared files in the repository.
	private := []string{"-c", noHooks, "-c", "core.splitIndex=false"}
	if _, err := gitBytes(ctx, dir, env, append(private, "read-tree", base)...); err != nil {
		return "", err
	}
	if _, err := gitBytesInput(ctx, dir, env, info, append(private, "update-index", "-z", "--index-info")...); err != nil {
		return "", err
	}
	tree, err := gitBytes(ctx, dir, env, append(private, "write-tree")...)
	return strings.TrimSpace(string(tree)), err
}

// commitInPlace commits exactly paths, at their entries from the snapshot
// taken after the run, on top of the snapshot's HEAD. It builds the commit
// through a private index and refuses one that would change any other path,
// as a file replacing a folder would. It then moves the branch under
// moveHead, so a commit, switch, or pull since the snapshot fails it, and
// while the locks are held points the person's index entries for paths at
// the commit (updateIndex); everything else they staged stays staged. Hooks
// and signing are off. It returns "" when the paths need no commit.
func (s *Service) commitInPlace(ctx context.Context, def Definition, a attempt, ip *inPlace, paths []string, entries map[string]string, message string) (string, error) {
	if ip.moved(ctx) != "" {
		return "", errHeadMoved
	}
	if op := operationInProgress(ctx, ip.dir); op != "" {
		return "", fmt.Errorf("%s is in progress in %s", op, ip.dir)
	}
	info := indexInfo(paths, entries, len(ip.head))
	tree, err := buildTree(ctx, ip.dir, ip.noHooks, ip.head, info)
	if err != nil {
		return "", err
	}
	changed, err := diffTrees(ctx, ip.dir, ip.head, tree)
	if err != nil {
		return "", err
	}
	if extra := slices.DeleteFunc(slices.Clone(changed), func(p string) bool { return slices.Contains(paths, p) }); len(extra) > 0 {
		return "", fmt.Errorf("the commit would also change %s, which is not the task's", strings.Join(extra, ", "))
	}
	if len(changed) == 0 {
		return "", nil
	}
	out, err := gitBytesInput(ctx, ip.dir, nil, []byte(inPlaceMessage(def, message, a.slotID())), "-c", ip.noHooks, "commit-tree", "--no-gpg-sign", "-p", ip.head, "-F", "-", tree)
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(out))
	landingStep("commit")
	err = moveHead(ctx, ip, ip.head, commit, "rhizome tasks: "+def.ID, func() error {
		return updateIndex(ctx, ip, paths, info)
	})
	if err != nil {
		return "", err
	}
	return commit, nil
}

// updateIndex applies info to the person's index under index.lock, which it
// takes as Git does (created exclusively, written, renamed over the index),
// so no `git add` can land between its check and its write. Under the lock
// it checks that the index still has paths as HEAD has them, as when the
// run's own paths were chosen, and changes nothing when the person staged
// one since. The new index is built in a private copy.
func updateIndex(ctx context.Context, ip *inPlace, paths []string, info []byte) error {
	index, err := gitOutput(ctx, ip.dir, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return err
	}
	lockFile := index + ".lock"
	lock, err := os.OpenFile(lockFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("another Git command holds %s", lockFile)
	}
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = lock.Close()
			_ = os.Remove(lockFile)
		}
	}()
	staged, err := nameSet(ctx, ip.dir, append([]string{"--literal-pathspecs", "diff-index", "--cached", "--name-only", "-z", "--no-renames", ip.head, "--"}, paths...)...)
	if err != nil {
		return err
	}
	if len(staged) > 0 {
		return fmt.Errorf("%s was staged while Rhizome committed", strings.Join(slices.Sorted(maps.Keys(staged)), ", "))
	}
	tmp, err := os.MkdirTemp("", "rhizome-task-index-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	private := filepath.Join(tmp, "index")
	data, err := os.ReadFile(index)
	if err == nil {
		err = os.WriteFile(private, data, 0o600)
	}
	if err != nil {
		return err
	}
	env := []string{"GIT_INDEX_FILE=" + private}
	if _, err := gitBytesInput(ctx, ip.dir, env, info, "-c", ip.noHooks, "-c", "core.splitIndex=false", "update-index", "-z", "--index-info"); err != nil {
		return err
	}
	if data, err = os.ReadFile(private); err != nil {
		return err
	}
	if _, err := lock.Write(data); err != nil {
		return err
	}
	if err := lock.Close(); err != nil {
		return err
	}
	if err := os.Rename(lockFile, index); err != nil {
		return err
	}
	renamed = true
	return nil
}

// restoreProtected saves the run's diff of protected paths to diffFile and
// puts each path back to its snapshot content, removing ones the run
// created. It touches nothing when the branch or HEAD changed since the
// snapshot, skips paths that differ only in letter case from another
// restored path (a case-only rename, which on a case-insensitive file
// system is one file), and never writes through a symlinked folder. It
// returns the notice for the person.
func restoreProtected(ctx context.Context, ip *inPlace, after string, paths []string, diffFile string) string {
	diff, err := gitBytes(ctx, ip.dir, nil, append([]string{"--literal-pathspecs", "diff", "--binary", "--no-renames", ip.tree, after, "--"}, paths...)...)
	if err == nil {
		if err = os.MkdirAll(filepath.Dir(diffFile), 0o700); err == nil {
			err = os.WriteFile(diffFile, diff, 0o600)
		}
	}
	if err != nil {
		return fmt.Sprintf("The run changed protected files (%s), and saving its diff failed, so Rhizome did not restore them: %v.", strings.Join(paths, ", "), err)
	}
	snapshot, err := treeEntries(ctx, ip.dir, ip.tree, paths)
	if err != nil {
		return fmt.Sprintf("The run changed protected files (%s), and reading their earlier content failed, so Rhizome did not restore them; the run's diff is in %s: %v.", strings.Join(paths, ", "), diffFile, err)
	}
	folded := map[string]int{}
	for _, p := range paths {
		folded[strings.ToLower(p)]++
	}
	var left []string
	var restore []string
	for _, p := range paths {
		if folded[strings.ToLower(p)] > 1 {
			left = append(left, p+" (renamed only in letter case)")
		} else {
			restore = append(restore, p)
		}
	}
	landingStep("restore")
	err = moveHead(ctx, ip, ip.head, "", "rhizome tasks: restore protected files", func() error {
		// Removals first, so a symlink the run put in a folder's place
		// is gone before anything is written beneath it.
		slices.SortStableFunc(restore, func(a, b string) int {
			_, ka := snapshot[a]
			_, kb := snapshot[b]
			return boolOrder(ka) - boolOrder(kb)
		})
		for _, p := range restore {
			if err := restorePath(ctx, ip.dir, p, snapshot[p]); err != nil {
				left = append(left, fmt.Sprintf("%s (%v)", p, err))
			}
		}
		return nil
	})
	if errors.Is(err, errHeadMoved) {
		return fmt.Sprintf("The run changed protected files (%s), but %s, so Rhizome did not restore them; the run's diff is in %s.", strings.Join(paths, ", "), ip.moved(ctx), diffFile)
	}
	if err != nil {
		return fmt.Sprintf("The run changed protected files (%s), and restoring them failed: %v; the run's diff is in %s.", strings.Join(paths, ", "), err, diffFile)
	}
	if len(left) > 0 {
		return fmt.Sprintf("The run changed protected files (%s); Rhizome restored what it safely could, left %s, and saved the run's diff to %s.", strings.Join(paths, ", "), strings.Join(left, ", "), diffFile)
	}
	return fmt.Sprintf("The run changed protected files (%s); Rhizome restored them and saved the run's diff to %s.", strings.Join(paths, ", "), diffFile)
}

func boolOrder(b bool) int {
	if b {
		return 1
	}
	return 0
}

// restorePath puts one path back to entry ("<mode> <object id>") as a
// checkout would write it, or removes it when entry is "".
func restorePath(ctx context.Context, dir, path, entry string) error {
	full, err := checkoutPath(dir, path, entry != "")
	if entry == "" {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		// The run created it.
		return os.RemoveAll(full)
	}
	if err != nil {
		return err
	}
	mode, id, _ := strings.Cut(entry, " ")
	// A file gets what a checkout writes (smudge and line-ending filters);
	// a symlink's target is its raw content.
	args := []string{"cat-file", "--filters", "--path=" + path, id}
	if mode == "120000" {
		args = []string{"cat-file", "blob", id}
	}
	data, err := gitBytes(ctx, dir, nil, args...)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(full); err != nil {
		return err
	}
	switch mode {
	case "120000":
		return os.Symlink(string(data), full)
	case "100755":
		return os.WriteFile(full, data, 0o755)
	default:
		return os.WriteFile(full, data, 0o644)
	}
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
