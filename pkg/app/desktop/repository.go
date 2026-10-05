package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	rzmpaths "github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type Worktree struct {
	Path          string `json:"path"`
	Branch        string `json:"branch,omitempty"`
	Head          string `json:"head,omitempty"`
	Main          bool   `json:"main,omitempty"`
	Configured    bool   `json:"configured"`
	HasDatabase   bool   `json:"hasDatabase"`
	Trusted       bool   `json:"trusted"`
	TrustRequired bool   `json:"trustRequired"`
	Authority     string `json:"authority,omitempty"`
	Executable    string `json:"executable,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Repository groups every worktree that shares one Git common directory. A
// folder outside Git is a repository with exactly one worktree.
type Repository struct {
	ID            string `json:"id"`
	Root          string `json:"root"`
	Name          string `json:"name"`
	Git           bool   `json:"git"`
	DefaultBranch string `json:"defaultBranch,omitempty"`
	// Subpath is the vault's folder below each Git worktree root, if any.
	Subpath        string     `json:"subpath,omitempty"`
	DefaultPrimary string     `json:"defaultPrimary"`
	Primary        string     `json:"primary"`
	Worktrees      []Worktree `json:"worktrees"`
}

type gitWorktree struct {
	path, branch, head string
	main               bool
	// err marks a worktree Git still lists that cannot be opened.
	err string
}

// Repository discovers worktrees by reading Git metadata files. It never runs
// git or repository code. path may be any worktree or the common Git directory.
//
// A Rhizome folder configured below its Git working tree, such as <repo>/docs,
// is a repository of its own whose worktrees are the same subfolder of every
// Git worktree; its identity is the common Git directory plus that subpath.
func (s *Service) Repository(path, primary string) (Repository, error) {
	if !filepath.IsAbs(path) {
		return Repository{}, problem("invalid_request", "Choose an absolute folder path.")
	}
	canonical, err := repositorytrust.CanonicalCheckout(path)
	if err != nil {
		return Repository{}, problem("folder_missing", "This repository is unavailable. Restore it or remove it from the library.")
	}
	common, hint, subpath, from := canonical, "", "", canonical
	if !gitDirectory(canonical) {
		info, err := s.Inspect(canonical)
		if err != nil {
			return Repository{}, err
		}
		root := gitWorktreeRoot(info.Path)
		if root == "" {
			return s.repository(Repository{ID: info.ID, Root: info.Path, Name: info.Name}, []gitWorktree{{path: info.Path, main: true}}, primary)
		}
		gitDir, err := worktreeGitDir(root)
		if err != nil {
			return Repository{}, problem("folder_missing", "This folder has unreadable Git metadata.")
		}
		common = commonDir(gitDir)
		if gitDir == common {
			hint = root
		}
		if info.Configured && root != info.Path {
			rel, err := rzmpaths.ToRel(rzmpaths.AbsPath(info.Path), root)
			if err != nil {
				return Repository{}, err
			}
			subpath = string(rel)
		}
		from = info.Path
	}
	worktrees, fromHint, defaultBranch, err := readWorktrees(common, hint)
	if err != nil {
		return Repository{}, err
	}
	identity := common
	name := strings.TrimSuffix(filepath.Base(common), ".git")
	if len(worktrees) > 0 && worktrees[0].main {
		name = filepath.Base(worktrees[0].path)
	}
	if subpath != "" {
		identity += "\x00" + subpath
		name += "/" + subpath
		for i := range worktrees {
			worktrees[i].path = filepath.Join(worktrees[i].path, subpath)
			if stat, err := os.Stat(worktrees[i].path); worktrees[i].err == "" && (err != nil || !stat.IsDir()) {
				worktrees[i].err = fmt.Sprintf("This worktree has no %s folder.", subpath)
			}
		}
	}
	digest := sha256.Sum256([]byte(identity))
	repo := Repository{ID: hex.EncodeToString(digest[:]), Root: common, Name: name, Git: true, DefaultBranch: defaultBranch, Subpath: subpath}
	// Discovery restarts from Root, so Root must lead back to this identity. A
	// main working tree that only its own .git file names is found again from
	// that checkout. A subfolder repository is found again only from a
	// worktree whose own configuration is that subfolder; any other subfolder
	// would resolve to the whole repository.
	if subpath != "" || fromHint {
		repo.Root = from
		if i := slices.IndexFunc(worktrees, func(w gitWorktree) bool {
			if w.err != "" || subpath == "" {
				return w.err == ""
			}
			info, err := s.Inspect(w.path)
			return err == nil && info.Configured && info.Path == w.path
		}); i >= 0 {
			repo.Root = worktrees[i].path
		}
	}
	return s.repository(repo, worktrees, primary)
}

func (s *Service) repository(repo Repository, worktrees []gitWorktree, primary string) (Repository, error) {
	for _, wt := range worktrees {
		w := Worktree{Path: wt.path, Branch: wt.branch, Head: wt.head, Main: wt.main, Error: wt.err}
		if w.Error == "" {
			info, err := s.Inspect(wt.path)
			w.Configured, w.Trusted, w.TrustRequired, w.Authority, w.Executable = info.Configured, info.Trusted, info.TrustRequired, info.Authority, info.Executable
			if err != nil {
				w.Error = err.Error()
			}
			if w.Configured {
				_, w.HasDatabase = databasePath(wt.path)
			}
		}
		repo.Worktrees = append(repo.Worktrees, w)
	}
	if len(repo.Worktrees) == 0 {
		return repo, problem("folder_missing", "This repository has no available worktrees.")
	}
	available := func(w Worktree) bool { return w.Error == "" }
	if i := slices.IndexFunc(repo.Worktrees, func(w Worktree) bool {
		return available(w) && repo.DefaultBranch != "" && w.Branch == repo.DefaultBranch
	}); i >= 0 {
		repo.DefaultPrimary = repo.Worktrees[i].Path
	} else if i := slices.IndexFunc(repo.Worktrees, available); i >= 0 {
		repo.DefaultPrimary = repo.Worktrees[i].Path
	} else {
		repo.DefaultPrimary = repo.Worktrees[0].Path
	}
	repo.Primary = repo.DefaultPrimary
	for _, w := range repo.Worktrees {
		if primary != "" && available(w) && w.Path == filepath.Clean(primary) {
			repo.Primary = w.Path
		}
	}
	return repo, nil
}

// databasePath reports the worktree's unified database path and whether it exists.
func databasePath(worktree string) (string, bool) {
	dir, cfg, err := obsidian.FindLocalConfig(worktree)
	path := obsidian.UnifiedIndexPath(worktree, "")
	if err == nil {
		path = obsidian.UnifiedIndexPath(dir, cfg.IndexPath)
	}
	stat, err := os.Stat(path)
	return path, err == nil && stat.Mode().IsRegular()
}

func gitDirectory(dir string) bool {
	head, headErr := os.Stat(filepath.Join(dir, "HEAD"))
	objects, objectsErr := os.Stat(filepath.Join(dir, "objects"))
	_, dotGitErr := os.Lstat(filepath.Join(dir, ".git"))
	return headErr == nil && head.Mode().IsRegular() && objectsErr == nil && objects.IsDir() && errors.Is(dotGitErr, os.ErrNotExist)
}

func gitWorktreeRoot(dir string) string {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func readLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	return strings.TrimSpace(string(data)), err
}

func resolveFrom(base, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		return canonical
	}
	return filepath.Clean(path)
}

// worktreeGitDir follows a worktree's .git directory or "gitdir:" file.
func worktreeGitDir(root string) (string, error) {
	dotGit := filepath.Join(root, ".git")
	stat, err := os.Stat(dotGit)
	if err != nil {
		return "", err
	}
	if stat.IsDir() {
		return resolveFrom(root, ".git"), nil
	}
	line, err := readLine(dotGit)
	if err != nil {
		return "", err
	}
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", problem("folder_missing", "This folder has unreadable Git metadata.")
	}
	return resolveFrom(root, strings.TrimSpace(target)), nil
}

// commonDir follows a worktree Git directory's "commondir" file, if any.
func commonDir(gitDir string) string {
	if relative, err := readLine(filepath.Join(gitDir, "commondir")); err == nil && relative != "" {
		return resolveFrom(gitDir, relative)
	}
	return gitDir
}

// coreWorktree reads core.worktree from the repository's config file.
func coreWorktree(common string) string {
	data, err := os.ReadFile(filepath.Join(common, "config"))
	if err != nil {
		return ""
	}
	section, value := "", ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.TrimSpace(strings.Trim(line, "[]")))
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if ok && section == "core" && strings.EqualFold(strings.TrimSpace(key), "worktree") {
			value = strings.Trim(strings.TrimSpace(raw), `"`)
		}
	}
	return value
}

