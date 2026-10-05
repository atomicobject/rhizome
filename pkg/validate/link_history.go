package validate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// linkHistory is git evidence about files that no longer exist at their old
// names. It separates links that broke (their target was deleted or renamed)
// from placeholders that never had a target, and it names where a renamed
// target went.
type linkHistory struct {
	root      string
	available bool
	// unavailable explains why history could not be used.
	unavailable string
	// removed holds link keys of every deleted or renamed-away file, plus every
	// file tracked in HEAD or the index (so uncommitted deletions count).
	removed map[string]struct{}
	// renamedTo maps a link key to the newest rename of its file.
	renamedTo map[string]renameEvent
}

type renameEvent struct {
	to     string
	commit string
}

// historyLinkKey is the case-insensitive name a wikilink uses for a file:
// its base name, without `.md`.
func historyLinkKey(target string) string {
	name := strings.ToLower(path.Base(strings.TrimSpace(target)))
	return strings.TrimSuffix(name, ".md")
}

// Git evidence is bounded so a huge history degrades to the strict fallback
// instead of stalling validation.
const (
	linkHistoryTimeout     = 30 * time.Second
	linkPredatesRenameWait = 5 * time.Second
)

// runLinkGit runs git for history evidence. core.quotepath=false keeps
// non-ASCII names unescaped; GIT_NO_LAZY_FETCH stops a partial clone from
// fetching file contents over the network; no prompt can block validation.
func runLinkGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root, "-c", "core.quotepath=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
	return cmd.Output()
}

// loadLinkHistory reads deletions and renames under root from git. History
// that cannot prove a link broke (no repository, a shallow or partial clone, a
// git failure or timeout) counts as unavailable rather than as "nothing was
// ever deleted", so every unresolved link stays broken.
func loadLinkHistory(ctx context.Context, root string) linkHistory {
	ctx, cancel := context.WithTimeout(ctx, linkHistoryTimeout)
	defer cancel()
	shallow, err := runLinkGit(ctx, root, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return linkHistory{unavailable: gitUnavailableReason(ctx, err)}
	}
	if strings.TrimSpace(string(shallow)) == "true" {
		return linkHistory{unavailable: "the git clone is shallow"}
	}
	if partial, err := runLinkGit(ctx, root, "config", "--get", "extensions.partialClone"); err == nil && strings.TrimSpace(string(partial)) != "" {
		return linkHistory{unavailable: "the git clone is partial, so reading history would fetch file contents from the remote"}
	}
	out, err := runLinkGit(ctx, root, "log", "-z", "--format=commit %H", "--name-status", "-M", "--diff-filter=DR", "--relative", "HEAD", "--", ".")
	if err != nil {
		return linkHistory{unavailable: gitUnavailableReason(ctx, err)}
	}
	history := linkHistory{root: root, available: true, removed: map[string]struct{}{}, renamedTo: map[string]renameEvent{}}
	parseLinkHistory(out, &history)
	// Files still in HEAD or the index but gone from the working tree were
	// deleted or renamed and not yet committed; links to them broke too.
	tracked, err := runLinkGit(ctx, root, "ls-files", "-z", "--cached", "--with-tree=HEAD")
	if err != nil {
		return linkHistory{unavailable: gitUnavailableReason(ctx, err)}
	}
	for _, file := range strings.Split(string(tracked), "\x00") {
		if file != "" {
			history.removed[historyLinkKey(file)] = struct{}{}
		}
	}
	return history
}

// parseLinkHistory reads `git log -z --name-status` output: a `commit <hash>`
// header, then NUL-terminated status and path fields (two paths for a rename).
// Fields are read by position, so no file name is ever mistaken for a header.
func parseLinkHistory(out []byte, history *linkHistory) {
	fields := strings.Split(string(out), "\x00")
	commit := ""
	for i := 0; i < len(fields); i++ {
		field := strings.TrimLeft(fields[i], "\n")
		if hash, ok := strings.CutPrefix(field, "commit "); ok {
			commit = hash
			continue
		}
		switch {
		case strings.HasPrefix(field, "D") && i+1 < len(fields):
			history.removed[historyLinkKey(fields[i+1])] = struct{}{}
			i++
		case strings.HasPrefix(field, "R") && i+2 < len(fields):
			from, to := fields[i+1], fields[i+2]
			key := historyLinkKey(from)
			history.removed[key] = struct{}{}
			// Log order is newest first, so the first rename seen for a name wins.
			if _, seen := history.renamedTo[key]; !seen && historyLinkKey(to) != key {
				history.renamedTo[key] = renameEvent{to: to, commit: commit}
			}
			i += 2
		}
	}
}

func gitUnavailableReason(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("reading git history took longer than %s", linkHistoryTimeout)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "git is not installed"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr := strings.ToLower(string(exitErr.Stderr))
		switch {
		case strings.Contains(stderr, "not a git repository"):
			return "the vault is not a git repository"
		case strings.Contains(stderr, "dubious ownership"):
			return "git refuses the repository because of its safe.directory ownership check"
		case strings.Contains(stderr, "does not have any commits") || strings.Contains(stderr, "bad revision 'head'") || strings.Contains(stderr, "unknown revision"):
			return "the git repository has no commits"
		}
		if line, _, _ := strings.Cut(strings.TrimSpace(string(exitErr.Stderr)), "\n"); line != "" {
			return "git failed: " + line
		}
	}
	return "git history could not be read: " + err.Error()
}

// broke reports whether an unresolved target once named a file.
func (h linkHistory) broke(target string) bool {
	_, ok := h.removed[historyLinkKey(target)]
	return ok
}

// renameDestination follows renames from target to a path accepted by exists
// and returns the commit of the first rename, the one that orphaned target.
func (h linkHistory) renameDestination(target string, exists func(string) bool) (string, string, bool) {
	key := historyLinkKey(target)
	first := ""
	for range 10 {
		next, ok := h.renamedTo[key]
		if !ok {
			return "", "", false
		}
		if first == "" {
			first = next.commit
		}
		if exists(next.to) {
			return next.to, first, true
		}
		key = historyLinkKey(next.to)
	}
	return "", "", false
}

// linkPredatesRename reports whether source already linked to target just
// before commit renamed target away. Only then did the rename orphan the link;
// a link written after the rename names something else.
func (h linkHistory) linkPredatesRename(ctx context.Context, metadata notemeta.Indexer, commit, source, target string) (bool, error) {
	if !h.available || commit == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, linkPredatesRenameWait)
	defer cancel()
	out, err := runLinkGit(ctx, h.root, "show", commit+"^:./"+source)
	if err != nil {
		return false, nil
	}
	formats, err := metadata.FormatRuntime()
	if err != nil {
		return false, fmt.Errorf("historical rename evidence requires RunContext.NoteMetadata: %w", err)
	}
	sourcePath, err := paths.CleanNotePath(source)
	if err != nil {
		return false, nil
	}
	provider, ok := formats.ProviderForPath(paths.RelPath(sourcePath))
	if !ok {
		return false, nil
	}
	authored, err := noteformat.NewAuthoredSource(sourcePath, provider.Descriptor(), out, 0)
	if err != nil {
		return false, nil
	}
	projection, err := formats.Project(authored)
	if err != nil || projection.Status != noteformat.ProjectionStatusCurrent {
		return false, nil
	}
	targetPath, _ := splitFragment(target)
	for _, link := range projection.Facts.Links {
		if link.Resolution == noteformat.LinkResolutionNoteReference && strings.EqualFold(strings.TrimSpace(link.Path), strings.TrimSpace(targetPath)) {
			return true, nil
		}
	}
	return false, nil
}
