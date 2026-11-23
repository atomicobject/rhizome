package tasks

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
)

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
// it leaves. Anything the person had changed, staged, flagged, or ignored
// before the run is theirs, except a path an earlier failed run left (leftover) whose
// content before this run (before) is still what that run left. A path
// staged since the snapshot (stagedNow) is theirs too, and so is any task
// path whose commit would replace a file or folder that is not the task's,
// and a gitlink (a submodule or nested repository) on either side.
// ponytail: a person's edit to a clean file inside writes during the run
// is indistinguishable from the task's and is committed with it (US12).
func ownPaths(def Definition, ip *inPlace, leftover, before map[string]string, stagedNow, gitlinks map[string]bool, changed, guarded []string) ([]string, leftPaths) {
	adopted := func(p string) bool {
		want, ok := leftover[p]
		return ok && before[p] == want
	}
	why := func(p string) string {
		switch {
		case ip.wasIgnored(p):
			return "ignored before the run"
		case gitlinks[p]:
			return "a nested Git repository"
		case ip.staged[p]:
			return "staged before the run"
		case stagedNow[p]:
			return "staged during the run"
		case ip.flagged[p]:
			return "marked assume-unchanged or skip-worktree"
		case ip.dirty[p] && !adopted(p):
			return "changed before the run"
		case !def.MayWrite(p):
			return "outside writes"
		}
		return ""
	}
	var owned []string
	var left leftPaths
	for _, p := range changed {
		if slices.Contains(guarded, p) {
			continue
		}
		if reason := why(p); reason != "" {
			left = append(left, [2]string{p, reason})
		} else {
			owned = append(owned, p)
		}
	}
	for p := range leftover {
		if !slices.Contains(changed, p) && ip.dirty[p] && adopted(p) && why(p) == "" && !protected(def, p) {
			owned = append(owned, p)
		}
	}
	// ponytail: owned x others string scan; index by folder if checkouts
	// with many thousands of changed files make it slow.
	others := slices.Concat(guarded, left.paths(), slices.Collect(maps.Keys(ip.dirty)), slices.Collect(maps.Keys(ip.staged)), slices.Collect(maps.Keys(stagedNow)))
	mine := map[string]bool{}
	for _, p := range owned {
		mine[p] = true
	}
	owned = slices.DeleteFunc(owned, func(p string) bool {
		for _, o := range others {
			if !mine[o] && nested(p, o) {
				left = append(left, [2]string{p, "replaces " + o + ", which is not the task's"})
				return true
			}
		}
		return false
	})
	slices.Sort(owned)
	return owned, left
}

// wasIgnored reports whether p, or a folder holding it, was an untracked
// path Git ignored before the run. Such a path was on disk outside the
// snapshot, so it is the person's even when changed ignore rules now show
// it as new.
func (ip *inPlace) wasIgnored(p string) bool {
	for q := p; q != "." && q != "/"; q = path.Dir(q) {
		if ip.ignored[q] {
			return true
		}
	}
	return false
}

// ignoreRulesNotice names the ignore files the run changed that are not
// the task's to change: a .gitignore outside writes or protected, and the
// repository's info/exclude file. Either way, files ignored before the run
// stay the person's.
func (ip *inPlace) ignoreRulesNotice(ctx context.Context, def Definition, changed []string) string {
	var files []string
	for _, p := range changed {
		if path.Base(p) == ".gitignore" && (!def.MayWrite(p) || protected(def, p)) {
			files = append(files, p)
		}
	}
	if name, rules := excludeRules(ctx, ip.dir); rules != ip.exclude {
		files = append(files, name)
	}
	if len(files) == 0 {
		return ""
	}
	return fmt.Sprintf("The run changed ignore rules (%s). Files they hid before the run are yours: Rhizome neither commits nor restores them.", strings.Join(files, ", "))
}

func joinNotices(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), " ")
}

func diffTrees(ctx context.Context, dir, from, to string) ([]string, error) {
	paths, _, err := treeChanges(ctx, dir, from, to)
	return paths, err
}

// treeChanges lists the paths that differ between two trees, and the ones
// among them that are a gitlink (mode 160000) on either side.
func treeChanges(ctx context.Context, dir, from, to string) ([]string, map[string]bool, error) {
	out, err := gitBytes(ctx, dir, nil, "diff-tree", "-r", "-z", "--no-renames", "--raw", from, to)
	if err != nil {
		return nil, nil, err
	}
	var paths []string
	gitlinks := map[string]bool{}
	fields := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		// ":<old mode> <new mode> <old id> <new id> <status>", then the path.
		meta, p := strings.Fields(fields[i]), fields[i+1]
		paths = append(paths, p)
		if len(meta) > 1 && (meta[0] == ":160000" || meta[1] == "160000") {
			gitlinks[p] = true
		}
	}
	return paths, gitlinks, nil
}