// mainWorktree finds the main working tree as Git records it: the parent of a
// ".git" common directory, or core.worktree. A repository made with
// `git init --separate-git-dir` records neither, so a hinted checkout whose
// .git file names the common directory is accepted and reported by fromHint.
func mainWorktree(common, hint string) (path string, fromHint bool) {
	owns := func(dir string) bool {
		gitDir, err := worktreeGitDir(dir)
		return err == nil && gitDir == common
	}
	if filepath.Base(common) == ".git" && owns(filepath.Dir(common)) {
		return filepath.Dir(common), false
	}
	if configured := coreWorktree(common); configured != "" {
		dir := resolveFrom(common, configured)
		if stat, err := os.Stat(dir); err == nil && stat.IsDir() {
			return dir, false
		}
	}
	if hint != "" && owns(hint) {
		return hint, true
	}
	return "", false
}

func readHead(path string) (branch, head string) {
	line, err := readLine(path)
	if err != nil {
		return "", ""
	}
	if ref, ok := strings.CutPrefix(line, "ref: "); ok {
		return strings.TrimPrefix(ref, "refs/heads/"), ""
	}
	if len(line) > 7 {
		line = line[:7]
	}
	return "", line
}

// readWorktrees lists the main working tree first, when one is known, then
// each linked worktree Git would keep. Like `git worktree prune`, it drops a
// linked worktree whose directory is missing unless it is locked; a locked
// missing worktree, or one whose .git names another repository, stays listed
// as unavailable.
func readWorktrees(common, hint string) (worktrees []gitWorktree, fromHint bool, defaultBranch string, err error) {
	main, fromHint := mainWorktree(common, hint)
	if main != "" {
		branch, head := readHead(filepath.Join(common, "HEAD"))
		worktrees = append(worktrees, gitWorktree{path: main, branch: branch, head: head, main: true})
	}
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, "", err
	}
	for _, entry := range entries {
		admin := filepath.Join(common, "worktrees", entry.Name())
		gitFile, err := readLine(filepath.Join(admin, "gitdir"))
		if err != nil || gitFile == "" {
			continue
		}
		branch, head := readHead(filepath.Join(admin, "HEAD"))
		wt := gitWorktree{path: filepath.Dir(resolveFrom(admin, gitFile)), branch: branch, head: head}
		_, lockErr := os.Stat(filepath.Join(admin, "locked"))
		gitDir, err := worktreeGitDir(wt.path)
		switch {
		case errors.Is(err, os.ErrNotExist) && lockErr != nil:
			continue
		case errors.Is(err, os.ErrNotExist):
			wt.err = "This locked worktree is missing. Restore its folder, or unlock and prune it with Git."
		case err != nil || gitDir != resolveFrom(admin, "."):
			wt.err = "This worktree's Git metadata names another location. Run `git worktree repair` in it."
		}
		worktrees = append(worktrees, wt)
	}
	ref, _ := readLine(filepath.Join(common, "refs", "remotes", "origin", "HEAD"))
	defaultBranch, ok := strings.CutPrefix(ref, "ref: refs/remotes/origin/")
	if !ok {
		defaultBranch = ""
	}
	return worktrees, fromHint, defaultBranch, nil
}
